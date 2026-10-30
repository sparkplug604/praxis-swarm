package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type scheduleFixture struct {
	left, right *sqlite.Store
	policy      domain.SwarmSchedulerPolicy
	route       domain.SwarmRoutingPolicy
	quota       domain.SwarmQuotaSnapshot
	plans       map[string]domain.SwarmPlan
	root        string
}

func schedulePolicy() domain.SwarmSchedulerPolicy {
	return domain.SwarmSchedulerPolicy{PoolID: "pool", MaxWorkers: 8, MaxReviewers: 8,
		ProviderLimits: map[string]int64{"account": 8, "other-account": 8},
		ProjectLimits:  map[string]int64{"project-one": 8, "project-two": 8}}
}

func newScheduleFixture(t *testing.T, policy domain.SwarmSchedulerPolicy) scheduleFixture {
	t.Helper()
	dir := t.TempDir()
	f := scheduleFixture{left: sqlitetest.MustOpenAt(t, dir), policy: policy, plans: map[string]domain.SwarmPlan{}, root: filepath.Join(dir, "workspaces")}
	var err error
	f.right, err = sqlite.OpenPreMigrated(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.right.Close() })
	for _, project := range []string{"project-one", "project-two"} {
		seedProject(t, f.left, project)
	}
	for _, id := range []string{"one", "two", "three"} {
		p := swarmPlan()
		p.ProjectID = "project-one"
		if id == "three" {
			p.ProjectID = "project-two"
		}
		p.TokenLimit, p.MaxActive = 10000, 10
		p.Tasks = append(p.Tasks, p.Tasks[0], p.Tasks[1], p.Tasks[0])
		p.Tasks[2].ID, p.Tasks[2].Role = "c", "reviewer"
		p.Tasks[3].ID, p.Tasks[3].Role = "d", "reviewer"
		p.Tasks[4].ID, p.Tasks[4].Role = "e", "planner"
		for i := range p.Tasks {
			p.Tasks[i].Resources = []domain.SwarmResource{
				{Namespace: "repository-" + id, Kind: "path", Key: "src/" + p.Tasks[i].ID},
				{Namespace: "repository-" + id, Kind: "semantic", Key: "interface-" + p.Tasks[i].ID},
			}
		}
		f.plans[id] = swarmCreate(t, f.left, id, p).Plan
	}
	f.route = domain.SwarmRoutingPolicy{ID: "route", AccountID: "account", Bucket: "shared-account", PremiumTokenLimit: 0,
		Models: []domain.SwarmRouteModel{{Harness: domain.HarnessFake, Model: "test-model", Roles: []string{"worker", "reviewer", "planner"}, QuotaScope: "meter", MinEffort: "medium", MaxEffort: "high"}}}
	f.quota = domain.SwarmQuotaSnapshot{ID: "quota", Bucket: "shared-account", Revision: 1, ObservedAt: swarmNow.Add(-time.Minute), ExpiresAt: swarmNow.Add(time.Hour),
		Windows: []domain.SwarmQuotaWindow{{ID: "window", Scope: "meter", LimitTokens: routeNumber(10000), UsedTokens: routeNumber(0), ResetAt: swarmNow.Add(time.Hour)}}}
	ctx := context.Background()
	for _, err := range []error{f.left.ConfigureSwarmRouting(ctx, f.route), f.left.UpdateSwarmQuota(ctx, f.quota), f.left.ConfigureSwarmScheduler(ctx, policy)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f scheduleFixture) claim(runID, taskID string) domain.SwarmClaim {
	c := routeClaim(f.plans[runID], f.route, f.quota, taskID, runID+"-"+taskID)
	c.RunID = runID
	c.Schedule = &domain.SwarmScheduleBinding{PoolID: f.policy.PoolID, PolicyHash: domain.SwarmHash(f.policy), ProviderKey: f.route.AccountID,
		SourceManifestHash: strings.Repeat("a", 64), WorkspaceIdentityHash: strings.Repeat("b", 64), WorkspaceRoot: filepath.Join(f.root, runID, taskID),
		Resources: []domain.SwarmResource{{Namespace: "resolved-filesystem", Kind: "path", Key: runID + "/" + taskID}}}
	return c
}

func scheduleClaim(s *sqlite.Store, c domain.SwarmClaim, now time.Time) (domain.SwarmAttempt, error) {
	return s.ClaimSwarmTask(context.Background(), c, "attempt-"+c.RequestID, "operation-"+c.RequestID, now, now.Add(time.Minute))
}

func TestSwarmScheduleConcurrentLimitsAcrossRunsAndStores(t *testing.T) {
	for _, dimension := range []string{"workers", "reviewers", "provider", "project"} {
		t.Run(dimension, func(t *testing.T) {
			policy := schedulePolicy()
			runs, task := []string{"one", "three"}, "a"
			switch dimension {
			case "workers":
				policy.MaxWorkers = 1
			case "reviewers":
				policy.MaxReviewers, task = 1, "c"
			case "provider":
				policy.ProviderLimits["account"] = 1
			case "project":
				policy.ProjectLimits["project-one"], runs = 1, []string{"one", "two"}
			}
			f := newScheduleFixture(t, policy)
			start := make(chan struct{})
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i, db := range []*sqlite.Store{f.left, f.right} {
				wg.Add(1)
				go func(i int, db *sqlite.Store) {
					defer wg.Done()
					<-start
					_, errs[i] = scheduleClaim(db, f.claim(runs[i], task), swarmNow)
				}(i, db)
			}
			close(start)
			wg.Wait()
			wins, blocks := 0, 0
			for _, err := range errs {
				if err == nil {
					wins++
				} else if errors.Is(err, domain.ErrSwarmCapacity) {
					blocks++
				} else {
					t.Fatal(err)
				}
			}
			if wins != 1 || blocks != 1 {
				t.Fatalf("capacity oversubscription: %v", errs)
			}
			budget, err := f.left.GetSwarmBudget(context.Background(), f.route.Bucket)
			if err != nil || budget.Reserved != 30 {
				t.Fatalf("failed admission leaked reservation: %+v, %v", budget, err)
			}
		})
	}
}

func TestSwarmScheduleSeparateRoleAndProjectSlots(t *testing.T) {
	p := schedulePolicy()
	p.MaxWorkers, p.MaxReviewers = 1, 1
	p.ProjectLimits["project-one"] = 1
	f := newScheduleFixture(t, p)
	for _, c := range []domain.SwarmClaim{f.claim("one", "a"), f.claim("three", "c")} {
		if _, err := scheduleClaim(f.left, c, swarmNow); err != nil {
			t.Fatalf("independent role/project rejected: %v", err)
		}
	}
	for _, c := range []domain.SwarmClaim{f.claim("two", "b"), f.claim("three", "d"), f.claim("three", "e")} {
		_, err := scheduleClaim(f.right, c, swarmNow)
		swarmCheck(t, err, domain.ErrSwarmCapacity)
	}
}

func TestSwarmScheduleFullProviderDoesNotBlockAnotherAccount(t *testing.T) {
	p := schedulePolicy()
	p.ProviderLimits["account"], p.ProviderLimits["other-account"] = 1, 1
	f := newScheduleFixture(t, p)
	ctx := context.Background()
	first, err := scheduleClaim(f.left, f.claim("one", "a"), swarmNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, launch, err := f.left.BeginSwarmDispatch(ctx, swarmFenceFor(first), swarmNow); err != nil || !launch {
		t.Fatalf("begin: %v %v", launch, err)
	}
	if err := f.left.ConfirmSwarmRunning(ctx, swarmFenceFor(first), "running-session", "test-model", swarmNow); err != nil {
		t.Fatal(err)
	}
	_, err = scheduleClaim(f.right, f.claim("three", "a"), swarmNow)
	swarmCheck(t, err, domain.ErrSwarmCapacity)
	otherPlan := f.plans["three"]
	otherPlan.BudgetBucket = "other-account-bucket"
	swarmCreate(t, f.left, "other-account-run", otherPlan)
	otherRoute := f.route
	otherRoute.ID, otherRoute.AccountID, otherRoute.Bucket = "other-policy", "other-account", otherPlan.BudgetBucket
	otherQuota := f.quota
	otherQuota.Bucket = otherPlan.BudgetBucket
	if err := f.left.ConfigureSwarmRouting(ctx, otherRoute); err != nil {
		t.Fatal(err)
	}
	if err := f.left.UpdateSwarmQuota(ctx, otherQuota); err != nil {
		t.Fatal(err)
	}
	c := routeClaim(otherPlan, otherRoute, otherQuota, "b", "other-account-b")
	c.RunID = "other-account-run"
	c.Schedule = f.claim("three", "b").Schedule
	c.Schedule.ProviderKey = otherRoute.AccountID
	if _, err := scheduleClaim(f.right, c, swarmNow); err != nil {
		t.Fatalf("independent account rejected: %v", err)
	}
}

func TestSwarmScheduleExpiryUnknownOwnershipAndReplayRetainCapacity(t *testing.T) {
	p := schedulePolicy()
	p.MaxWorkers = 1
	f := newScheduleFixture(t, p)
	ctx := context.Background()
	c := f.claim("one", "a")
	a, err := scheduleClaim(f.left, c, swarmNow)
	if err != nil {
		t.Fatal(err)
	}
	_, launch, err := f.left.BeginSwarmDispatch(ctx, swarmFenceFor(a), swarmNow)
	if err != nil || !launch {
		t.Fatalf("begin dispatch = %v, %v", launch, err)
	}
	if err = f.left.CancelSwarmRun(ctx, a.RunID); err != nil {
		t.Fatal(err)
	}
	if err = f.right.Close(); err != nil {
		t.Fatal(err)
	}
	f.right, err = sqlite.OpenPreMigrated(filepath.Dir(f.root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.right.Close() })
	later := swarmNow.Add(2 * time.Minute)
	// A second store observes the expired dispatch and cancellation; neither
	// establishes process death, so all account/project/role capacity is retained.
	_, err = scheduleClaim(f.right, f.claim("three", "a"), later)
	swarmCheck(t, err, domain.ErrSwarmCapacity)
	err = f.right.FinishSwarmAttempt(ctx, swarmFenceFor(a), "cancelled", "", false, later)
	swarmCheck(t, err, domain.ErrSwarmInvalid)
	_, err = scheduleClaim(f.right, f.claim("three", "a"), later)
	swarmCheck(t, err, domain.ErrSwarmCapacity)
	c.Owner = "restarted-owner"
	replayed, err := scheduleClaim(f.right, c, later)
	if err != nil || replayed.ID != a.ID || replayed.State != "dispatching" || replayed.Owner != a.Owner || replayed.Schedule == nil {
		t.Fatalf("replay changed allocation: %+v, %v", replayed, err)
	}
	c.Schedule.WorkspaceIdentityHash = strings.Repeat("c", 64)
	_, err = scheduleClaim(f.right, c, later)
	swarmCheck(t, err, domain.ErrSwarmConflict)
	owned, err := f.right.TakeOverSwarmLease(ctx, swarmFenceFor(a), "restarted-owner", later, later.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.right.FinishSwarmAttempt(ctx, swarmFenceFor(owned), "cancelled", "", true, later); err != nil {
		t.Fatal(err)
	}
	if _, err = scheduleClaim(f.right, f.claim("three", "a"), later); err != nil {
		t.Fatalf("confirmed stop did not free slot: %v", err)
	}
}

func TestSwarmSchedulePolicyBindingCannotEvadeLimits(t *testing.T) {
	f := newScheduleFixture(t, schedulePolicy())
	ctx := context.Background()
	for _, mutate := range []func(*domain.SwarmSchedulerPolicy){
		func(p *domain.SwarmSchedulerPolicy) { p.PoolID = "another-pool" },
		func(p *domain.SwarmSchedulerPolicy) { p.MaxWorkers++ },
	} {
		p := schedulePolicy()
		mutate(&p)
		swarmCheck(t, f.right.ConfigureSwarmScheduler(ctx, p), domain.ErrSwarmConflict)
	}
	if err := f.right.ConfigureSwarmScheduler(ctx, f.policy); err != nil {
		t.Fatal(err)
	}
	stored, err := f.right.GetSwarmSchedulerPolicy(ctx, f.policy.PoolID)
	if err != nil || domain.SwarmHash(stored) != domain.SwarmHash(f.policy) {
		t.Fatalf("policy changed on reload: %+v %v", stored, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*domain.SwarmClaim)
		want   error
	}{
		{"missing", func(c *domain.SwarmClaim) { c.Schedule = nil }, domain.ErrSwarmNotReady},
		{"stale", func(c *domain.SwarmClaim) { c.Schedule.PolicyHash = strings.Repeat("0", 64) }, domain.ErrSwarmStale},
		{"pool", func(c *domain.SwarmClaim) { c.Schedule.PoolID = "another-pool" }, domain.ErrSwarmStale},
		{"provider", func(c *domain.SwarmClaim) { c.Schedule.ProviderKey = "other-account" }, domain.ErrSwarmConflict},
		{"route", func(c *domain.SwarmClaim) { c.Routing = nil }, domain.ErrSwarmConflict},
		{"root", func(c *domain.SwarmClaim) { c.Schedule.WorkspaceRoot = "relative" }, domain.ErrSwarmInvalid},
		{"source", func(c *domain.SwarmClaim) { c.Schedule.SourceManifestHash = "unknown" }, domain.ErrSwarmInvalid},
		{"identity", func(c *domain.SwarmClaim) { c.Schedule.WorkspaceIdentityHash = "unknown" }, domain.ErrSwarmInvalid},
		{"resource", func(c *domain.SwarmClaim) { c.Schedule.Resources[0].Key = "../outside" }, domain.ErrSwarmInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := f.claim("one", "a")
			test.mutate(&c)
			_, err := scheduleClaim(f.right, c, swarmNow)
			swarmCheck(t, err, test.want)
		})
	}
	if _, err := scheduleClaim(f.right, f.claim("one", "a"), swarmNow); err != nil {
		t.Fatalf("invalid claims left durable side effects: %v", err)
	}
}

func TestSwarmScheduleOriginalAndResolvedResourcesPreserved(t *testing.T) {
	f := newScheduleFixture(t, schedulePolicy())
	c := f.claim("one", "a")
	original := f.plans["one"].Tasks[0]
	c.Schedule.Resources = append(c.Schedule.Resources, original.Resources[0], original.Resources[0])
	a, err := scheduleClaim(f.left, c, swarmNow)
	if err != nil {
		t.Fatal(err)
	}
	if domain.SwarmHash(a.Contract) != domain.SwarmHash(original) || a.ContractHash != domain.SwarmHash(original) || len(a.Contract.Resources) != len(original.Resources) {
		t.Fatal("resolved resources rewrote original contract")
	}
	for _, resource := range append(append([]domain.SwarmResource(nil), original.Resources...), c.Schedule.Resources[0]) {
		other := f.claim("three", "a")
		other.Schedule.Resources = []domain.SwarmResource{resource}
		_, err := scheduleClaim(f.right, other, swarmNow)
		swarmCheck(t, err, domain.ErrSwarmConflict)
	}
	// A different task also cannot share the same active workspace root.
	other := f.claim("three", "a")
	other.Schedule.WorkspaceRoot = c.Schedule.WorkspaceRoot
	_, err = scheduleClaim(f.right, other, swarmNow)
	swarmCheck(t, err, domain.ErrSwarmConflict)
	if err = f.left.FinishSwarmAttempt(context.Background(), swarmFenceFor(a), "failed", "", true, swarmNow); err != nil {
		t.Fatal(err)
	}
	if _, err = scheduleClaim(f.right, other, swarmNow); err != nil {
		t.Fatalf("confirmed stop retained workspace: %v", err)
	}
}

func TestSwarmScheduleConfigurationRejectsUncountedLegacyWork(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "swarm-project")
	swarmCreate(t, s, "legacy", swarmPlan())
	a := swarmReserve(t, s, "legacy", "a", "legacy-claim")
	ctx := context.Background()
	p := schedulePolicy()
	swarmCheck(t, s.ConfigureSwarmScheduler(ctx, p), domain.ErrSwarmConflict)
	if err := s.FinishSwarmAttempt(ctx, swarmFenceFor(a), "failed", "", true, swarmNow); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureSwarmScheduler(ctx, p); err != nil {
		t.Fatal(err)
	}
	// Completed pre-scheduler requests still replay byte-compatible identity.
	replayed, err := scheduleClaim(s, swarmClaim("legacy", "a", "legacy-claim"), swarmNow)
	if err != nil || replayed.ID != a.ID || replayed.Schedule != nil {
		t.Fatalf("historical replay: %+v %v", replayed, err)
	}
	data, err := json.Marshal(replayed)
	if err != nil || strings.Contains(string(data), `"schedule"`) {
		t.Fatalf("legacy JSON gained scheduling fields: %s %v", data, err)
	}
	_, err = scheduleClaim(s, swarmClaim("legacy", "b", "new-unscheduled"), swarmNow)
	swarmCheck(t, err, domain.ErrSwarmNotReady)
}

func TestSwarmScheduleExplicitZeroAndAbsentAllowlistEntries(t *testing.T) {
	for _, dimension := range []string{"workers", "reviewers", "provider-zero", "provider-absent", "project-zero", "project-absent"} {
		t.Run(dimension, func(t *testing.T) {
			p, task := schedulePolicy(), "a"
			switch dimension {
			case "workers":
				p.MaxWorkers = 0
			case "reviewers":
				p.MaxReviewers, task = 0, "c"
			case "provider-zero":
				p.ProviderLimits["account"] = 0
			case "provider-absent":
				delete(p.ProviderLimits, "account")
			case "project-zero":
				p.ProjectLimits["project-one"] = 0
			case "project-absent":
				delete(p.ProjectLimits, "project-one")
			}
			f := newScheduleFixture(t, p)
			_, err := scheduleClaim(f.left, f.claim("one", task), swarmNow)
			swarmCheck(t, err, domain.ErrSwarmCapacity)
		})
	}
}
