package scheduler

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/qualification"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/routing"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type fixtureCatalog struct{ now time.Time }

func (c fixtureCatalog) RevalidateModels(context.Context, string, string) (ports.AgentModelCatalog, error) {
	return ports.AgentModelCatalog{AgentID: "codex", SelectionMode: ports.ModelSelectionCatalog, InputFingerprint: "fixture-catalog", FetchedAt: c.now, ValidatedAt: c.now, Models: []ports.AgentModelInfo{{ID: "worker", ContextWindow: 10000, Efforts: []string{"low"}, DefaultEffort: "low"}}}, nil
}

// Packet behavior is independently tested by the planning package; this double
// exposes its fresh-validation boundary without inventing live provider evidence.
type fixtureComposer struct {
	store    *sqlite.Store
	onResume func()
}

func (c *fixtureComposer) Compose(ctx context.Context, run, id string) (planning.Packet, error) {
	a, e := c.store.GetSwarmAttempt(ctx, id)
	return planning.Packet{RunID: run, AttemptID: id, OperationID: a.OperationID, Resolved: a.Resolved}, e
}
func (c *fixtureComposer) ValidateResume(context.Context, planning.Packet) error {
	if c.onResume != nil {
		c.onResume()
	}
	return nil
}

type fixturePreparer struct {
	now  time.Time
	mode qualification.EvidenceMode
}

func (p fixturePreparer) Prepare(_ context.Context, a domain.SwarmAttempt, _ planning.Packet, w workspace.Snapshot) (qualification.Preparation, error) {
	return qualification.Preparation{Launch: ports.LaunchConfig{WorkspacePath: w.Destination.Path, Config: ports.AgentConfig{Model: a.Resolved.Model, Effort: a.Resolved.Effort}}, Report: qualification.Report{
		CapabilityFingerprint: a.CapabilityFingerprint, Binding: a.CapabilityFingerprint, Resolved: qualification.Selection{AgentID: string(a.Resolved.Harness), Model: a.Resolved.Model, Effort: a.Resolved.Effort}, TestVerified: true,
		Evidence: qualification.CapabilityEvidence{Binding: a.CapabilityFingerprint, ReceiptID: "test-only", Mode: p.mode, ExpiresAt: p.now.Add(time.Minute), Checks: qualification.CapabilityChecks{SettingsPreserved: qualification.Passed, CancellationObserved: qualification.Passed, ProcessContainment: qualification.Passed, ProtectedStorageDenied: qualification.Passed, SubmissionReadOnly: qualification.Passed}},
	}}, qualification.ErrUnverified
}

type child struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	session string
}
type processRuntime struct {
	mu        sync.Mutex
	children  map[string]*child
	unknown   bool
	ambiguous bool
	mode      qualification.EvidenceMode
	starts    int
}

func (r *processRuntime) Mode() qualification.EvidenceMode { return r.mode }
func (r *processRuntime) Start(_ context.Context, l Launch) (Observation, error) {
	// Local controllable helper processes, not models and not a sandbox test.
	cmd := exec.Command("/bin/sh", "-c", "printf worker > result.txt; cat >/dev/null")
	cmd.Dir = l.Workspace.Destination.Path
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return Observation{}, e
	}
	if e = cmd.Start(); e != nil {
		_ = stdin.Close()
		return Observation{}, e
	}
	c := &child{cmd: cmd, stdin: stdin, done: make(chan struct{}), session: "session-" + l.Attempt.ID}
	r.mu.Lock()
	r.children[l.Attempt.OperationID] = c
	r.starts++
	ambiguous := r.ambiguous
	r.mu.Unlock()
	go func() { _ = cmd.Wait(); close(c.done) }()
	if ambiguous {
		return Observation{}, errors.New("simulated lost launch response")
	}
	return Observation{OperationID: l.Attempt.OperationID, SessionID: c.session, State: "alive", Exact: true}, nil
}
func (r *processRuntime) Observe(_ context.Context, a domain.SwarmAttempt) (Observation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unknown {
		return Observation{OperationID: a.OperationID, State: "unknown"}, nil
	}
	c := r.children[a.OperationID]
	if c == nil {
		return Observation{OperationID: a.OperationID, State: "unknown"}, nil
	}
	state := "alive"
	select {
	case <-c.done:
		state = "absent"
	default:
	}
	// Only Wait on the exact owned child closes done. A missing registry entry
	// above remains unknown; it can never assert that a pending start is final.
	return Observation{OperationID: a.OperationID, SessionID: c.session, State: state, Exact: true, Terminal: state == "absent"}, nil
}
func (r *processRuntime) stopAll(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	children := make([]*child, 0, len(r.children))
	for _, c := range r.children {
		children = append(children, c)
	}
	r.mu.Unlock()
	for _, c := range children {
		_ = c.stdin.Close()
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
			_ = c.cmd.Process.Kill()
			<-c.done
			t.Error("fixture child required forced cleanup")
		}
	}
}

type schedulerFixture struct {
	store      *sqlite.Store
	svc        *Service
	runtime    *processRuntime
	composer   *fixtureComposer
	candidates []Candidate
	now        time.Time
	source     string
}

func newSchedulerFixture(t *testing.T, configure ...func(*domain.SwarmPlan)) *schedulerFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := sqlitetest.MustOpen(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = source
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=0")
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %v %s", args, e, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-q")
	for _, name := range []string{"a", "b", "c"} {
		if e := os.WriteFile(filepath.Join(source, name), []byte("initial"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture")
	head := git("rev-parse", "HEAD")
	if e := os.WriteFile(filepath.Join(source, "a"), []byte("preserved dirty input"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.UpsertProject(ctx, domain.ProjectRecord{ID: "p", Path: source, RegisteredAt: now}); e != nil {
		t.Fatal(e)
	}
	sel := domain.SwarmSelection{Harness: "codex", Model: "worker", Effort: "low"}
	plan := domain.SwarmPlan{ProtocolVersion: 1, Version: 1, ProjectID: "p", SourceCommit: head, Objective: "local scheduler test", BudgetBucket: "budget", TokenLimit: 10000, MaxActive: 3}
	for _, id := range []string{"a", "b", "c"} {
		plan.Tasks = append(plan.Tasks, domain.SwarmTaskContract{ID: id, Version: 1, Objective: "edit " + id, OracleHash: strings.Repeat("a", 64), Agent: sel, ReserveTokens: 100, Resources: []domain.SwarmResource{{Namespace: "repo", Kind: "path", Key: id}}})
	}
	plan.Tasks[2].Dependencies = []domain.SwarmDependency{{TaskID: "a", Version: 1}}
	for _, setup := range configure {
		setup(&plan)
	}
	if _, e := s.CreateSwarmRun(ctx, "run", "create", plan, now); e != nil {
		t.Fatal(e)
	}
	policy := domain.SwarmRoutingPolicy{ID: "routes", AccountID: "account", Bucket: "budget", PremiumTokenLimit: 1000, Models: []domain.SwarmRouteModel{{Harness: "codex", Model: "worker", Roles: []string{"worker"}, QuotaScope: "worker", MinEffort: "low", MaxEffort: "low"}}}
	if e := s.ConfigureSwarmRouting(ctx, policy); e != nil {
		t.Fatal(e)
	}
	limit, used := int64(10000), int64(0)
	q := domain.SwarmQuotaSnapshot{ID: "quota", Bucket: "budget", Revision: 1, ObservedAt: now, ExpiresAt: now.Add(time.Hour), Windows: []domain.SwarmQuotaWindow{{ID: "window", Scope: "worker", LimitTokens: &limit, UsedTokens: &used, ResetAt: now.Add(time.Hour)}}}
	if e := s.UpdateSwarmQuota(ctx, q); e != nil {
		t.Fatal(e)
	}
	pool := domain.SwarmSchedulerPolicy{PoolID: "pool", MaxWorkers: 2, MaxReviewers: 1, ProviderLimits: map[string]int64{"account": 3}, ProjectLimits: map[string]int64{"p": 3}}
	if e := s.ConfigureSwarmScheduler(ctx, pool); e != nil {
		t.Fatal(e)
	}
	r := &processRuntime{children: map[string]*child{}, mode: qualification.TestEvidence}
	t.Cleanup(func() { r.stopAll(t) })
	composer := &fixtureComposer{store: s}
	f := &schedulerFixture{store: s, runtime: r, composer: composer, now: now, source: source}
	f.svc = &Service{Owner: "coordinator", Store: s, Admission: routing.Service{Store: s, Catalog: fixtureCatalog{now}, Now: func() time.Time { return now }}, Composer: composer, Preparer: fixturePreparer{now, qualification.TestEvidence}, Runtime: r, Now: func() time.Time { return now }, OperationTimeout: 10 * time.Second, MaxPerPass: 3}
	for _, id := range []string{"a", "b", "c"} {
		snapshot, e := workspace.Capture(ctx, workspace.CaptureRequest{SourceRoot: source, Destination: filepath.Join(root, "worker-"+id)})
		if e != nil {
			t.Fatal(e)
		}
		f.candidates = append(f.candidates, Candidate{Admission: routing.AdmissionRequest{Claim: domain.SwarmClaim{RunID: "run", TaskID: id, Version: 1, RequestID: "request-" + id, Owner: "coordinator", CapabilityFingerprint: strings.Repeat("b", 64), Schedule: &domain.SwarmScheduleBinding{PoolID: "pool", PolicyHash: domain.SwarmHash(pool), ProviderKey: "account"}}, AttemptID: "attempt-" + id, OperationID: "operation-" + id, ContextTokens: 100, LeaseUntil: now.Add(time.Hour)}, Workspace: snapshot})
	}
	return f
}

func TestSchedulerOverlappingProcessesIndependentCopiesAndDependency(t *testing.T) {
	f := newSchedulerFixture(t)
	ctx := context.Background()
	// Put the blocked dependency first: it must not stop independent work.
	results, e := f.svc.Tick(ctx, []Candidate{f.candidates[2], f.candidates[0], f.candidates[1]})
	if e != nil {
		t.Fatal(e)
	}
	if len(results) != 3 || !errors.Is(results[0].Err, domain.ErrSwarmNotReady) || results[1].Err != nil || results[2].Err != nil {
		t.Fatalf("results=%+v", results)
	}
	f.runtime.mu.Lock()
	defer f.runtime.mu.Unlock()
	if f.runtime.starts != 2 {
		t.Fatal(f.runtime.starts)
	}
	for _, c := range f.runtime.children {
		select {
		case <-c.done:
			t.Fatal("worker did not overlap other live worker")
		default:
		}
	}
	for _, c := range f.candidates[:2] {
		a, e := f.store.GetSwarmAttempt(ctx, c.Admission.AttemptID)
		if e != nil || a.State != "running" || a.Schedule == nil {
			t.Fatalf("%+v %v", a, e)
		}
		if a.Schedule.WorkspaceRoot != c.Workspace.Destination.Path {
			t.Fatal("workspace identity lost")
		}
	}
	data, e := os.ReadFile(filepath.Join(f.source, "a"))
	if e != nil || string(data) != "preserved dirty input" {
		t.Fatal("source changed", e)
	}
}

func TestSchedulerAmbiguousLaunchRetainsSlotAndReconcilesWithoutRelaunch(t *testing.T) {
	f := newSchedulerFixture(t)
	ctx := context.Background()
	f.runtime.ambiguous = true
	results, e := f.svc.Tick(ctx, f.candidates[:1])
	if e != nil || results[0].Err == nil {
		t.Fatal(results, e)
	}
	a, e := f.store.GetSwarmAttempt(ctx, "attempt-a")
	if e != nil || a.State != "dispatching" {
		t.Fatal(a, e)
	}
	if e = f.svc.Dispatch(ctx, a.ID, f.candidates[0].Workspace); !errors.Is(e, domain.ErrSwarmNotReady) {
		t.Fatal(e)
	}
	f.runtime.unknown = true
	if e = f.svc.Reconcile(ctx, a.ID); !errors.Is(e, qualification.ErrUnverified) {
		t.Fatal(e)
	}
	a, _ = f.store.GetSwarmAttempt(ctx, a.ID)
	if a.State != "dispatching" || f.runtime.starts != 1 {
		t.Fatal(a)
	}
	f.runtime.unknown = false
	if e = f.svc.Reconcile(ctx, a.ID); e != nil {
		t.Fatal(e)
	}
	a, _ = f.store.GetSwarmAttempt(ctx, a.ID)
	if a.State != "running" {
		t.Fatal(a)
	}
	f.runtime.stopAll(t)
	if e = f.store.CancelSwarmRun(ctx, "run"); e != nil {
		t.Fatal(e)
	}
	if e = f.svc.Reconcile(ctx, a.ID); e != nil {
		t.Fatal(e)
	}
	a, _ = f.store.GetSwarmAttempt(ctx, a.ID)
	if a.State != "cancelled" {
		t.Fatal(a)
	}
	budget, _ := f.store.GetSwarmBudget(ctx, "budget")
	if budget.Reserved != 100 {
		t.Fatal("unknown usage freed", budget)
	}
}

func TestSchedulerRejectsTestEvidenceForLiveRuntime(t *testing.T) {
	f := newSchedulerFixture(t)
	f.runtime.mode = qualification.LiveEvidence
	results, e := f.svc.Tick(context.Background(), f.candidates[:1])
	if len(results) != 0 || !errors.Is(e, qualification.ErrUnverified) || !errors.Is(e, ErrAOExactWorkspaceUnsupported) || f.runtime.starts != 0 {
		t.Fatal(results, e)
	}
	assertSchedulerNoReservations(t, f)
}

type blockingStartRuntime struct {
	Runtime
	entered  chan struct{}
	release  chan struct{}
	mu       sync.Mutex
	observes int
}

func (r *blockingStartRuntime) Start(ctx context.Context, launch Launch) (Observation, error) {
	close(r.entered)
	select {
	case <-r.release:
		return r.Runtime.Start(ctx, launch)
	case <-ctx.Done():
		return Observation{}, ctx.Err()
	}
}
func (r *blockingStartRuntime) Observe(_ context.Context, a domain.SwarmAttempt) (Observation, error) {
	r.mu.Lock()
	r.observes++
	r.mu.Unlock()
	// Deliberately unsafe observation: exclusion must prevent calling this
	// while a local Start can still acquire a process after reconciliation.
	return Observation{OperationID: a.OperationID, State: "absent", Exact: true, Terminal: true}, nil
}

func TestSchedulerReconcileCannotReleaseAnInFlightStart(t *testing.T) {
	f := newSchedulerFixture(t)
	r := &blockingStartRuntime{Runtime: f.runtime, entered: make(chan struct{}), release: make(chan struct{})}
	f.svc.Runtime = r
	release := sync.OnceFunc(func() { close(r.release) })
	done := make(chan []Result, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		results, err := f.svc.Tick(context.Background(), f.candidates[:1])
		if err != nil {
			results = append(results, Result{Err: err})
		}
		done <- results
	}()
	t.Cleanup(func() {
		release()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("pending test dispatch failed to settle")
		}
	})
	select {
	case <-r.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Start was not reached")
	}
	if err := f.svc.Reconcile(context.Background(), "attempt-a"); !errors.Is(err, domain.ErrSwarmNotReady) {
		t.Fatalf("in-flight reconciliation: %v", err)
	}
	a, err := f.store.GetSwarmAttempt(context.Background(), "attempt-a")
	if err != nil || a.State != "dispatching" {
		t.Fatalf("ownership released before start: %+v %v", a, err)
	}
	r.mu.Lock()
	observed := r.observes
	r.mu.Unlock()
	if observed != 0 {
		t.Fatal("runtime absence was queried while Start was pending")
	}
	release()
	select {
	case results := <-done:
		if len(results) != 1 || results[0].Err != nil {
			t.Fatalf("start completion: %+v", results)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not complete")
	}
	a, err = f.store.GetSwarmAttempt(context.Background(), "attempt-a")
	if err != nil || a.State != "running" || f.runtime.starts != 1 {
		t.Fatalf("launch ownership lost: %+v %v", a, err)
	}
}

type observingRuntime struct {
	Runtime
	observe func(context.Context, domain.SwarmAttempt) (Observation, error)
}

func (r observingRuntime) Observe(ctx context.Context, a domain.SwarmAttempt) (Observation, error) {
	return r.observe(ctx, a)
}

func TestSchedulerRequiresTerminalAbsenceBeforeReleasingOwnership(t *testing.T) {
	f := newSchedulerFixture(t)
	ctx := context.Background()
	results, err := f.svc.Tick(ctx, f.candidates[:1])
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	f.svc.Runtime = observingRuntime{Runtime: f.runtime, observe: func(_ context.Context, a domain.SwarmAttempt) (Observation, error) {
		return Observation{OperationID: a.OperationID, SessionID: a.SessionID, State: "absent", Exact: true}, nil
	}}
	if err = f.svc.Reconcile(ctx, "attempt-a"); !errors.Is(err, qualification.ErrUnverified) {
		t.Fatal(err)
	}
	a, err := f.store.GetSwarmAttempt(ctx, "attempt-a")
	if err != nil || a.State != "running" {
		t.Fatalf("nonterminal absence released ownership: %+v %v", a, err)
	}
	f.runtime.stopAll(t)
	f.svc.Runtime = f.runtime
	if err = f.svc.Reconcile(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	a, err = f.store.GetSwarmAttempt(ctx, a.ID)
	if err != nil || a.State != "failed" {
		t.Fatalf("terminal absence not settled: %+v %v", a, err)
	}
}

func TestSchedulerRunningReplayDoesNotConsumePassAdmission(t *testing.T) {
	f := newSchedulerFixture(t)
	ctx := context.Background()
	f.svc.MaxPerPass = 1
	results, err := f.svc.Tick(ctx, f.candidates[:1])
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	// The worker is entitled to edit its copy. Replay must bypass the pristine
	// prelaunch filesystem check and still permit the next task in this project.
	if err = os.WriteFile(filepath.Join(f.candidates[0].Workspace.Destination.Path, "a"), []byte("worker edit"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err = f.svc.Tick(ctx, f.candidates[:2])
	if err != nil || len(results) != 2 || results[0].Err != nil || !results[0].Advice.Replayed || results[0].Attempt.State != "running" || results[1].Err != nil || f.runtime.starts != 2 {
		t.Fatalf("replay blocked new work: %+v %v", results, err)
	}
}

func TestSchedulerReplaySurvivesSourceDriftAndCompletedWorkspaceEdits(t *testing.T) {
	f := newSchedulerFixture(t)
	ctx := context.Background()
	results, err := f.svc.Tick(ctx, f.candidates[:1])
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	if err = os.WriteFile(filepath.Join(f.source, "a"), []byte("later source edit"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"running", "failed"} {
		if state == "failed" {
			f.runtime.stopAll(t)
			if err = f.svc.Reconcile(ctx, "attempt-a"); err != nil {
				t.Fatal(err)
			}
		}
		results, err = f.svc.Tick(ctx, f.candidates[:1])
		if err != nil || len(results) != 1 || results[0].Err != nil || !results[0].Advice.Replayed || results[0].Attempt.State != state || f.runtime.starts != 1 {
			t.Fatalf("%s replay lost: %+v %v", state, results, err)
		}
	}
}

func reserveSchedulerCandidate(t *testing.T, f *schedulerFixture, index int) domain.SwarmAttempt {
	t.Helper()
	c := f.candidates[index]
	ctx := context.Background()
	if err := f.svc.bindWorkspace(ctx, &c); err != nil {
		t.Fatal(err)
	}
	a, _, err := f.svc.Admission.Admit(ctx, c.Admission)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSchedulerReplayOwnerRequiresExplicitTakeover(t *testing.T) {
	f := newSchedulerFixture(t)
	ctx := context.Background()
	a := reserveSchedulerCandidate(t, f, 0)
	f.runtime.mode = ""
	if err := f.svc.Reconcile(ctx, a.ID); !errors.Is(err, qualification.ErrUnverified) {
		t.Fatalf("unclassified runtime reconciled: %v", err)
	}
	f.runtime.mode = qualification.TestEvidence
	c := f.candidates[0]
	c.Admission.Claim.Owner = "replacement"
	f.svc.Owner = "replacement"
	if err := f.svc.Dispatch(ctx, a.ID, c.Workspace); !errors.Is(err, domain.ErrSwarmStale) {
		t.Fatalf("direct foreign dispatch: %v", err)
	}
	if err := f.svc.Reconcile(ctx, a.ID); !errors.Is(err, domain.ErrSwarmStale) {
		t.Fatalf("direct foreign reconciliation: %v", err)
	}
	results, err := f.svc.Tick(ctx, []Candidate{c})
	if err != nil || len(results) != 1 || !errors.Is(results[0].Err, domain.ErrSwarmStale) || f.runtime.starts != 0 {
		t.Fatalf("foreign owner launched: %+v %v", results, err)
	}
	future := f.now.Add(2 * time.Hour)
	if _, err = f.store.TakeOverSwarmLease(ctx, fence(a), "replacement", future, future.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.svc.Now = func() time.Time { return future }
	f.svc.Preparer = fixturePreparer{future, qualification.TestEvidence}
	results, err = f.svc.Tick(ctx, []Candidate{c})
	if err != nil || len(results) != 1 || results[0].Err != nil || f.runtime.starts != 1 {
		t.Fatalf("explicit takeover failed: %+v %v", results, err)
	}
}

type changedBaseStore struct {
	Store
	base string
}

func (s changedBaseStore) GetSwarmAttempt(ctx context.Context, id string) (domain.SwarmAttempt, error) {
	a, err := s.Store.GetSwarmAttempt(ctx, id)
	a.BaseCommit = s.base
	return a, err
}
func TestSchedulerRejectsSnapshotOlderThanAtomicClaimBase(t *testing.T) {
	f := newSchedulerFixture(t)
	a := reserveSchedulerCandidate(t, f, 0)
	// Models integration-head advancement after bindWorkspace and before the
	// admission transaction freezes the attempt's base, without altering files.
	f.svc.Store = changedBaseStore{Store: f.store, base: strings.Repeat("f", 40)}
	if err := f.svc.Dispatch(context.Background(), a.ID, f.candidates[0].Workspace); !errors.Is(err, domain.ErrSwarmStale) {
		t.Fatal(err)
	}
	if f.runtime.starts != 0 {
		t.Fatal("stale source started")
	}
	a, err := f.store.GetSwarmAttempt(context.Background(), a.ID)
	if err != nil || a.State != "reserved" {
		t.Fatal(a, err)
	}
}

type purportedLivePreparer struct{ fixturePreparer }

func (p purportedLivePreparer) Prepare(ctx context.Context, a domain.SwarmAttempt, packet planning.Packet, w workspace.Snapshot) (qualification.Preparation, error) {
	prepared, _ := p.fixturePreparer.Prepare(ctx, a, packet, w)
	prepared.Report.TestVerified = false
	prepared.Report.LiveQualified = true
	prepared.Report.Evidence.Mode = qualification.LiveEvidence
	return prepared, nil
}
func TestSchedulerLiveGateRejectsPurportedPassingLiveReceipts(t *testing.T) {
	f := newSchedulerFixture(t)
	f.runtime.mode = qualification.LiveEvidence
	f.svc.Preparer = purportedLivePreparer{fixturePreparer{f.now, qualification.LiveEvidence}}
	results, err := f.svc.Tick(context.Background(), f.candidates[:1])
	if len(results) != 0 || !errors.Is(err, ErrAOExactWorkspaceUnsupported) || f.runtime.starts != 0 {
		t.Fatalf("live boundary bypassed: %+v %v", results, err)
	}
	assertSchedulerNoReservations(t, f)
	if err = f.svc.Dispatch(context.Background(), "attempt-a", f.candidates[0].Workspace); !errors.Is(err, ErrAOExactWorkspaceUnsupported) {
		t.Fatalf("direct live dispatch bypassed: %v", err)
	}
}

func assertSchedulerNoReservations(t *testing.T, f *schedulerFixture) {
	t.Helper()
	attempts, err := f.store.ListSwarmAttempts(context.Background(), "run")
	if err != nil || len(attempts) != 0 {
		t.Fatalf("unavailable dispatch admitted attempts: %+v %v", attempts, err)
	}
	budget, err := f.store.GetSwarmBudget(context.Background(), "budget")
	if err != nil || budget.Reserved != 0 {
		t.Fatalf("unavailable dispatch consumed budget: %+v %v", budget, err)
	}
}

func TestSchedulerMissingExecutionDependenciesCannotAdmit(t *testing.T) {
	f := newSchedulerFixture(t)
	f.svc.Runtime = nil
	if _, err := f.svc.Tick(context.Background(), f.candidates[:1]); !errors.Is(err, domain.ErrSwarmInvalid) {
		t.Fatal(err)
	}
	assertSchedulerNoReservations(t, f)
}

func TestSchedulerRechecksWorkspaceAfterPreparation(t *testing.T) {
	f := newSchedulerFixture(t)
	f.composer.onResume = func() {
		if e := os.WriteFile(filepath.Join(f.candidates[0].Workspace.Destination.Path, "a"), []byte("changed while preparing"), 0600); e != nil {
			t.Error(e)
		}
	}
	results, e := f.svc.Tick(context.Background(), f.candidates[:1])
	if e != nil || results[0].Err == nil || f.runtime.starts != 0 {
		t.Fatal(results, e)
	}
}

func TestSchedulerFairOrderRotatesAndInterleavesProjects(t *testing.T) {
	c := func(id string) Candidate {
		return Candidate{Admission: routing.AdmissionRequest{Claim: domain.SwarmClaim{RequestID: id}}}
	}
	groups := map[string][]Candidate{"a": {c("a1"), c("a2")}, "b": {c("b1"), c("b2")}, "c": {c("c1")}}
	ordered := fairOrder(groups, "a")
	var ids []string
	for _, entry := range ordered {
		ids = append(ids, entry.candidate.Admission.Claim.RequestID)
	}
	if strings.Join(ids, ",") != "b1,c1,a1,b2,a2" {
		t.Fatal(ids)
	}
}
