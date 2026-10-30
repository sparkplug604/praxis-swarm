package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func routeNumber(n int64) *int64      { return &n }
func routePercent(n float64) *float64 { return &n }
func routeFixture(t *testing.T) (*sqlite.Store, domain.SwarmPlan, domain.SwarmRoutingPolicy, domain.SwarmQuotaSnapshot) {
	t.Helper()
	s := newTestStore(t)
	seedProject(t, s, "swarm-project")
	p := swarmPlan()
	p.TokenLimit = 1000
	p.MaxActive = 4
	p.Tasks = append(p.Tasks, p.Tasks[0], p.Tasks[1])
	p.Tasks[2].ID = "c"
	p.Tasks[3].ID = "d"
	p.Tasks[2].Agent.Model = "premium-test"
	p.Tasks[3].Role = "reviewer"
	for i := range p.Tasks {
		p.Tasks[i].Resources = []domain.SwarmResource{{Namespace: "repository", Kind: "path", Key: "src/" + p.Tasks[i].ID}}
	}
	swarmCreate(t, s, "run", p)
	policy := domain.SwarmRoutingPolicy{ID: "policy", AccountID: "account", Bucket: p.BudgetBucket, PremiumTokenLimit: 60, Models: []domain.SwarmRouteModel{
		{Harness: domain.HarnessFake, Model: "test-model", Roles: []string{"worker", "reviewer"}, QuotaScope: "worker-meter", MinEffort: "medium", MaxEffort: "high"},
		{Harness: domain.HarnessFake, Model: "premium-test", Roles: []string{"worker"}, QuotaScope: "premium-meter", Premium: true, MinEffort: "medium", MaxEffort: "high"},
	}}
	q := domain.SwarmQuotaSnapshot{ID: "quota", Bucket: p.BudgetBucket, Revision: 1, ObservedAt: swarmNow.Add(-time.Minute), ExpiresAt: swarmNow.Add(time.Minute), Windows: []domain.SwarmQuotaWindow{
		{ID: "worker", Scope: "worker-meter", LimitTokens: routeNumber(10000), UsedTokens: routeNumber(0), ResetAt: swarmNow.Add(time.Hour)},
		{ID: "premium", Scope: "premium-meter", LimitTokens: routeNumber(10000), UsedTokens: routeNumber(0), ResetAt: swarmNow.Add(time.Hour)},
	}}
	if err := s.ConfigureSwarmRouting(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSwarmQuota(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	return s, p, policy, q
}
func routeClaim(p domain.SwarmPlan, policy domain.SwarmRoutingPolicy, q domain.SwarmQuotaSnapshot, id, key string) domain.SwarmClaim {
	var task domain.SwarmTaskContract
	for _, t := range p.Tasks {
		if t.ID == id {
			task = t
		}
	}
	role := domain.SwarmTaskRole(task)
	m, _ := domain.SwarmPolicyModel(policy, role, task.Agent)
	c := swarmClaim("run", id, key)
	c.Resolved = task.Agent
	c.Routing = &domain.SwarmRouteBinding{Role: role, Premium: m.Premium, QuotaScope: m.QuotaScope, PolicyHash: domain.SwarmHash(policy), QuotaHash: domain.SwarmHash(q), CatalogFingerprint: "fixture-catalog", ContextTokens: 100}
	return c
}
func routeReserve(t *testing.T, s *sqlite.Store, p domain.SwarmPlan, policy domain.SwarmRoutingPolicy, q domain.SwarmQuotaSnapshot, id string) domain.SwarmAttempt {
	t.Helper()
	a, err := s.ClaimSwarmTask(context.Background(), routeClaim(p, policy, q, id, "claim-"+id), "attempt-"+id, "operation-"+id, swarmNow, swarmNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func routeReceipt(a domain.SwarmAttempt, id string, input, output *int64) domain.SwarmUsageReceipt {
	return domain.SwarmUsageReceipt{ID: id, AttemptID: a.ID, OperationID: a.OperationID, ProviderID: "fixture-provider", ResponseID: "response-" + id, ReportedModel: a.Resolved.Model, InputTokens: input, OutputTokens: output}
}
func routeFinish(t *testing.T, s *sqlite.Store, a domain.SwarmAttempt, state string) {
	t.Helper()
	if err := s.FinishSwarmAttempt(context.Background(), swarmFenceFor(a), state, "", true, swarmNow); err != nil {
		t.Fatal(err)
	}
}

func TestSwarmRoutingIndependentMetersAndRefresh(t *testing.T) {
	s, p, policy, q := routeFixture(t)
	ctx := context.Background()
	q.Revision++
	q.ObservedAt = swarmNow
	q.ID = "refresh"
	q.Windows[1].UsedTokens = routeNumber(10000)
	if err := s.UpdateSwarmQuota(ctx, q); err != nil {
		t.Fatal(err)
	}
	a := routeReserve(t, s, p, policy, q, "a")
	_, err := s.ClaimSwarmTask(ctx, routeClaim(p, policy, q, "c", "blocked"), "blocked", "blocked", swarmNow, swarmNow.Add(time.Hour))
	swarmCheck(t, err, domain.ErrSwarmCapacity)
	stale := q
	stale.Revision--
	if err := s.UpdateSwarmQuota(ctx, stale); !errors.Is(err, domain.ErrSwarmStale) {
		t.Fatal(err)
	}
	q.Revision++
	q.ObservedAt = swarmNow.Add(time.Second)
	q.ExpiresAt = swarmNow.Add(time.Minute)
	q.Windows[0].ResetAt = swarmNow.Add(-time.Second)
	if err := s.UpdateSwarmQuota(ctx, q); err != nil {
		t.Fatal(err)
	}
	_, err = s.ClaimSwarmTask(ctx, routeClaim(p, policy, q, "b", "reset"), "reset", "reset", swarmNow.Add(2*time.Second), swarmNow.Add(time.Hour))
	swarmCheck(t, err, domain.ErrSwarmStale)
	if a.Routing.QuotaScope != "worker-meter" {
		t.Fatal("lost billing meter")
	}
}

func TestSwarmRoutingUnknownQuotaRequiresExplicitPolicy(t *testing.T) {
	s, p, policy, q := routeFixture(t)
	q.Revision++
	q.ObservedAt = swarmNow
	q.Windows[0].LimitTokens = nil
	q.Windows[0].UsedTokens = nil
	q.Windows[0].UsedPercent = routePercent(10)
	if err := s.UpdateSwarmQuota(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	_, err := s.ClaimSwarmTask(context.Background(), routeClaim(p, policy, q, "a", "unknown"), "unknown", "unknown", swarmNow, swarmNow.Add(time.Hour))
	swarmCheck(t, err, domain.ErrSwarmNotReady)
	policy.AllowUnknownQuota = true
	if err := s.ConfigureSwarmRouting(context.Background(), policy); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("policy silently changed: %v", err)
	}
}

func TestSwarmRoutingConcurrentProviderReservations(t *testing.T) {
	s, p, policy, q := routeFixture(t)
	q.Revision++
	q.ObservedAt = swarmNow
	q.Windows[0].LimitTokens = routeNumber(50)
	if err := s.UpdateSwarmQuota(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	// A second handle uses the same durable DB, not the same Go mutex.
	// The public store exposes no data-dir accessor; create the pair explicitly.
	dir := t.TempDir()
	left := sqlitetest.MustOpenAt(t, dir)
	right, err := sqlite.OpenPreMigrated(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer right.Close()
	seedProject(t, left, "swarm-project")
	swarmCreate(t, left, "run", p)
	if err := left.ConfigureSwarmRouting(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	q.Revision = 1
	if err := left.UpdateSwarmQuota(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, db := range []*sqlite.Store{left, right} {
		wg.Add(1)
		go func(i int, db *sqlite.Store) {
			defer wg.Done()
			<-start
			id := []string{"a", "b"}[i]
			_, errs[i] = db.ClaimSwarmTask(context.Background(), routeClaim(p, policy, q, id, id), id, id, swarmNow, swarmNow.Add(time.Hour))
		}(i, db)
	}
	close(start)
	wg.Wait()
	wins, blocks := 0, 0
	for _, e := range errs {
		if e == nil {
			wins++
		} else if errors.Is(e, domain.ErrSwarmCapacity) {
			blocks++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || blocks != 1 {
		t.Fatalf("concurrent results: %v", errs)
	}
	r, err := left.GetSwarmUsageReport(context.Background(), p.BudgetBucket)
	if err != nil || r.Total.Reserved != 30 {
		t.Fatalf("double reservation %+v %v", r, err)
	}
}

func TestSwarmRoutingReceiptsDedupeAndCancelledReviewUsage(t *testing.T) {
	s, p, policy, q := routeFixture(t)
	ctx := context.Background()
	a := routeReserve(t, s, p, policy, q, "a")
	review := routeReserve(t, s, p, policy, q, "d")
	premium := routeReserve(t, s, p, policy, q, "c")
	r := routeReceipt(a, "receipt", routeNumber(20), routeNumber(10))
	r.CacheReadTokens = routeNumber(15)
	r.ReasoningTokens = routeNumber(5)
	for i := 0; i < 2; i++ {
		if err := s.RecordSwarmUsageReceipt(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	redelivered := r
	redelivered.ID = "new-transport-id"
	if err := s.RecordSwarmUsageReceipt(ctx, redelivered); err != nil {
		t.Fatal(err)
	}
	conflict := redelivered
	conflict.InputTokens = routeNumber(21)
	swarmCheck(t, s.RecordSwarmUsageReceipt(ctx, conflict), domain.ErrSwarmConflict)
	wrong := r
	wrong.ID = "wrong"
	wrong.OperationID = "other"
	swarmCheck(t, s.RecordSwarmUsageReceipt(ctx, wrong), domain.ErrSwarmConflict)
	if err := s.RecordSwarmUsageReceipt(ctx, routeReceipt(review, "unknown", nil, nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSwarmUsageReceipt(ctx, routeReceipt(premium, "premium", routeNumber(10), routeNumber(20))); err != nil {
		t.Fatal(err)
	}
	routeFinish(t, s, a, "failed")
	routeFinish(t, s, review, "cancelled")
	routeFinish(t, s, premium, "failed")
	if err := s.SettleSwarmUsage(ctx, domain.SwarmUsage{AttemptID: a.ID, ReceiptID: "final-a", ActualTokens: 29}); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("undercounted observed receipt: %v", err)
	}
	for _, attempt := range []domain.SwarmAttempt{a, premium} {
		if err := s.SettleSwarmUsage(ctx, domain.SwarmUsage{AttemptID: attempt.ID, ReceiptID: "final-" + attempt.ID, ActualTokens: 30}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.GetSwarmUsageReport(ctx, p.BudgetBucket)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total.Settled != 60 || report.Total.Observed != 60 || report.Total.Reserved != 30 || report.Total.UnknownResponses != 1 || report.Total.UnsettledAttempts != 1 || report.Premium.Settled != 30 {
		t.Fatalf("usage %+v", report)
	}
}

func TestSwarmRoutingObservedOverrunAndLateReceiptHold(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "late"}[late], func(t *testing.T) {
			s, p, policy, q := routeFixture(t)
			ctx := context.Background()
			a := routeReserve(t, s, p, policy, q, "a")
			if late {
				routeFinish(t, s, a, "failed")
				if err := s.SettleSwarmUsage(ctx, domain.SwarmUsage{AttemptID: a.ID, ReceiptID: "final", ActualTokens: 0}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.RecordSwarmUsageReceipt(ctx, routeReceipt(a, "overrun", routeNumber(980), routeNumber(0))); err != nil {
				t.Fatal(err)
			}
			if late {
				if err := s.SettleSwarmUsage(ctx, domain.SwarmUsage{AttemptID: a.ID, ReceiptID: "final", ActualTokens: 0}); err != nil {
					t.Fatalf("idempotent settlement replay: %v", err)
				}
			}
			report, err := s.GetSwarmUsageReport(ctx, p.BudgetBucket)
			if err != nil || report.Total.Reserved != 980 {
				t.Fatalf("overrun lost: %+v %v", report, err)
			}
			_, err = s.ClaimSwarmTask(ctx, routeClaim(p, policy, q, "b", "next"), "next", "next", swarmNow, swarmNow.Add(time.Hour))
			swarmCheck(t, err, domain.ErrSwarmCapacity)
		})
	}
}

func TestSwarmRoutingPremiumCapAndRuntimeMismatch(t *testing.T) {
	s, p, policy, q := routeFixture(t)
	ctx := context.Background()
	a := routeReserve(t, s, p, policy, q, "c")
	if err := s.RecordSwarmUsageReceipt(ctx, routeReceipt(a, "premium-overrun", routeNumber(65), routeNumber(0))); err != nil {
		t.Fatal(err)
	}
	routeFinish(t, s, a, "failed")
	if err := s.SettleSwarmUsage(ctx, domain.SwarmUsage{AttemptID: a.ID, ReceiptID: "final", ActualTokens: 65}); err != nil {
		t.Fatal(err)
	}
	_, err := s.ClaimSwarmTask(ctx, routeClaim(p, policy, q, "c", "second-premium"), "second-premium", "second-premium", swarmNow, swarmNow.Add(time.Hour))
	swarmCheck(t, err, domain.ErrSwarmCapacity)
	worker := routeReserve(t, s, p, policy, q, "a")
	f := swarmFenceFor(worker)
	if _, _, err := s.BeginSwarmDispatch(ctx, f, swarmNow); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmSwarmRunning(ctx, f, "fixture-session", "premium-test", swarmNow); err != nil {
		t.Fatal(err)
	}
	report, err := s.GetSwarmUsageReport(ctx, p.BudgetBucket)
	if err != nil || report.ModelMismatches != 1 {
		t.Fatalf("mismatch lost: %+v %v", report, err)
	}
	_, err = s.ClaimSwarmTask(ctx, routeClaim(p, policy, q, "b", "next"), "next", "next", swarmNow, swarmNow.Add(time.Hour))
	swarmCheck(t, err, domain.ErrSwarmConflict)
}

func TestSwarmRoutingCannotBypassPolicyOrDuplicateAccount(t *testing.T) {
	s, p, policy, q := routeFixture(t)
	ctx := context.Background()
	c := routeClaim(p, policy, q, "a", "bad")
	c.Routing = nil
	_, err := s.ClaimSwarmTask(ctx, c, "bad", "bad", swarmNow, swarmNow.Add(time.Hour))
	swarmCheck(t, err, domain.ErrSwarmStale)
	c = routeClaim(p, policy, q, "a", "bad")
	c.Resolved.Effort = "max"
	_, err = s.ClaimSwarmTask(ctx, c, "bad", "bad", swarmNow, swarmNow.Add(time.Hour))
	swarmCheck(t, err, domain.ErrSwarmConflict)
	second := p
	second.BudgetBucket = "other-bucket"
	for i := range second.Tasks {
		second.Tasks[i].Resources = []domain.SwarmResource{{Namespace: "other-repo", Kind: "path", Key: second.Tasks[i].ID}}
	}
	swarmCreate(t, s, "other-run", second)
	policy.Bucket = second.BudgetBucket
	swarmCheck(t, s.ConfigureSwarmRouting(ctx, policy), domain.ErrSwarmConflict)
	if _, err := s.GetSwarmAttemptByRequest(ctx, "missing"); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatal(err)
	}
}
