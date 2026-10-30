package scheduler

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/qualification"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

// ExactPreparer composes the final request without inheriting interactive AO
// defaults or choosing another model. Requests supplies trusted account/runtime
// configuration; discovery and permission verification remain qualification's
// responsibility. A preflight using the same profile can pin its capability
// fingerprint before the attempt's prompt and session ID exist.
type ExactPreparer struct {
	Qualification qualification.Service
	Requests      func(context.Context, domain.SwarmAttempt, workspace.Snapshot) (qualification.Request, error)
}

// Prepare binds the packet and workspace to a fresh qualification request.
func (p ExactPreparer) Prepare(ctx context.Context, a domain.SwarmAttempt, packet planning.Packet, snapshot workspace.Snapshot) (qualification.Preparation, error) {
	if p.Requests == nil {
		return qualification.Preparation{}, qualification.ErrUnverified
	}
	if _, err := SpawnConfigFor(a, packet); err != nil {
		return qualification.Preparation{}, err
	}
	req, err := p.Requests(ctx, a, snapshot)
	if err != nil {
		return qualification.Preparation{}, err
	}
	if a.Routing == nil || req.Scope != a.Routing.CatalogScope {
		return qualification.Preparation{}, domain.ErrSwarmConflict
	}
	req.Role = qualification.Role(domain.SwarmTaskRole(a.Contract))
	req.Requested = qualification.Selection{AgentID: string(a.Resolved.Harness), Model: a.Resolved.Model, Effort: a.Resolved.Effort}
	req.Policy = qualification.Policy{} // route changes require a new attempt
	req.Launch.Prompt = packet.Markdown
	req.Launch.SessionID = exactSessionID(a.OperationID)
	req.Launch.NativeSessionID = ""
	req.Launch.WorkspacePath = snapshot.Destination.Path
	req.Launch.Kind = domain.KindWorker
	return p.Qualification.Prepare(ctx, req)
}

// ExactRuntime connects the controller to the session-manager's single-use
// Runtime bridge. This remains TEST evidence, including when a real local helper
// process is used. It is not daemon wiring or model/sandbox qualification.
type ExactRuntime struct {
	Launcher sessionmanager.SwarmExactLauncher
	Preparer Preparer
	Composer interface {
		ValidateDispatch(context.Context, planning.Packet) error
	}
	Now func() time.Time
}

// Mode reports test evidence even when the exact bridge uses real helper processes.
func (*ExactRuntime) Mode() qualification.EvidenceMode { return qualification.TestEvidence }
func (r *ExactRuntime) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
func exactSessionID(operation string) string { return "swarm-" + domain.SwarmHash(operation) }

// Start consumes a bound launch receipt without replaying a previously attempted creation.
func (r *ExactRuntime) Start(ctx context.Context, launch Launch) (Observation, error) {
	if r.Preparer == nil || r.Composer == nil || r.Launcher.Store == nil || r.Launcher.Runtime == nil {
		return Observation{}, qualification.ErrUnverified
	}
	createdAt := r.now()
	prior, err := r.Launcher.Store.GetSwarmExecution(ctx, launch.Attempt.ID)
	if err == nil {
		// Recovery checks the original immutable decision, not a newly dated
		// authorization. The launcher will read its result without revalidating
		// expired evidence or mutable worker files and will never call Create.
		createdAt = prior.CreatedAt
	} else if !errors.Is(err, domain.ErrSwarmNotFound) {
		return Observation{}, err
	}
	receipt, cfg, err := exactLaunch(launch, createdAt)
	if err != nil {
		return Observation{}, fmt.Errorf("freeze exact launch: %w", err)
	}
	launcher := r.Launcher
	launcher.Now = r.Now
	launcher.Validate = func(ctx context.Context, got domain.SwarmExecutionReceipt, cfg ports.RuntimeConfig) error {
		if domain.SwarmHash(got) != domain.SwarmHash(receipt) || domain.SwarmHash(cfg) != receipt.ConfigHash {
			return domain.ErrSwarmConflict
		}
		if err := r.Composer.ValidateDispatch(ctx, launch.Packet); err != nil {
			return fmt.Errorf("revalidate dispatch packet: %w", err)
		}
		fresh, prepareErr := r.Preparer.Prepare(ctx, launch.Attempt, launch.Packet, launch.Workspace)
		if prepareErr != nil && !errors.Is(prepareErr, qualification.ErrUnverified) {
			return prepareErr
		}
		current := launch
		current.Preparation = fresh
		rechecked, _, err := exactLaunch(current, receipt.CreatedAt)
		if err != nil {
			return fmt.Errorf("recheck final launch: %w", err)
		}
		// A refreshed receipt may have a later expiry, but the original final
		// evidence is the one consumed. Changed binding or receipt requires a new
		// attempt; never silently rewrite an authorized operation.
		if domain.SwarmHash(rechecked) != domain.SwarmHash(receipt) {
			return fmt.Errorf("final receipt changed during revalidation: %w", qualification.ErrUnverified)
		}
		return workspace.ValidateForLaunch(ctx, launch.Workspace)
	}
	result, err := launcher.Start(ctx, receipt, cfg)
	if err != nil {
		return Observation{}, err
	}
	return r.observation(ctx, launch.Attempt, receipt, result), nil
}

func exactLaunch(l Launch, now time.Time) (domain.SwarmExecutionReceipt, ports.RuntimeConfig, error) {
	var receipt domain.SwarmExecutionReceipt
	var cfg ports.RuntimeConfig
	a, p := l.Attempt, l.Preparation
	if _, err := SpawnConfigFor(a, l.Packet); err != nil {
		return receipt, cfg, err
	}
	if a.Schedule == nil || a.Schedule.SourceManifestHash != l.Workspace.ManifestHash ||
		a.Schedule.WorkspaceIdentityHash != l.Workspace.IdentityHash || a.Schedule.WorkspaceRoot != l.Workspace.Destination.Path || a.BaseCommit != l.Workspace.Source.Head {
		return receipt, cfg, domain.ErrSwarmStale
	}
	check := Service{Runtime: &ExactRuntime{}, Now: func() time.Time { return now }}
	if err := check.checkPreparation(a, l.Workspace, p, nil); err != nil {
		return receipt, cfg, err
	}
	if p.Report.FallbackUsed || p.PromptDelivery != ports.PromptDeliveryInCommand || p.Launch.Prompt != l.Packet.Markdown ||
		p.Launch.SessionID != exactSessionID(a.OperationID) || p.Launch.NativeSessionID != "" || p.Launch.Kind != domain.KindWorker ||
		p.Launch.Permissions == ports.PermissionModeBypassPermissions || !p.Launch.Permissions.Valid() ||
		p.Launch.Config.Permissions != p.Launch.Permissions || len(p.Command) == 0 || p.Command[0] != p.Report.Runtime.BinaryPath {
		return receipt, cfg, qualification.ErrUnverified
	}
	cfg = ports.RuntimeConfig{SessionID: domain.SessionID(p.Launch.SessionID), WorkspacePath: p.Launch.WorkspacePath,
		Argv: slices.Clone(p.Command), Env: maps.Clone(p.Launch.Env), ExitOnCommandCompletion: true}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	// This coordinator generation is the only addition to the prepared overlay;
	// it is included in the final config hash. It is not an agent permission.
	generation := domain.SwarmHash(a.OperationID)
	if value, exists := cfg.Env["AO_RUNTIME_LAUNCH_ID"]; exists && value != generation {
		return receipt, cfg, domain.ErrSwarmConflict
	}
	cfg.Env["AO_RUNTIME_LAUNCH_ID"] = generation
	receipt = domain.SwarmExecutionReceipt{
		Version: 1, AttemptID: a.ID, OperationID: a.OperationID, Owner: a.Owner, Generation: a.Generation,
		SessionID: string(cfg.SessionID), CapabilityFingerprint: a.CapabilityFingerprint,
		QualificationBinding: p.Report.Binding, PacketHash: l.Packet.SHA256,
		WorkspaceIdentityHash: a.Schedule.WorkspaceIdentityHash, SourceManifestHash: a.Schedule.SourceManifestHash,
		WorkspaceRoot: cfg.WorkspacePath, ConfigHash: domain.SwarmHash(cfg), PermissionReceiptID: p.Report.Evidence.ReceiptID,
		EvidenceMode: string(p.Report.Evidence.Mode), ExpiresAt: p.Report.Evidence.ExpiresAt, CreatedAt: now,
	}
	return receipt, cfg, domain.ValidateSwarmExecutionReceipt(receipt)
}

// Observe reconciles retained execution facts against the exact runtime generation.
func (r *ExactRuntime) Observe(ctx context.Context, a domain.SwarmAttempt) (Observation, error) {
	unknown := Observation{OperationID: a.OperationID, State: "unknown"}
	if r.Launcher.Store == nil || r.Launcher.Runtime == nil {
		return unknown, qualification.ErrUnverified
	}
	receipt, err := r.Launcher.Store.GetSwarmExecution(ctx, a.ID)
	if errors.Is(err, domain.ErrSwarmNotFound) {
		return unknown, nil
	}
	if err != nil {
		return unknown, err
	}
	if receipt.OperationID != a.OperationID || receipt.CapabilityFingerprint != a.CapabilityFingerprint || receipt.SessionID != exactSessionID(a.OperationID) {
		return unknown, domain.ErrSwarmConflict
	}
	result, err := r.Launcher.Store.GetSwarmExecutionResult(ctx, a.ID)
	if errors.Is(err, domain.ErrSwarmNotFound) {
		return unknown, nil
	}
	if err != nil {
		return unknown, err
	}
	if err := validateExactExecution(a, receipt, result); err != nil {
		return unknown, err
	}
	return r.observation(ctx, a, receipt, result), nil
}

func (r *ExactRuntime) observation(ctx context.Context, a domain.SwarmAttempt, receipt domain.SwarmExecutionReceipt, result domain.SwarmExecutionResult) Observation {
	out := Observation{OperationID: a.OperationID, SessionID: receipt.SessionID, State: "unknown"}
	if validateExactExecution(a, receipt, result) != nil {
		return out
	}
	if result.Outcome == "no_effect" {
		out.State, out.Exact, out.Terminal = "absent", true, true
		return out
	}
	// An ambiguous Create may still be in flight externally. Even an absent
	// instantaneous probe cannot release its slot or authorize another launch.
	if result.Outcome != "created" || result.HandleID == "" {
		return out
	}
	prober, ok := r.Launcher.Runtime.(ports.FencedRuntimeProber)
	if !ok {
		return out
	}
	probe := prober.ProbeFencedRuntime(ctx, ports.FencedRuntimeRef{Handle: ports.RuntimeHandle{ID: result.HandleID}, SessionID: domain.SessionID(receipt.SessionID), Generation: domain.SwarmHash(receipt.OperationID)})
	if probe.Liveness == ports.FencedAlive && probe.Reason == ports.FencedReasonExactMatch {
		out.State, out.Exact = "alive", true
	}
	if probe.Liveness == ports.FencedDead && probe.Reason == ports.FencedReasonExactAbsent {
		out.State, out.Exact, out.Terminal = "absent", true, true
	}
	return out
}
