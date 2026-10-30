package store_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

var swarmNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
var swarmSelection = domain.SwarmSelection{Harness: domain.HarnessFake, Model: "test-model", Effort: "medium"}

func swarmPlan() domain.SwarmPlan {
	return domain.SwarmPlan{ProtocolVersion: 1, Version: 1, ProjectID: "swarm-project", SourceCommit: strings.Repeat("a", 40), Objective: "test durable admission", BudgetBucket: "shared-account", TokenLimit: 100, MaxActive: 3,
		Tasks: []domain.SwarmTaskContract{
			{ID: "a", Version: 1, Objective: "first independent task", OracleHash: strings.Repeat("1", 64), Agent: swarmSelection, ReserveTokens: 30, Resources: []domain.SwarmResource{{Namespace: "repository", Kind: "path", Key: "src/a"}}},
			{ID: "b", Version: 1, Objective: "second independent task", OracleHash: strings.Repeat("2", 64), Agent: swarmSelection, ReserveTokens: 30, Resources: []domain.SwarmResource{{Namespace: "repository", Kind: "path", Key: "src/b"}}},
		}}
}

func swarmCreate(t *testing.T, s *sqlite.Store, id string, p domain.SwarmPlan) domain.SwarmRun {
	t.Helper()
	r, e := s.CreateSwarmRun(context.Background(), id, "create-"+id, p, swarmNow)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func swarmClaim(run, task, key string) domain.SwarmClaim {
	return domain.SwarmClaim{RunID: run, TaskID: task, Version: 1, RequestID: key, Owner: "coordinator", Resolved: swarmSelection, CapabilityFingerprint: strings.Repeat("f", 64)}
}
func swarmReserve(t *testing.T, s *sqlite.Store, run, task, key string) domain.SwarmAttempt {
	t.Helper()
	a, e := s.ClaimSwarmTask(context.Background(), swarmClaim(run, task, key), "attempt-"+key, "operation-"+key, swarmNow, swarmNow.Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func swarmFenceFor(a domain.SwarmAttempt) domain.SwarmFence {
	return domain.SwarmFence{AttemptID: a.ID, Owner: a.Owner, Generation: a.Generation}
}
func swarmCheck(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}
func swarmSubmit(t *testing.T, s *sqlite.Store, a domain.SwarmAttempt) domain.SwarmAttempt {
	t.Helper()
	ctx, f := context.Background(), swarmFenceFor(a)
	_, launch, e := s.BeginSwarmDispatch(ctx, f, swarmNow)
	if e != nil || !launch {
		t.Fatalf("begin = %v, %v", launch, e)
	}
	if e = s.ConfirmSwarmRunning(ctx, f, "worker-"+a.ID, "", swarmNow); e != nil {
		t.Fatal(e)
	}
	if e = s.FinishSwarmAttempt(ctx, f, "submitted", strings.Repeat("b", 40), true, swarmNow); e != nil {
		t.Fatal(e)
	}
	a, e = s.GetSwarmAttempt(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func swarmProof(a domain.SwarmAttempt, kind, actor string) domain.SwarmEvidence {
	return domain.SwarmEvidence{ID: a.ID + "-" + kind, AttemptID: a.ID, Kind: kind, ActorID: actor, Commit: a.SubmissionCommit, ContractHash: a.ContractHash, OracleHash: a.Contract.OracleHash, Verdict: "pass", ArtifactHash: strings.Repeat("d", 64)}
}
func swarmIntegration(a domain.SwarmAttempt) domain.SwarmIntegration {
	return domain.SwarmIntegration{ID: "integrate-" + a.ID, RunID: a.RunID, BaseCommit: strings.Repeat("a", 40), Commit: strings.Repeat("c", 40), AttemptIDs: []string{a.ID}, ReviewID: "combined-reviewer", EvaluatorID: "combined-evaluator", ReviewArtifactHash: strings.Repeat("e", 64), EvaluationArtifactHash: strings.Repeat("f", 64)}
}

func TestSwarmConcurrentClaimsAcrossStores(t *testing.T) {
	dir, ctx := t.TempDir(), context.Background()
	a := sqlitetest.MustOpenAt(t, dir)
	b, e := sqlite.OpenPreMigrated(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = b.Close() })
	seedProject(t, a, "swarm-project")
	swarmCreate(t, a, "run", swarmPlan())
	start := make(chan struct{})
	var wg sync.WaitGroup
	var attempts [2]domain.SwarmAttempt
	var errs [2]error
	for i, s := range []*sqlite.Store{a, b} {
		wg.Add(1)
		go func(i int, s *sqlite.Store) {
			defer wg.Done()
			<-start
			key := fmt.Sprintf("request-%d", i)
			attempts[i], errs[i] = s.ClaimSwarmTask(ctx, swarmClaim("run", "a", key), "attempt-"+key, "operation-"+key, swarmNow, swarmNow.Add(time.Minute))
		}(i, s)
	}
	close(start)
	wg.Wait()
	winner := 0
	if errs[0] != nil {
		winner = 1
	}
	if errs[winner] != nil || !errors.Is(errs[1-winner], domain.ErrSwarmConflict) {
		t.Fatalf("claims: %v", errs)
	}
	got, e := b.GetSwarmBudget(ctx, "shared-account")
	if e != nil || got.Reserved != 30 {
		t.Fatalf("budget = %+v, %v", got, e)
	}
	c := swarmClaim("run", "a", attempts[winner].RequestID)
	c.Owner = "restarted-coordinator"
	duplicate, e := b.ClaimSwarmTask(ctx, c, "new-generated-id", "new-operation", swarmNow, swarmNow.Add(time.Hour))
	if e != nil || duplicate.ID != attempts[winner].ID || duplicate.OperationID != attempts[winner].OperationID || duplicate.Owner != "coordinator" {
		t.Fatalf("duplicate = %+v, %v", duplicate, e)
	}
	c.Resolved.Model = "unauthorized-replacement"
	_, e = b.ClaimSwarmTask(ctx, c, "another-id", "another-op", swarmNow, swarmNow.Add(time.Hour))
	swarmCheck(t, e, domain.ErrSwarmConflict)
	// Reusing an operation ID after budget admission must roll everything back.
	_, e = b.ClaimSwarmTask(ctx, swarmClaim("run", "b", "rollback"), "rollback-attempt", duplicate.OperationID, swarmNow, swarmNow.Add(time.Minute))
	if e == nil {
		t.Fatal("duplicate operation accepted")
	}
	got, e = a.GetSwarmBudget(ctx, "shared-account")
	if e != nil || got.Reserved != 30 {
		t.Fatalf("rollback budget = %+v, %v", got, e)
	}
	swarmReserve(t, b, "run", "b", "independent")
	all, e := a.ListSwarmAttempts(ctx, "run")
	if e != nil || len(all) != 2 {
		t.Fatalf("attempts = %d, %v", len(all), e)
	}
}

func TestSwarmGlobalResourcesAndSharedBudget(t *testing.T) {
	for _, kind := range []string{"path", "semantic"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx := newTestStore(t), context.Background()
			seedProject(t, s, "swarm-project")
			p := swarmPlan()
			p.Tasks = p.Tasks[:1]
			p.Tasks[0].Resources[0].Kind = kind
			p.Tasks[0].Resources[0].Key = "src"
			swarmCreate(t, s, "one", p)
			swarmReserve(t, s, "one", "a", "one")
			if kind == "path" {
				p.Tasks[0].Resources[0].Key = "src/child"
			}
			swarmCreate(t, s, "two", p)
			_, e := s.ClaimSwarmTask(ctx, swarmClaim("two", "a", "two"), "attempt-two", "op-two", swarmNow, swarmNow.Add(time.Minute))
			swarmCheck(t, e, domain.ErrSwarmConflict)
			p.Tasks[0].Resources[0].Key = "unrelated"
			p.Tasks[0].ReserveTokens = 80
			swarmCreate(t, s, "three", p)
			_, e = s.ClaimSwarmTask(ctx, swarmClaim("three", "a", "three"), "attempt-three", "op-three", swarmNow, swarmNow.Add(time.Minute))
			swarmCheck(t, e, domain.ErrSwarmCapacity)
		})
	}
}

func TestSwarmRestartFencesUnknownLaunch(t *testing.T) {
	dir, ctx := t.TempDir(), context.Background()
	s, e := sqlitetest.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	seedProject(t, s, "swarm-project")
	swarmCreate(t, s, "run", swarmPlan())
	a := swarmReserve(t, s, "run", "a", "request")
	f := swarmFenceFor(a)
	_, launch, e := s.BeginSwarmDispatch(ctx, f, swarmNow)
	if e != nil || !launch {
		t.Fatalf("begin = %v %v", launch, e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = sqlite.OpenPreMigrated(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := swarmNow.Add(2 * time.Minute)
	_, _, e = s.BeginSwarmDispatch(ctx, f, now)
	swarmCheck(t, e, domain.ErrSwarmStale)
	owned, e := s.TakeOverSwarmLease(ctx, f, "new-owner", now, now.Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	if owned.OperationID != a.OperationID || owned.State != "dispatching" || owned.Generation != 2 {
		t.Fatalf("takeover = %+v", owned)
	}
	_, launch, e = s.BeginSwarmDispatch(ctx, swarmFenceFor(owned), now)
	if e != nil || launch {
		t.Fatalf("duplicate launch = %v, %v", launch, e)
	}
	e = s.FinishSwarmAttempt(ctx, f, "failed", "", true, now)
	swarmCheck(t, e, domain.ErrSwarmStale)
	e = s.FinishSwarmAttempt(ctx, swarmFenceFor(owned), "failed", "", false, now)
	swarmCheck(t, e, domain.ErrSwarmInvalid)
	_, e = s.ClaimSwarmTask(ctx, swarmClaim("run", "a", "retry"), "retry", "retry-op", now, now.Add(time.Minute))
	swarmCheck(t, e, domain.ErrSwarmConflict)
	if e = s.ConfirmSwarmRunning(ctx, swarmFenceFor(owned), "provider-session", "", now); e != nil {
		t.Fatal(e)
	}
	if e = s.CancelSwarmRun(ctx, "run"); e != nil {
		t.Fatal(e)
	}
	e = s.FinishSwarmAttempt(ctx, swarmFenceFor(owned), "submitted", strings.Repeat("b", 40), true, now)
	swarmCheck(t, e, domain.ErrSwarmNotReady)
	if e = s.FinishSwarmAttempt(ctx, swarmFenceFor(owned), "cancelled", "", true, now); e != nil {
		t.Fatal(e)
	}
	budget, e := s.GetSwarmBudget(ctx, "shared-account")
	if e != nil || budget.Reserved != 30 {
		t.Fatalf("unknown usage = %+v, %v", budget, e)
	}
	_, e = s.ClaimSwarmTask(ctx, swarmClaim("run", "b", "after-cancel"), "cancelled-attempt", "cancelled-op", now, now.Add(time.Minute))
	swarmCheck(t, e, domain.ErrSwarmNotReady)
	u := domain.SwarmUsage{AttemptID: a.ID, ReceiptID: "usage-1", ActualTokens: 35}
	if e = s.SettleSwarmUsage(ctx, u); e != nil {
		t.Fatal(e)
	}
	if e = s.SettleSwarmUsage(ctx, u); e != nil {
		t.Fatal(e)
	}
	u.ActualTokens++
	swarmCheck(t, s.SettleSwarmUsage(ctx, u), domain.ErrSwarmConflict)
	budget, e = s.GetSwarmBudget(ctx, "shared-account")
	if e != nil || budget.Reserved != 0 || budget.Spent != 35 {
		t.Fatalf("settled = %+v, %v", budget, e)
	}
}

func TestSwarmAcceptanceAndVersionedDependencies(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	seedProject(t, s, "swarm-project")
	p := swarmPlan()
	p.Tasks[1].Dependencies = []domain.SwarmDependency{{TaskID: "a", Version: 1}}
	r := swarmCreate(t, s, "run", p)
	duplicate, e := s.CreateSwarmRun(ctx, "other-run-id", "create-run", p, swarmNow)
	if e != nil || duplicate.ID != r.ID {
		t.Fatalf("create retry = %+v %v", duplicate, e)
	}
	changed := p
	changed.Objective = "different"
	_, e = s.CreateSwarmRun(ctx, "other", "create-run", changed, swarmNow)
	swarmCheck(t, e, domain.ErrSwarmConflict)
	_, e = s.ClaimSwarmTask(ctx, swarmClaim("run", "b", "not-ready"), "blocked", "blocked-op", swarmNow, swarmNow.Add(time.Minute))
	swarmCheck(t, e, domain.ErrSwarmNotReady)
	a := swarmSubmit(t, s, swarmReserve(t, s, "run", "a", "a"))
	i := swarmIntegration(a)
	swarmCheck(t, s.AcceptSwarmIntegration(ctx, i), domain.ErrSwarmNotReady)
	review := swarmProof(a, "review", a.SessionID)
	swarmCheck(t, s.RecordSwarmEvidence(ctx, review), domain.ErrSwarmConflict)
	review.ActorID = "independent-reviewer"
	review.Commit = strings.Repeat("e", 40)
	swarmCheck(t, s.RecordSwarmEvidence(ctx, review), domain.ErrSwarmConflict)
	review.Commit = a.SubmissionCommit
	if e = s.RecordSwarmEvidence(ctx, review); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordSwarmEvidence(ctx, review); e != nil {
		t.Fatal(e)
	}
	review.Verdict = "fail"
	swarmCheck(t, s.RecordSwarmEvidence(ctx, review), domain.ErrSwarmConflict)
	evaluation := swarmProof(a, "evaluation", "independent-evaluator")
	if e = s.RecordSwarmEvidence(ctx, evaluation); e != nil {
		t.Fatal(e)
	}
	i.BaseCommit = strings.Repeat("f", 40)
	swarmCheck(t, s.AcceptSwarmIntegration(ctx, i), domain.ErrSwarmStale)
	i.BaseCommit = p.SourceCommit
	if e = s.AcceptSwarmIntegration(ctx, i); e != nil {
		t.Fatal(e)
	}
	if e = s.AcceptSwarmIntegration(ctx, i); e != nil {
		t.Fatal(e)
	}
	b := swarmReserve(t, s, "run", "b", "b")
	if a.BaseCommit != p.SourceCommit || b.BaseCommit != i.Commit {
		t.Fatalf("assignment bases: first=%s dependent=%s", a.BaseCommit, b.BaseCommit)
	}
	if e = s.FinishSwarmAttempt(ctx, swarmFenceFor(b), "failed", "", true, swarmNow); e != nil {
		t.Fatal(e)
	}
	p.Version = 2
	p.Tasks[0].Version = 2
	p.Tasks[0].Objective = "changed contract"
	p.Tasks[1].Version = 2
	swarmCheck(t, s.ReviseSwarmPlan(ctx, "run", 1, p), domain.ErrSwarmInvalid)
	p.Tasks[1].Dependencies[0].Version = 2
	if e = s.ReviseSwarmPlan(ctx, "run", 1, p); e != nil {
		t.Fatal(e)
	}
	_, e = s.ClaimSwarmTask(ctx, swarmClaim("run", "a", "stale"), "stale", "stale-op", swarmNow, swarmNow.Add(time.Minute))
	swarmCheck(t, e, domain.ErrSwarmStale)
	c := swarmClaim("run", "b", "waiting-v2")
	c.Version = 2
	_, e = s.ClaimSwarmTask(ctx, c, "waiting", "waiting-op", swarmNow, swarmNow.Add(time.Minute))
	swarmCheck(t, e, domain.ErrSwarmNotReady)
	old, e := s.GetSwarmAttempt(ctx, a.ID)
	if e != nil || old.Contract.Version != 1 || old.State != "accepted" {
		t.Fatalf("history = %+v, %v", old, e)
	}
	contract, e := s.GetSwarmContract(ctx, "run", "a", 1)
	if e != nil || contract.Version != 1 || contract.Objective == p.Tasks[0].Objective {
		t.Fatalf("old contract = %+v %v", contract, e)
	}
	proofs, e := s.ListSwarmEvidence(ctx, a.ID)
	if e != nil || len(proofs) != 2 {
		t.Fatalf("proofs = %+v %v", proofs, e)
	}
	integrated, e := s.GetSwarmIntegration(ctx, i.ID)
	if e != nil || integrated.Commit != i.Commit {
		t.Fatalf("integration = %+v %v", integrated, e)
	}
}

func TestSwarmOneRuntimeSessionPerAttempt(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	seedProject(t, s, "swarm-project")
	swarmCreate(t, s, "run", swarmPlan())
	a := swarmReserve(t, s, "run", "a", "a")
	b := swarmReserve(t, s, "run", "b", "b")
	for _, item := range []domain.SwarmAttempt{a, b} {
		if _, launch, e := s.BeginSwarmDispatch(ctx, swarmFenceFor(item), swarmNow); e != nil || !launch {
			t.Fatalf("begin = %v %v", launch, e)
		}
	}
	if e := s.ConfirmSwarmRunning(ctx, swarmFenceFor(a), "shared-session", "", swarmNow); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfirmSwarmRunning(ctx, swarmFenceFor(b), "shared-session", "", swarmNow); e == nil {
		t.Fatal("two attempts adopted one session")
	}
	got, e := s.GetSwarmAttempt(ctx, b.ID)
	if e != nil || got.State != "dispatching" || got.SessionID != "" {
		t.Fatalf("failed adoption changed state: %+v %v", got, e)
	}
}

func TestSwarmExpiredSubmissionCanBeClosed(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			s, ctx := newTestStore(t), context.Background()
			seedProject(t, s, "swarm-project")
			p := swarmPlan()
			swarmCreate(t, s, "run", p)
			a := swarmSubmit(t, s, swarmReserve(t, s, "run", "a", "request"))
			proof := swarmProof(a, "evaluation", "evaluator")
			proof.Verdict = "fail"
			if e := s.RecordSwarmEvidence(ctx, proof); e != nil {
				t.Fatal(e)
			}
			swarmCheck(t, s.AcceptSwarmIntegration(ctx, swarmIntegration(a)), domain.ErrSwarmNotReady)
			now := swarmNow.Add(2 * time.Minute)
			owned, e := s.TakeOverSwarmLease(ctx, swarmFenceFor(a), "recovery", now, now.Add(time.Minute))
			if e != nil {
				t.Fatal(e)
			}
			_, launch, e := s.BeginSwarmDispatch(ctx, swarmFenceFor(owned), now)
			if e != nil || launch {
				t.Fatalf("submitted launch = %v %v", launch, e)
			}
			state := "failed"
			if cancel {
				state = "cancelled"
				if e = s.CancelSwarmRun(ctx, "run"); e != nil {
					t.Fatal(e)
				}
			}
			if e = s.FinishSwarmAttempt(ctx, swarmFenceFor(owned), state, "", true, now); e != nil {
				t.Fatal(e)
			}
			closed, e := s.GetSwarmAttempt(ctx, a.ID)
			if e != nil || closed.SubmissionCommit != a.SubmissionCommit {
				t.Fatalf("lost rejected submission: %+v %v", closed, e)
			}
			if !cancel {
				p.Version = 2
				p.Tasks[0].Version = 2
				p.Tasks[0].Objective = "fix failed oracle"
				if e = s.ReviseSwarmPlan(ctx, "run", 1, p); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestSwarmUsageOverrunAndCapacity(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	seedProject(t, s, "swarm-project")
	p := swarmPlan()
	p.MaxActive = 1
	swarmCreate(t, s, "run", p)
	a := swarmReserve(t, s, "run", "a", "a")
	_, e := s.ClaimSwarmTask(ctx, swarmClaim("run", "b", "b"), "b", "b-op", swarmNow, swarmNow.Add(time.Minute))
	swarmCheck(t, e, domain.ErrSwarmCapacity)
	swarmCheck(t, s.SettleSwarmUsage(ctx, domain.SwarmUsage{AttemptID: a.ID, ReceiptID: "early", ActualTokens: 1}), domain.ErrSwarmNotReady)
	if e = s.FinishSwarmAttempt(ctx, swarmFenceFor(a), "failed", "", true, swarmNow); e != nil {
		t.Fatal(e)
	}
	if e = s.SettleSwarmUsage(ctx, domain.SwarmUsage{AttemptID: a.ID, ReceiptID: "overrun", ActualTokens: math.MaxInt64}); e != nil {
		t.Fatal(e)
	}
	_, e = s.ClaimSwarmTask(ctx, swarmClaim("run", "b", "b2"), "b2", "b2-op", swarmNow, swarmNow.Add(time.Minute))
	swarmCheck(t, e, domain.ErrSwarmCapacity)
	b, e := s.GetSwarmBudget(ctx, "shared-account")
	if e != nil || b.Spent != math.MaxInt64 || b.Reserved != 0 {
		t.Fatalf("overrun = %+v %v", b, e)
	}
}
