package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/agentbase"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/qualification"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

func exactDigest(body []byte) string { h := sha256.Sum256(body); return hex.EncodeToString(h[:]) }

// Real planning, qualification, SQLite and session-manager code surrounds the
// fake provider/runtime here. No model, actual sandbox or provider account runs.
type exactAgent struct {
	agentbase.Base
	binary string
}

func (a exactAgent) AuthStatus(context.Context) (ports.AgentAuthStatus, error) {
	return ports.AgentAuthStatusAuthorized, nil
}
func (a exactAgent) GetLaunchCommand(_ context.Context, c ports.LaunchConfig) ([]string, error) {
	return []string{a.binary, "--model", c.Config.Model, "--effort", c.Config.Effort, "--", c.Prompt}, nil
}

type exactInspector struct{ binary string }

func (i exactInspector) Inspect(context.Context, string, ports.LaunchConfig) (qualification.RuntimeIdentity, error) {
	return qualification.RuntimeIdentity{BinaryPath: i.binary, BinarySHA256: strings.Repeat("d", 64), Version: "fixture", ConfigFingerprint: "fixture-runtime"}, nil
}

type exactVerifier struct{ now time.Time }

func (v exactVerifier) Verify(_ context.Context, p qualification.Probe) (qualification.CapabilityEvidence, error) {
	return qualification.CapabilityEvidence{Binding: p.Binding, Mode: qualification.TestEvidence, ReceiptID: "test-receipt", ExpiresAt: v.now.Add(time.Hour), Checks: qualification.CapabilityChecks{
		SettingsPreserved: qualification.Passed, CancellationObserved: qualification.Passed, ProcessContainment: qualification.Passed,
		ProtectedStorageDenied: qualification.Passed, SubmissionReadOnly: qualification.Passed,
	}}, nil
}

type exactContext struct{}

func (exactContext) Resolve(_ context.Context, _ planning.Subject, ref domain.SwarmContextRef) (planning.ApprovedContext, error) {
	return planning.ApprovedContext{ID: ref.ID, SHA256: ref.SHA256, DecisionID: "fixture-approval", DecisionVersion: 1, Body: "fixture " + ref.ID}, nil
}

type exactRuntimePort struct {
	ports.Runtime
	mu            sync.Mutex
	configs       []ports.RuntimeConfig
	ready         chan struct{}
	dead, unknown bool
}

func (r *exactRuntimePort) Create(ctx context.Context, c ports.RuntimeConfig) (ports.RuntimeHandle, error) {
	r.mu.Lock()
	r.configs = append(r.configs, c)
	if len(r.configs) == 2 && r.ready != nil {
		close(r.ready)
	}
	r.mu.Unlock()
	if r.ready != nil {
		select {
		case <-r.ready:
		case <-ctx.Done():
			return ports.RuntimeHandle{}, ctx.Err()
		}
	}
	return ports.RuntimeHandle{ID: string(c.SessionID)}, nil
}
func (r *exactRuntimePort) ProbeFencedRuntime(_ context.Context, ref ports.FencedRuntimeRef) ports.FencedProbeResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.configs {
		if c.SessionID == ref.SessionID && string(c.SessionID) == ref.Handle.ID && c.Env["AO_RUNTIME_LAUNCH_ID"] == ref.Generation && !r.unknown {
			if r.dead {
				return ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}
			}
			return ports.FencedProbeResult{Liveness: ports.FencedAlive, Reason: ports.FencedReasonExactMatch}
		}
	}
	return ports.FencedProbeResult{Liveness: ports.FencedUnknown, Reason: ports.FencedReasonIdentityMissing}
}

func exactFixture(t *testing.T) (*schedulerFixture, *ExactRuntime, *exactRuntimePort) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "test-tool")
	body := []byte("fixture tool; never executed")
	if err = os.WriteFile(binary, body, 0700); err != nil {
		t.Fatal(err)
	}
	f := newSchedulerFixture(t, func(plan *domain.SwarmPlan) {
		for i := range plan.Tasks {
			plan.Tasks[i].Work = &domain.SwarmWorkSpec{
				Version: 1, TaskType: "implementation", ContextByteLimit: planning.MaxPacketBytes,
				NonGoals: []string{"No unrelated changes"}, Interfaces: []domain.SwarmInterface{{ID: "write", Contract: "write(value)", Ownership: "Assigned file only", ErrorRuleIDs: []string{"invalid"}}},
				Errors:       []domain.SwarmErrorRule{{ID: "invalid", When: "value invalid", Signal: "reject", Value: "INVALID_VALUE", Destination: "unchanged", Effect: "Reject before mutation"}},
				Acceptance:   domain.SwarmAcceptance{OracleID: "fixture-oracle", Checks: []domain.SwarmAcceptanceCheck{{ID: "invalid", Command: []string{binary}, ExpectedResult: "Invalid value leaves destination unchanged", ErrorRuleID: "invalid"}}},
				Context:      []domain.SwarmContextRef{{ID: "spec", Kind: "spec", SHA256: exactDigest([]byte("fixture spec")), Required: true}, {ID: "source", Kind: "source_manifest", SHA256: exactDigest([]byte("fixture source")), Required: true}},
				Tools:        []domain.SwarmToolIdentity{{Name: "test-tool", Path: binary, SHA256: exactDigest(body), Version: "fixture"}},
				ProgressPath: "docs/progress.md", OnComplete: "submit_and_stop", OnBlocked: "report_and_stop",
			}
		}
	})
	for _, name := range []string{"coordinator", "evaluator", "agent-data"} {
		if err = os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	requests := func(_ context.Context, a domain.SwarmAttempt, w workspace.Snapshot) (qualification.Request, error) {
		return qualification.Request{Role: qualification.Worker, Requested: qualification.Selection{AgentID: "codex", Model: "worker", Effort: "low"}, Scope: "account",
			Launch:      ports.LaunchConfig{Kind: domain.KindWorker, WorkspacePath: w.Destination.Path, DataDir: filepath.Join(dir, "agent-data"), Permissions: ports.PermissionModeAuto},
			Permissions: qualification.PermissionRequirements{CoordinatorRoot: filepath.Join(dir, "coordinator"), EvaluatorRoot: filepath.Join(dir, "evaluator")}}, nil
	}
	q := qualification.Service{Catalog: fixtureCatalog{f.now}, Agent: func(string) (ports.Agent, error) { return exactAgent{binary: binary}, nil }, Runtime: exactInspector{binary}, Permissions: exactVerifier{f.now}, Now: func() time.Time { return f.now }}
	for i := range f.candidates {
		f.candidates[i].Admission.Scope = "account"
		req, _ := requests(context.Background(), domain.SwarmAttempt{}, f.candidates[i].Workspace)
		preflight, e := q.Prepare(context.Background(), req)
		if !errors.Is(e, qualification.ErrUnverified) || !preflight.Report.TestVerified {
			t.Fatalf("preflight: %v", e)
		}
		f.candidates[i].Admission.Claim.CapabilityFingerprint = preflight.Report.CapabilityFingerprint
	}
	composer := planning.Service{Store: f.store, Context: exactContext{}, Tools: planning.FileToolVerifier{}, Now: func() time.Time { return f.now }}
	preparer := ExactPreparer{Qualification: q, Requests: requests}
	port := &exactRuntimePort{}
	runtime := &ExactRuntime{Launcher: sessionmanager.SwarmExactLauncher{Store: f.store, Runtime: port}, Preparer: preparer, Composer: composer, Now: func() time.Time { return f.now }}
	f.svc.Composer, f.svc.Preparer, f.svc.Runtime = composer, preparer, runtime
	return f, runtime, port
}

func TestExactRuntimeComposesQualifiesAndPersistsParallelLaunches(t *testing.T) {
	f, runtime, port := exactFixture(t)
	port.ready = make(chan struct{})
	ctx := context.Background()
	results, err := f.svc.Tick(ctx, f.candidates[:2])
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Err != nil {
			a, _ := f.store.GetSwarmAttempt(ctx, result.Attempt.ID)
			t.Fatalf("attempt %s state %s: %v", a.ID, a.State, result.Err)
		}
		a, e := f.store.GetSwarmAttempt(ctx, result.Attempt.ID)
		if e != nil {
			t.Fatal(e)
		}
		if a.State != "running" || a.ReportedModel != "" {
			t.Fatalf("state/telemetry: %+v", a)
		}
		receipt, e := f.store.GetSwarmExecution(ctx, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		if receipt.CapabilityFingerprint == receipt.QualificationBinding || receipt.EvidenceMode != "test" {
			t.Fatal("circular binding or false live claim")
		}
		p, e := f.svc.Composer.Compose(ctx, a.RunID, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		if p.SHA256 != receipt.PacketHash {
			t.Fatal("launch did not bind real planning packet")
		}
		stored, e := f.store.GetSwarmExecutionResult(ctx, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		if stored.ReceiptHash != domain.SwarmHash(receipt) || stored.Outcome != "created" {
			t.Fatal("missing persisted launch outcome")
		}
		obs, e := runtime.Observe(ctx, a)
		if e != nil || obs.State != "alive" || !obs.Exact {
			t.Fatalf("observation %+v %v", obs, e)
		}
	}
	if len(port.configs) != 2 || port.configs[0].WorkspacePath == port.configs[1].WorkspacePath || port.configs[0].SessionID == port.configs[1].SessionID {
		t.Fatal("launches were not independently bound")
	}
	for _, c := range port.configs {
		if !strings.Contains(c.Argv[len(c.Argv)-1], "# Current assignment") || c.Argv[2] != "worker" || c.Argv[4] != "low" {
			t.Fatal("task/settings changed at runtime boundary")
		}
	}
	if _, err = f.svc.Tick(ctx, f.candidates[:2]); err != nil {
		t.Fatal(err)
	}
	if len(port.configs) != 2 {
		t.Fatal("replay launched again")
	}
	port.unknown = true
	if err = f.svc.Reconcile(ctx, results[0].Attempt.ID); !errors.Is(err, qualification.ErrUnverified) {
		t.Fatal(err)
	}
	a, _ := f.store.GetSwarmAttempt(ctx, results[0].Attempt.ID)
	if a.State != "running" {
		t.Fatal("unknown probe released ownership")
	}
	port.unknown, port.dead = false, true
	if err = f.svc.Reconcile(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	a, _ = f.store.GetSwarmAttempt(ctx, a.ID)
	if a.State != "failed" {
		t.Fatalf("terminal test process state: %s", a.State)
	}
	if data, e := os.ReadFile(filepath.Join(f.source, "a")); e != nil || string(data) != "preserved dirty input" {
		t.Fatal("source changed")
	}
}

func TestExactRuntimeReplayKeepsOriginalReceiptAfterExpiry(t *testing.T) {
	f, runtime, port := exactFixture(t)
	ctx := context.Background()
	results, err := f.svc.Tick(ctx, f.candidates[:1])
	if err != nil || len(results) != 1 || results[0].Err != nil {
		t.Fatalf("initial dispatch: %+v %v", results, err)
	}
	a, err := f.store.GetSwarmAttempt(ctx, results[0].Attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := f.svc.Composer.Compose(ctx, a.RunID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.Preparer.Prepare(ctx, a, packet, f.candidates[0].Workspace)
	if !errors.Is(err, qualification.ErrUnverified) {
		t.Fatal(err)
	}
	launch := Launch{a, packet, f.candidates[0].Workspace, p}
	f.now = f.now.Add(2 * time.Hour)
	if err = os.WriteFile(filepath.Join(launch.Workspace.Destination.Path, "a"), []byte("worker result"), 0600); err != nil {
		t.Fatal(err)
	}
	obs, err := runtime.Start(ctx, launch)
	if err != nil || obs.State != "alive" || len(port.configs) != 1 {
		t.Fatalf("replay: %+v %v starts=%d", obs, err, len(port.configs))
	}
	launch.Preparation.Command = append([]string{}, p.Command...)
	launch.Preparation.Command[1] = "--different"
	if _, err = runtime.Start(ctx, launch); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	if len(port.configs) != 1 {
		t.Fatal("changed replay relaunched")
	}
}

func TestExactLaunchRejectsFinalPromptDeliveryAndSnapshotDrift(t *testing.T) {
	f, _, _ := exactFixture(t)
	c := f.candidates[0]
	ctx := context.Background()
	if err := f.svc.bindCandidate(ctx, &c); err != nil {
		t.Fatal(err)
	}
	a, _, err := f.svc.Admission.Admit(ctx, c.Admission)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := f.svc.Composer.Compose(ctx, a.RunID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.Preparer.Prepare(ctx, a, packet, c.Workspace)
	if !errors.Is(err, qualification.ErrUnverified) {
		t.Fatal(err)
	}
	base := Launch{a, packet, c.Workspace, p}
	wrongScope := f.svc.Preparer.(ExactPreparer)
	originalRequests := wrongScope.Requests
	wrongScope.Requests = func(ctx context.Context, a domain.SwarmAttempt, w workspace.Snapshot) (qualification.Request, error) {
		req, err := originalRequests(ctx, a, w)
		req.Scope = "different-account"
		return req, err
	}
	if _, err := wrongScope.Prepare(ctx, a, packet, c.Workspace); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("scope changed after admission: %v", err)
	}
	for name, mutate := range map[string]func(*Launch){
		"prompt":    func(l *Launch) { l.Preparation.Launch.Prompt = "other" },
		"delivery":  func(l *Launch) { l.Preparation.PromptDelivery = ports.PromptDeliveryAfterStart },
		"profile":   func(l *Launch) { l.Preparation.Report.CapabilityFingerprint = strings.Repeat("e", 64) },
		"live":      func(l *Launch) { l.Preparation.Report.Evidence.Mode = qualification.LiveEvidence },
		"workspace": func(l *Launch) { l.Preparation.Launch.WorkspacePath += "-other" },
		"source":    func(l *Launch) { l.Workspace.ManifestHash = strings.Repeat("e", 64) },
		"session":   func(l *Launch) { l.Preparation.Launch.SessionID += "-other" },
		"command":   func(l *Launch) { l.Preparation.Command = []string{"/other/binary"} },
	} {
		t.Run(name, func(t *testing.T) {
			l := base
			mutate(&l)
			if _, _, err := exactLaunch(l, f.now); err == nil {
				t.Fatal("changed launch accepted")
			}
		})
	}
	receipt, cfg, err := exactLaunch(base, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ConfigHash != domain.SwarmHash(cfg) || !reflect.DeepEqual(p.Command, cfg.Argv) {
		t.Fatal("final command omitted")
	}
}
