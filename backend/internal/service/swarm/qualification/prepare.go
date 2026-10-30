package qualification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Prepare validates the selection and builds AO's existing launch command. It
// never starts an agent, modifies authentication, installs hooks, or retries a
// failed model request. Only explicitly authorized unavailable-model fallbacks
// are considered, once each; other failures immediately stop preparation.
func (s Service) Prepare(ctx context.Context, req Request) (Preparation, error) {
	out := Preparation{Report: Report{Requested: req.Requested, Telemetry: "unknown", Auth: ports.AgentAuthStatusUnknown}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if !validRole(req.Role) {
		return out, fmt.Errorf("%w: unknown role %q", ErrInvalid, req.Role)
	}
	if err := validateSelection(req.Requested); err != nil {
		return out, err
	}
	if s.Catalog == nil || s.Agent == nil || s.Runtime == nil {
		return out, fmt.Errorf("%w: catalog, adapter lookup and runtime inspector are required", ErrUnverified)
	}
	if err := req.Launch.Config.Validate(); err != nil {
		return out, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if !req.Launch.Permissions.Valid() || req.Launch.Permissions == ports.PermissionModeBypassPermissions || req.Launch.Config.Permissions == ports.PermissionModeBypassPermissions {
		return out, fmt.Errorf("%w: select a supported non-bypass permission mode", ErrUnsafePermissions)
	}
	if req.Launch.Config.Mode != "" || req.Launch.Config.Launch.FastMode && req.Launch.Config.Launch.FastModeOff {
		return out, fmt.Errorf("%w: conflicting model mode or fast-tier settings", ErrInvalid)
	}
	if req.Launch.Config.Permissions != "" && req.Launch.Config.Permissions != req.Launch.Permissions {
		return out, fmt.Errorf("%w: launch and config permission modes disagree", ErrInvalid)
	}
	directories, err := validatePaths(&req)
	if err != nil {
		return out, err
	}
	defer closeDirectories(directories)
	selections := append([]Selection{req.Requested}, req.Policy.Fallbacks[req.Role]...)
	seen := make(map[Selection]bool)
	for _, selection := range selections {
		if err := validateSelection(selection); err != nil {
			return out, err
		}
		if seen[selection] {
			return out, fmt.Errorf("%w: duplicate role fallback", ErrInvalid)
		}
		seen[selection] = true
	}
	var adapter ports.Agent
	var catalog ports.AgentModelCatalog
	var last error
	for i, selection := range selections {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		var err error
		adapter, err = s.Agent(selection.AgentID)
		if err == nil && adapter == nil {
			err = fmt.Errorf("%w: adapter %s is not installed", ErrUnavailable, selection.AgentID)
		}
		if err == nil {
			catalog, err = s.Catalog.RevalidateModels(ctx, selection.AgentID, req.Scope)
			if err == nil {
				err = validateCatalog(ctx, selection, catalog)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			if errors.Is(err, ErrUnavailable) || errors.Is(err, ports.ErrAgentBinaryNotFound) {
				last = err
				continue
			}
			return out, err
		}
		out.Report.Resolved, out.Report.FallbackUsed = selection, i != 0
		last = nil
		break
	}
	if last != nil {
		return out, last
	}
	launch := cloneLaunch(req.Launch)
	launch.Config.Model, launch.Config.Effort = out.Report.Resolved.Model, out.Report.Resolved.Effort
	launch.Config.Permissions = launch.Permissions
	if (launch.Config.Launch.FastMode || launch.Config.Launch.FastModeOff) && !supportsFast(catalog, launch.Config.Model) {
		return out, fmt.Errorf("%w: selected model does not advertise fast-tier control", ErrInvalid)
	}
	runtime, err := s.Runtime.Inspect(ctx, out.Report.Resolved.AgentID, cloneLaunch(launch))
	if err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := validateRuntime(runtime); err != nil {
		return out, err
	}
	out.Report.Runtime = runtime
	switch auth := adapter.(type) {
	case ports.AgentLaunchAuthValidator:
		out.Report.Auth, err = auth.ValidateLaunchAuth(ctx, launch.WorkspacePath, maps.Clone(launch.Env))
	case ports.AgentAuthChecker:
		out.Report.Auth, err = auth.AuthStatus(ctx)
	}
	if err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if out.Report.Auth == ports.AgentAuthStatusUnauthorized {
		return out, fmt.Errorf("%w: authenticate the selected adapter using existing account settings", ports.ErrAgentAuthRequired)
	}
	if out.Report.Auth != ports.AgentAuthStatusAuthorized {
		return out, fmt.Errorf("%w: configured credentials or missing auth probes do not prove launch authorization", ErrUnverified)
	}
	command, err := adapter.GetLaunchCommand(ctx, cloneLaunch(launch))
	if err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	out.PromptDelivery, err = adapter.GetPromptDeliveryStrategy(ctx, cloneLaunch(launch))
	if err != nil {
		return out, err
	}
	if len(command) == 0 || filepath.Clean(command[0]) != runtime.BinaryPath {
		return out, fmt.Errorf("%w: command does not use the inspected executable", ErrIdentityMismatch)
	}
	for i, arg := range command {
		if arg == "--" {
			break
		}
		if arg == "--dangerously-bypass-approvals-and-sandbox" || arg == "--dangerously-skip-permissions" || (arg == "bypassPermissions" && i > 0 && command[i-1] == "--permission-mode") {
			return out, fmt.Errorf("%w: adapter emitted a sandbox/permission bypass; choose a supported restricted launch", ErrUnsafePermissions)
		}
	}
	current, err := s.Runtime.Inspect(ctx, out.Report.Resolved.AgentID, cloneLaunch(launch))
	if err != nil {
		return out, err
	}
	if current != runtime {
		return out, fmt.Errorf("%w: runtime changed while constructing launch; refresh qualification", ErrUnverified)
	}
	out.Launch, out.Command = launch, slices.Clone(command)
	profile := cloneLaunch(launch)
	profile.Prompt, profile.SessionID, profile.NativeSessionID = "", "", ""
	out.Report.CapabilityFingerprint, err = fingerprint(struct {
		Version       int
		Role          Role
		Scope         string
		Selection     Selection
		Runtime       RuntimeIdentity
		Catalog       string
		Launch        ports.LaunchConfig
		LaunchOptions domain.AgentLaunchOptions
		Permissions   PermissionRequirements
		Directories   []directoryBinding
	}{1, req.Role, req.Scope, out.Report.Resolved, runtime, catalog.InputFingerprint, profile,
		profile.Config.Launch, req.Permissions, directoryBindings(directories)})
	if err != nil {
		return out, err
	}
	binding, err := fingerprint(struct {
		CapabilityFingerprint string
		PromptDelivery        ports.PromptDeliveryStrategy
		Role                  Role
		Scope                 string
		Selection             Selection
		Runtime               RuntimeIdentity
		Catalog               string
		Launch                ports.LaunchConfig
		Command               []string
		Permissions           PermissionRequirements
		// AgentConfig.Launch intentionally has json:"-" in the upstream API.
		// Include the full struct explicitly so current and future options bind
		// evidence even when an adapter ignores an option in its command line.
		LaunchOptions domain.AgentLaunchOptions
		Directories   []directoryBinding
	}{out.Report.CapabilityFingerprint, out.PromptDelivery, req.Role, req.Scope, out.Report.Resolved, runtime, catalog.InputFingerprint, launch, command, req.Permissions, launch.Config.Launch, directoryBindings(directories)})
	if err != nil {
		return out, err
	}
	out.Report.Binding = binding
	if s.Permissions == nil {
		return out, fmt.Errorf("%w: install a coordinator-owned live sandbox verifier; approval flags alone are insufficient", ErrUnverified)
	}
	evidence, err := s.Permissions.Verify(ctx, Probe{binding, req.Role, runtime, out.Report.Resolved, cloneLaunch(launch), slices.Clone(command), req.Permissions})
	out.Report.Evidence = evidence
	if err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	current, err = s.Runtime.Inspect(ctx, out.Report.Resolved.AgentID, cloneLaunch(launch))
	if err != nil {
		return out, err
	}
	if current != runtime {
		return out, fmt.Errorf("%w: runtime changed during capability verification; repeat qualification", ErrUnverified)
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := revalidateDirectories(directories); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if evidence.Binding != binding || evidence.ReceiptID == "" || !evidence.ExpiresAt.After(now) {
		return out, fmt.Errorf("%w: capability receipt is missing, expired or bound to different runtime/configuration", ErrUnverified)
	}
	checks := evidence.Checks
	if checks.SettingsPreserved != Passed || checks.CancellationObserved != Passed || checks.ProcessContainment != Passed || checks.ProtectedStorageDenied != Passed || (req.Role == Reviewer && checks.SubmissionReadOnly != Passed) {
		return out, fmt.Errorf("%w: require observed settings, cancellation, descendant containment, protected-storage denial and read-only review where applicable", ErrUnverified)
	}
	switch evidence.Mode {
	case TestEvidence:
		out.Report.TestVerified = true
		return out, fmt.Errorf("%w: test-mode evidence cannot authorize live dispatch", ErrUnverified)
	case LiveEvidence:
		out.Report.LiveQualified = true
		return out, nil
	default:
		return out, fmt.Errorf("%w: receipt must identify test or live evidence", ErrUnverified)
	}
}

func cloneLaunch(launch ports.LaunchConfig) ports.LaunchConfig {
	launch.Env = maps.Clone(launch.Env)
	launch.AllowedTools = slices.Clone(launch.AllowedTools)
	launch.DisallowedTools = slices.Clone(launch.DisallowedTools)
	return launch
}

func supportsFast(catalog ports.AgentModelCatalog, modelID string) bool {
	for _, model := range catalog.Models {
		if model.ID == modelID {
			return model.SupportsFast
		}
	}
	return false
}

func fingerprint(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func validDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size
}

func validateRuntime(identity RuntimeIdentity) error {
	if !filepath.IsAbs(identity.BinaryPath) || filepath.Clean(identity.BinaryPath) != identity.BinaryPath || !validDigest(identity.BinarySHA256) || strings.TrimSpace(identity.Version) == "" || strings.TrimSpace(identity.ConfigFingerprint) == "" {
		return fmt.Errorf("%w: inspect exact binary path/hash/version and effective configuration before qualification", ErrUnverified)
	}
	return nil
}

func overlaps(a, b string) bool {
	contains := func(parent, child string) bool {
		relative, err := filepath.Rel(parent, child)
		return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	return contains(a, b) || contains(b, a)
}
