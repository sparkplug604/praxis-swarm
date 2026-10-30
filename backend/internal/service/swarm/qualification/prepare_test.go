package qualification

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/agentbase"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/pkg/agentruntime"
)

type catalogStub struct {
	catalogs map[string]ports.AgentModelCatalog
	err      error
	calls    []string
}

func (f *catalogStub) RevalidateModels(ctx context.Context, id, scope string) (ports.AgentModelCatalog, error) {
	f.calls = append(f.calls, id)
	return f.catalogs[id], f.err
}

type runtimeFunc func(context.Context, string, ports.LaunchConfig) (RuntimeIdentity, error)

func (f runtimeFunc) Inspect(ctx context.Context, id string, cfg ports.LaunchConfig) (RuntimeIdentity, error) {
	return f(ctx, id, cfg)
}

type verifierFunc func(context.Context, Probe) (CapabilityEvidence, error)

func (f verifierFunc) Verify(ctx context.Context, probe Probe) (CapabilityEvidence, error) {
	return f(ctx, probe)
}

// Discovery/auth/runtime are test doubles; argv construction calls AO's actual
// shared Codex builder. This proves command construction, not live confinement.
type specAgent struct {
	agentbase.Base
	binary   string
	auth     ports.AgentAuthStatus
	authErr  error
	launches int
	onLaunch func()
}

func (a *specAgent) AuthStatus(context.Context) (ports.AgentAuthStatus, error) {
	return a.auth, a.authErr
}
func (a *specAgent) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) ([]string, error) {
	a.launches++
	if a.onLaunch != nil {
		a.onLaunch()
	}
	var provider []string
	if cfg.Config.Effort != "" {
		provider = []string{"-c", fmt.Sprintf("model_reasoning_effort=%q", cfg.Config.Effort)}
	}
	return agentruntime.BuildLaunchCommand(agentruntime.LaunchConfig{
		Harness: agentruntime.HarnessCodex, Binary: a.binary,
		WorkspacePath: cfg.WorkspacePath, Model: cfg.Config.Model,
		Prompt: cfg.Prompt, Permission: agentruntime.PermissionPolicy(cfg.Permissions), ProviderArgs: provider,
	})
}

type fixture struct {
	service Service
	request Request
	catalog *catalogStub
	agent   *specAgent
	runtime *RuntimeIdentity
	now     time.Time
}

func setup(t *testing.T) fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"workspace", "coordinator", "evaluator", "agent-data"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	selected := Selection{"codex", "selected-model", "high"}
	catalog := &catalogStub{catalogs: map[string]ports.AgentModelCatalog{"codex": {
		AgentID: "codex", SelectionMode: ports.ModelSelectionCatalog, InputFingerprint: "catalog-v1",
		Models: []ports.AgentModelInfo{{ID: selected.Model, Efforts: []string{"low", "high"}}},
	}}}
	identity := &RuntimeIdentity{BinaryPath: filepath.Join(root, "codex"), BinarySHA256: strings.Repeat("a", 64), Version: "test-version", ConfigFingerprint: "config-v1"}
	agent := &specAgent{binary: identity.BinaryPath, auth: ports.AgentAuthStatusAuthorized}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	service := Service{
		Catalog: catalog, Agent: func(string) (ports.Agent, error) { return agent, nil },
		Runtime: runtimeFunc(func(context.Context, string, ports.LaunchConfig) (RuntimeIdentity, error) { return *identity, nil }),
		Now:     func() time.Time { return now },
	}
	service.Permissions = verifierFunc(func(_ context.Context, p Probe) (CapabilityEvidence, error) {
		return passing(p, TestEvidence, now), nil
	})
	request := Request{Role: Worker, Requested: selected, Scope: "project-1",
		Launch:      ports.LaunchConfig{WorkspacePath: filepath.Join(root, "workspace"), DataDir: filepath.Join(root, "agent-data"), Permissions: ports.PermissionModeAuto, Prompt: "-implement requested task"},
		Permissions: PermissionRequirements{CoordinatorRoot: filepath.Join(root, "coordinator"), EvaluatorRoot: filepath.Join(root, "evaluator")},
	}
	return fixture{service, request, catalog, agent, identity, now}
}
func passing(p Probe, mode EvidenceMode, now time.Time) CapabilityEvidence {
	return CapabilityEvidence{Binding: p.Binding, Mode: mode, ReceiptID: "coordinator-receipt", ExpiresAt: now.Add(time.Hour), Checks: CapabilityChecks{Passed, Passed, Passed, Passed, Passed}}
}

func TestCapabilityProfileDoesNotHashItsOwnTaskPrompt(t *testing.T) {
	f := setup(t)
	before, err := f.service.Prepare(context.Background(), f.request)
	if !errors.Is(err, ErrUnverified) || !before.Report.TestVerified {
		t.Fatalf("preflight: %v", err)
	}
	f.request.Launch.Prompt = "Task packet with capability: " + before.Report.CapabilityFingerprint
	f.request.Launch.SessionID = "swarm-operation-1"
	after, err := f.service.Prepare(context.Background(), f.request)
	if !errors.Is(err, ErrUnverified) || !after.Report.TestVerified {
		t.Fatalf("final preparation: %v", err)
	}
	if !validDigest(before.Report.CapabilityFingerprint) || before.Report.CapabilityFingerprint != after.Report.CapabilityFingerprint || before.Report.Binding == after.Report.Binding {
		t.Fatal("profile must remain stable while final prompt/session binding changes")
	}
	for name, mutate := range map[string]func(*Request){
		"environment": func(r *Request) { r.Launch.Env = map[string]string{"PROFILE": "changed"} },
		"permissions": func(r *Request) { r.Launch.Permissions = ports.PermissionModeAcceptEdits },
		"scope":       func(r *Request) { r.Scope = "another-account" },
		"tools":       func(r *Request) { r.Launch.DisallowedTools = []string{"write"} },
	} {
		t.Run(name, func(t *testing.T) {
			req := f.request
			mutate(&req)
			changed, err := f.service.Prepare(context.Background(), req)
			if !errors.Is(err, ErrUnverified) || changed.Report.CapabilityFingerprint == after.Report.CapabilityFingerprint {
				t.Fatalf("unbound profile change: %v", err)
			}
		})
	}
}

func TestPreparationUsesAOCommandAndKeepsTestEvidenceSeparate(t *testing.T) {
	f := setup(t)
	result, err := f.service.Prepare(context.Background(), f.request)
	if !errors.Is(err, ErrUnverified) || !result.Report.TestVerified || result.Report.LiveQualified {
		t.Fatalf("result=%+v err=%v", result.Report, err)
	}
	if result.Report.Requested != f.request.Requested || result.Report.Resolved != f.request.Requested || result.Report.Telemetry != "unknown" || result.Report.ReportedModel != "" {
		t.Fatalf("identity=%+v", result.Report)
	}
	command := strings.Join(result.Command, " ")
	for _, want := range []string{"--model selected-model", `model_reasoning_effort="high"`, "-- -implement requested task"} {
		if !strings.Contains(command, want) {
			t.Errorf("command %q missing %q", command, want)
		}
	}
	if f.request.Launch.Config.Model != "" {
		t.Fatal("caller request mutated")
	}
}

func TestRoleFallbackIsExplicitAndUnavailableOnly(t *testing.T) {
	for _, role := range []Role{Planner, Worker, Reviewer, Integration} {
		t.Run(string(role), func(t *testing.T) {
			f := setup(t)
			f.request.Role = role
			if role == Reviewer {
				f.request.Permissions.SubmissionSHA256 = strings.Repeat("b", 64)
			}
			f.request.Requested.Model = "missing"
			f.request.Policy.Fallbacks = map[Role][]Selection{role: {{"codex", "selected-model", "low"}}}
			result, err := f.service.Prepare(context.Background(), f.request)
			if !errors.Is(err, ErrUnverified) || !result.Report.TestVerified || !result.Report.FallbackUsed || result.Report.Resolved.Effort != "low" || len(f.catalog.calls) != 2 {
				t.Fatalf("report=%+v calls=%v err=%v", result.Report, f.catalog.calls, err)
			}
		})
	}
	f := setup(t)
	f.request.Requested.Model = "missing"
	f.request.Policy.Fallbacks = map[Role][]Selection{Reviewer: {{"codex", "selected-model", "low"}}}
	if _, err := f.service.Prepare(context.Background(), f.request); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if f.agent.launches != 0 {
		t.Fatal("unavailable selection launched")
	}
}

func TestInvalidCatalogAndAuthNeverFallBack(t *testing.T) {
	tests := []struct {
		name   string
		change func(*fixture)
		want   error
	}{
		{"unsupported effort", func(f *fixture) { f.request.Requested.Effort = "invented" }, ports.ErrUnsupportedEffort},
		{"stale catalog", func(f *fixture) { c := f.catalog.catalogs["codex"]; c.Stale = true; f.catalog.catalogs["codex"] = c }, ports.ErrModelCapabilitiesUnavailable},
		{"fingerprint missing", func(f *fixture) {
			c := f.catalog.catalogs["codex"]
			c.InputFingerprint = ""
			f.catalog.catalogs["codex"] = c
		}, ports.ErrModelCapabilitiesUnavailable},
		{"wrong adapter catalog", func(f *fixture) {
			c := f.catalog.catalogs["codex"]
			c.AgentID = "other"
			f.catalog.catalogs["codex"] = c
		}, ports.ErrModelCapabilitiesUnavailable},
		{"mode catalog", func(f *fixture) {
			c := f.catalog.catalogs["codex"]
			c.SelectionMode = ports.ModelSelectionModeList
			f.catalog.catalogs["codex"] = c
		}, ports.ErrModelCapabilitiesUnavailable},
		{"catalog auth", func(f *fixture) {
			c := f.catalog.catalogs["codex"]
			c.WarningCode = ports.ModelCatalogWarningAuthRequired
			f.catalog.catalogs["codex"] = c
		}, ports.ErrAgentAuthRequired},
		{"expired credential", func(f *fixture) { f.catalog.err = ports.ErrAgentModelDiscoveryCredentialExpired }, ports.ErrAgentModelDiscoveryCredentialExpired},
		{"auth denied", func(f *fixture) { f.agent.auth = ports.AgentAuthStatusUnauthorized }, ports.ErrAgentAuthRequired},
		{"auth unknown", func(f *fixture) { f.agent.auth = ports.AgentAuthStatusUnknown }, ErrUnverified},
		{"auth configured", func(f *fixture) { f.agent.auth = ports.AgentAuthStatusConfigured }, ErrUnverified},
		{"auth network error", func(f *fixture) { f.agent.authErr = context.DeadlineExceeded }, context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			f.request.Policy.Fallbacks = map[Role][]Selection{Worker: {{"other", "different-model", "low"}}}
			test.change(&f)
			_, err := f.service.Prepare(context.Background(), f.request)
			if !errors.Is(err, test.want) || len(f.catalog.calls) != 1 || f.agent.launches != 0 {
				t.Fatalf("err=%v catalog calls=%v launches=%d", err, f.catalog.calls, f.agent.launches)
			}
		})
	}
}

func TestCancellationStopsPreparation(t *testing.T) {
	for _, stage := range []string{"before", "command", "verification"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch stage {
			case "before":
				cancel()
			case "command":
				f.agent.onLaunch = cancel
			case "verification":
				f.service.Permissions = verifierFunc(func(_ context.Context, p Probe) (CapabilityEvidence, error) {
					cancel()
					return passing(p, LiveEvidence, f.now), nil
				})
			}
			result, err := f.service.Prepare(ctx, f.request)
			if !errors.Is(err, context.Canceled) || result.Report.LiveQualified {
				t.Fatalf("report=%+v err=%v", result.Report, err)
			}
			if stage == "before" && len(f.catalog.calls) > 0 {
				t.Fatal("cancelled preparation performed discovery")
			}
		})
	}
}

func TestPermissionRestrictionsCannotBeReplacedByApprovalFlags(t *testing.T) {
	tests := []struct {
		name   string
		change func(*fixture)
		want   error
	}{
		{"missing verifier", func(f *fixture) { f.service.Permissions = nil }, ErrUnverified},
		{"explicit bypass", func(f *fixture) { f.request.Launch.Permissions = ports.PermissionModeBypassPermissions }, ErrUnsafePermissions},
		{"AO default emits bypass", func(f *fixture) { f.request.Launch.Permissions = ports.PermissionModeDefault }, ErrUnsafePermissions},
		{"nested protected storage", func(f *fixture) { f.request.Permissions.CoordinatorRoot = f.request.Launch.WorkspacePath }, ErrUnsafePermissions},
		{"agent data is coordinator", func(f *fixture) { f.request.Launch.DataDir = f.request.Permissions.CoordinatorRoot }, ErrUnsafePermissions},
		{"review missing frozen source", func(f *fixture) { f.request.Role = Reviewer }, ErrInvalid},
		{"invented permission", func(f *fixture) { f.request.Launch.Permissions = "invented" }, ErrUnsafePermissions},
		{"conflicting mode", func(f *fixture) { f.request.Launch.Config.Mode = "low" }, ErrInvalid},
		{"unadvertised fast tier", func(f *fixture) { f.request.Launch.Config.Launch.FastMode = true }, ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			test.change(&f)
			r, err := f.service.Prepare(context.Background(), f.request)
			if !errors.Is(err, test.want) || r.Report.LiveQualified {
				t.Fatalf("report=%+v err=%v", r.Report, err)
			}
		})
	}
	t.Run("symlink to protected storage", func(t *testing.T) {
		f := setup(t)
		link := filepath.Join(filepath.Dir(f.request.Launch.WorkspacePath), "alias")
		if err := os.Symlink(f.request.Permissions.CoordinatorRoot, link); err != nil {
			t.Fatal(err)
		}
		f.request.Launch.WorkspacePath = link
		if _, err := f.service.Prepare(context.Background(), f.request); !errors.Is(err, ErrUnsafePermissions) {
			t.Fatal(err)
		}
	})
}

func TestReceiptMustBeCurrentBoundLiveAndComplete(t *testing.T) {
	for _, mutation := range []string{"binding", "expired", "missing receipt", "test", "unknown mode", "settings", "cancellation", "processes", "protected", "review writes"} {
		t.Run(mutation, func(t *testing.T) {
			f := setup(t)
			f.request.Role = Reviewer
			f.request.Permissions.SubmissionSHA256 = strings.Repeat("b", 64)
			f.service.Permissions = verifierFunc(func(_ context.Context, p Probe) (CapabilityEvidence, error) {
				e := passing(p, LiveEvidence, f.now)
				switch mutation {
				case "binding":
					e.Binding = "another runtime"
				case "expired":
					e.ExpiresAt = f.now
				case "missing receipt":
					e.ReceiptID = ""
				case "test":
					e.Mode = TestEvidence
				case "unknown mode":
					e.Mode = "declared"
				case "settings":
					e.Checks.SettingsPreserved = Unknown
				case "cancellation":
					e.Checks.CancellationObserved = Unsupported
				case "processes":
					e.Checks.ProcessContainment = Unknown
				case "protected":
					e.Checks.ProtectedStorageDenied = Failed
				case "review writes":
					e.Checks.SubmissionReadOnly = Failed
				}
				return e, nil
			})
			r, err := f.service.Prepare(context.Background(), f.request)
			if !errors.Is(err, ErrUnverified) || r.Report.LiveQualified {
				t.Fatalf("report=%+v err=%v", r.Report, err)
			}
		})
	}
}

func TestBindingInvalidatesOnlyChangedPreparation(t *testing.T) {
	for _, field := range []string{"version", "binary", "config", "catalog", "model effort", "permission", "environment", "submission"} {
		t.Run(field, func(t *testing.T) {
			f := setup(t)
			f.request.Role = Reviewer
			f.request.Permissions.SubmissionSHA256 = strings.Repeat("b", 64)
			before, err := f.service.Prepare(context.Background(), f.request)
			if !errors.Is(err, ErrUnverified) {
				t.Fatal(err)
			}
			again, _ := f.service.Prepare(context.Background(), f.request)
			if before.Report.Binding != again.Report.Binding {
				t.Fatal("unchanged binding drift")
			}
			switch field {
			case "version":
				f.runtime.Version = "next"
			case "binary":
				f.runtime.BinarySHA256 = strings.Repeat("c", 64)
			case "config":
				f.runtime.ConfigFingerprint = "next"
			case "catalog":
				c := f.catalog.catalogs["codex"]
				c.InputFingerprint = "next"
				f.catalog.catalogs["codex"] = c
			case "model effort":
				f.request.Requested.Effort = "low"
			case "permission":
				f.request.Launch.Permissions = ports.PermissionModeAcceptEdits
			case "environment":
				f.request.Launch.Env = map[string]string{"SETTING": "different"}
			case "submission":
				f.request.Permissions.SubmissionSHA256 = strings.Repeat("d", 64)
			}
			f.service.Permissions = verifierFunc(func(context.Context, Probe) (CapabilityEvidence, error) {
				e := before.Report.Evidence
				e.Mode = LiveEvidence
				return e, nil
			})
			after, err := f.service.Prepare(context.Background(), f.request)
			if !errors.Is(err, ErrUnverified) || after.Report.Binding == before.Report.Binding || after.Report.LiveQualified {
				t.Fatalf("receipt reused across change: %v", err)
			}
		})
	}
}

func TestRuntimeChangeDuringPreparationIsRejected(t *testing.T) {
	f := setup(t)
	f.agent.onLaunch = func() { f.runtime.Version = "updated" }
	if _, err := f.service.Prepare(context.Background(), f.request); !errors.Is(err, ErrUnverified) {
		t.Fatal(err)
	}
}

func TestTrustedLiveReceiptAdmissionDoesNotInventTelemetry(t *testing.T) {
	// Synthetic receipt exercises admission logic only; this is not a live test.
	f := setup(t)
	f.service.Permissions = verifierFunc(func(_ context.Context, p Probe) (CapabilityEvidence, error) {
		return passing(p, LiveEvidence, f.now), nil
	})
	r, err := f.service.Prepare(context.Background(), f.request)
	if err != nil || !r.Report.LiveQualified || r.Report.TestVerified || r.Report.Telemetry != "unknown" {
		t.Fatalf("report=%+v err=%v", r.Report, err)
	}
	report, err := RecordReportedIdentity(r.Report, "", "high")
	if err != nil || report.ReportedModel != "" {
		t.Fatal("inferred executed model")
	}
	report, err = RecordReportedIdentity(report, "different-model", "high")
	if !errors.Is(err, ErrIdentityMismatch) || report.LiveQualified || report.Telemetry != "mismatch" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestRunnableModelAndMissingAdapterUseOnlyAuthorizedAlternative(t *testing.T) {
	for _, missing := range []string{"binary", "not runnable"} {
		t.Run(missing, func(t *testing.T) {
			f := setup(t)
			other := f.catalog.catalogs["codex"]
			other.AgentID = "other"
			f.catalog.catalogs["other"] = other
			f.request.Policy.Fallbacks = map[Role][]Selection{Worker: {{"other", "selected-model", "low"}}}
			if missing == "binary" {
				f.service.Agent = func(id string) (ports.Agent, error) {
					if id == "codex" {
						return nil, ports.ErrAgentBinaryNotFound
					}
					return f.agent, nil
				}
			} else {
				c := f.catalog.catalogs["codex"]
				c.RunnableModels = []string{"different-model"}
				f.catalog.catalogs["codex"] = c
			}
			r, err := f.service.Prepare(context.Background(), f.request)
			if !errors.Is(err, ErrUnverified) || !r.Report.FallbackUsed || r.Report.Resolved.AgentID != "other" || !slices.Contains(f.catalog.calls, "other") {
				t.Fatalf("report=%+v err=%v", r.Report, err)
			}
		})
	}
}

func TestRuntimeChangeDuringVerifierAndLateCancellationAreRejected(t *testing.T) {
	for _, stage := range []string{"verification", "final inspection"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.service.Permissions = verifierFunc(func(_ context.Context, p Probe) (CapabilityEvidence, error) {
				if stage == "verification" {
					f.runtime.Version = "updated"
				}
				return passing(p, LiveEvidence, f.now), nil
			})
			if stage == "final inspection" {
				inspections := 0
				f.service.Runtime = runtimeFunc(func(context.Context, string, ports.LaunchConfig) (RuntimeIdentity, error) {
					inspections++
					if inspections == 3 {
						cancel()
					}
					return *f.runtime, nil
				})
			}
			result, err := f.service.Prepare(ctx, f.request)
			want := ErrUnverified
			if stage == "final inspection" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || result.Report.LiveQualified {
				t.Fatalf("report=%+v err=%v", result.Report, err)
			}
		})
	}
}

func TestPromptWordsDoNotBecomeLaunchOptions(t *testing.T) {
	f := setup(t)
	f.request.Launch.Prompt = "--dangerously-bypass-approvals-and-sandbox"
	result, err := f.service.Prepare(context.Background(), f.request)
	if !errors.Is(err, ErrUnverified) || !result.Report.TestVerified {
		t.Fatalf("err=%v", err)
	}
	if result.Command[len(result.Command)-2] != "--" {
		t.Fatal("prompt not separated from options")
	}
}

func TestMissingTelemetryDoesNotEraseObservedMismatch(t *testing.T) {
	report := Report{Resolved: Selection{Model: "selected", Effort: "high"}, LiveQualified: true}
	report, err := RecordReportedIdentity(report, "other", "low")
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatal(err)
	}
	report, err = RecordReportedIdentity(report, "", "")
	if !errors.Is(err, ErrIdentityMismatch) || report.ReportedModel != "other" || report.Telemetry != "mismatch" || report.LiveQualified {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestRuntimeChangeDoesNotInvalidateAnotherAdapterBinding(t *testing.T) {
	f := setup(t)
	otherRuntime := *f.runtime
	otherRuntime.BinaryPath = filepath.Join(filepath.Dir(otherRuntime.BinaryPath), "other")
	otherAgent := &specAgent{binary: otherRuntime.BinaryPath, auth: ports.AgentAuthStatusAuthorized}
	otherCatalog := f.catalog.catalogs["codex"]
	otherCatalog.AgentID = "other"
	f.catalog.catalogs["other"] = otherCatalog
	f.service.Agent = func(id string) (ports.Agent, error) {
		if id == "other" {
			return otherAgent, nil
		}
		return f.agent, nil
	}
	f.service.Runtime = runtimeFunc(func(_ context.Context, id string, _ ports.LaunchConfig) (RuntimeIdentity, error) {
		if id == "other" {
			return otherRuntime, nil
		}
		return *f.runtime, nil
	})
	otherRequest := f.request
	otherRequest.Requested.AgentID = "other"
	first, _ := f.service.Prepare(context.Background(), f.request)
	other, _ := f.service.Prepare(context.Background(), otherRequest)
	f.runtime.Version = "updated"
	changed, _ := f.service.Prepare(context.Background(), f.request)
	unchanged, _ := f.service.Prepare(context.Background(), otherRequest)
	if changed.Report.Binding == first.Report.Binding || other.Report.Binding != unchanged.Report.Binding {
		t.Fatal("invalidated the wrong bindings")
	}
}

func TestEveryLaunchOptionInvalidatesEvidenceEvenWhenAdapterIgnoresIt(t *testing.T) {
	options := []struct {
		name   string
		change func(*ports.LaunchConfig)
	}{
		{"fast mode", func(c *ports.LaunchConfig) { c.Config.Launch.FastMode = true }},
		{"fast mode off", func(c *ports.LaunchConfig) { c.Config.Launch.FastModeOff = true }},
		{"ultracode", func(c *ports.LaunchConfig) { c.Config.Launch.Ultracode = true }},
		{"compact window", func(c *ports.LaunchConfig) { c.Config.Launch.AutoCompactWindow = "1m" }},
		{"handoff note", func(c *ports.LaunchConfig) { c.Config.Launch.HandoffNote = "changed instructions" }},
	}
	for _, option := range options {
		t.Run(option.name, func(t *testing.T) {
			f := setup(t)
			c := f.catalog.catalogs["codex"]
			c.Models[0].SupportsFast = true
			f.catalog.catalogs["codex"] = c
			before, err := f.service.Prepare(context.Background(), f.request)
			if !errors.Is(err, ErrUnverified) || !before.Report.TestVerified {
				t.Fatalf("before: %v", err)
			}
			option.change(&f.request.Launch)
			f.service.Permissions = verifierFunc(func(context.Context, Probe) (CapabilityEvidence, error) {
				e := before.Report.Evidence
				e.Mode = LiveEvidence
				return e, nil
			})
			after, err := f.service.Prepare(context.Background(), f.request)
			if !slices.Equal(before.Command, after.Command) {
				t.Fatal("regression must cover an option omitted from argv")
			}
			if !errors.Is(err, ErrUnverified) || after.Report.Binding == before.Report.Binding || after.Report.LiveQualified {
				t.Fatalf("report=%+v err=%v", after.Report, err)
			}
		})
	}
}

func TestDirectorySwapsDuringVerificationCannotAuthorizeLaunch(t *testing.T) {
	for _, mutation := range []string{"canonical workspace symlink", "original alias symlink", "workspace replacement", "coordinator replacement", "evaluator symlink", "agent data symlink"} {
		t.Run(mutation, func(t *testing.T) {
			f := setup(t)
			if mutation == "original alias symlink" {
				alias := filepath.Join(filepath.Dir(f.request.Launch.WorkspacePath), "workspace-alias")
				if err := os.Symlink(f.request.Launch.WorkspacePath, alias); err != nil {
					t.Fatal(err)
				}
				f.request.Launch.WorkspacePath = alias
			}
			oldRuntime := *f.runtime
			f.service.Permissions = verifierFunc(func(_ context.Context, p Probe) (CapabilityEvidence, error) {
				var target string
				switch mutation {
				case "coordinator replacement":
					target = f.request.Permissions.CoordinatorRoot
				case "evaluator symlink":
					target = f.request.Permissions.EvaluatorRoot
				case "agent data symlink":
					target = f.request.Launch.DataDir
				default:
					target = f.request.Launch.WorkspacePath
				}
				if mutation == "original alias symlink" {
					if err := os.Remove(target); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Rename(target, target+"-old"); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(mutation, "replacement") {
					if err := os.Mkdir(target, 0700); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(f.request.Permissions.CoordinatorRoot, target); err != nil {
					t.Fatal(err)
				}
				return passing(p, LiveEvidence, f.now), nil
			})
			result, err := f.service.Prepare(context.Background(), f.request)
			if *f.runtime != oldRuntime {
				t.Fatal("regression must retain the runtime fingerprint")
			}
			if !errors.Is(err, ErrUnverified) || result.Report.LiveQualified {
				t.Fatalf("report=%+v err=%v", result.Report, err)
			}
		})
	}
}

func TestReplacementDirectoryInvalidatesPreviouslyBoundReceipt(t *testing.T) {
	f := setup(t)
	before, err := f.service.Prepare(context.Background(), f.request)
	if !errors.Is(err, ErrUnverified) || !before.Report.TestVerified {
		t.Fatalf("before: %v", err)
	}
	path := f.request.Launch.WorkspacePath
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	f.service.Permissions = verifierFunc(func(context.Context, Probe) (CapabilityEvidence, error) {
		e := before.Report.Evidence
		e.Mode = LiveEvidence
		return e, nil
	})
	after, err := f.service.Prepare(context.Background(), f.request)
	if !errors.Is(err, ErrUnverified) || before.Report.Binding == after.Report.Binding || after.Report.LiveQualified {
		t.Fatalf("report=%+v err=%v", after.Report, err)
	}
}

func TestDirectoryContentsDoNotChangeFilesystemIdentity(t *testing.T) {
	f := setup(t)
	// Synthetic live evidence tests the admission predicate, not a real sandbox.
	f.service.Permissions = verifierFunc(func(_ context.Context, p Probe) (CapabilityEvidence, error) {
		if err := os.WriteFile(filepath.Join(p.Launch.WorkspacePath, "work.txt"), []byte("worker-owned"), 0600); err != nil {
			t.Fatal(err)
		}
		return passing(p, LiveEvidence, f.now), nil
	})
	result, err := f.service.Prepare(context.Background(), f.request)
	if err != nil || !result.Report.LiveQualified {
		t.Fatalf("report=%+v err=%v", result.Report, err)
	}
}
