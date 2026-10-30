package routing

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type admissionCatalog struct {
	catalog      ports.AgentModelCatalog
	err          error
	calls        int
	agent, scope string
	onRead       func()
}

func (c *admissionCatalog) RevalidateModels(_ context.Context, agent, scope string) (ports.AgentModelCatalog, error) {
	c.calls++
	c.agent, c.scope = agent, scope
	if c.onRead != nil {
		c.onRead()
	}
	return c.catalog, c.err
}

type admissionFixture struct {
	routeFixture
	store       *sqlite.Store
	catalogStub *admissionCatalog
	service     Service
	request     AdmissionRequest
}

func newAdmissionFixture(t *testing.T) *admissionFixture {
	t.Helper()
	f := &admissionFixture{routeFixture: newRouteFixture(), store: sqlitetest.MustOpen(t)}
	ctx := context.Background()
	if err := f.store.UpsertProject(ctx, domain.ProjectRecord{ID: "routing-project", Path: t.TempDir(), RegisteredAt: f.now}); err != nil {
		t.Fatal(err)
	}
	plan := domain.SwarmPlan{
		ProtocolVersion: 1, Version: 1, ProjectID: "routing-project", SourceCommit: strings.Repeat("a", 40),
		Objective: "admit an exact model without dispatch", BudgetBucket: f.policy.Bucket, TokenLimit: 10000, MaxActive: 2,
		Tasks: []domain.SwarmTaskContract{{
			ID: "task", Version: 1, Objective: "implement one bounded task", OracleHash: strings.Repeat("b", 64),
			Agent: f.task.Requested, ReserveTokens: f.task.ReserveTokens,
			Resources: []domain.SwarmResource{{Namespace: "repository", Kind: "path", Key: "src/task"}},
		}},
	}
	if _, err := f.store.CreateSwarmRun(ctx, "run", "create-run", plan, f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ConfigureSwarmRouting(ctx, f.policy); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateSwarmQuota(ctx, f.quota); err != nil {
		t.Fatal(err)
	}
	f.catalogStub = &admissionCatalog{catalog: f.catalog}
	f.service = Service{Store: f.store, Catalog: f.catalogStub, Now: func() time.Time { return f.now }}
	f.request = AdmissionRequest{
		Claim:     domain.SwarmClaim{RunID: "run", TaskID: "task", Version: 1, RequestID: "request", Owner: "coordinator", CapabilityFingerprint: strings.Repeat("c", 64)},
		AttemptID: "attempt", OperationID: "operation", ContextTokens: f.task.ContextTokens, LeaseUntil: f.now.Add(time.Minute),
	}
	return f
}

func checkAdmissionReservation(t *testing.T, f *admissionFixture, reserved int64, attempts int) {
	t.Helper()
	b, err := f.store.GetSwarmBudget(context.Background(), f.policy.Bucket)
	if err != nil || b.Reserved != reserved || b.Spent != 0 {
		t.Fatalf("budget=%+v err=%v", b, err)
	}
	rows, err := f.store.ListSwarmAttempts(context.Background(), "run")
	if err != nil || len(rows) != attempts {
		t.Fatalf("attempts=%d err=%v", len(rows), err)
	}
}

func TestAdmitReplayCannotChangeSchedulerBinding(t *testing.T) {
	f := newAdmissionFixture(t)
	if _, _, err := f.service.Admit(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	changed := f.request
	changed.Claim.Schedule = &domain.SwarmScheduleBinding{PoolID: "different-pool"}
	if _, _, err := f.service.Admit(context.Background(), changed); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("replay changed scheduler authority: %v", err)
	}
	checkAdmissionReservation(t, f, f.task.ReserveTokens, 1)
}

func TestAdmitReplaysOriginalReservationAfterQuotaRefresh(t *testing.T) {
	f := newAdmissionFixture(t)
	ctx := context.Background()
	// The service must resolve the immutable contract, ignoring injected advice.
	f.request.Claim.Resolved = domain.SwarmSelection{Harness: "other", Model: "premium", Effort: "ultra"}
	f.request.Claim.Routing = &domain.SwarmRouteBinding{Role: "planner", Premium: true, QuotaHash: "caller-supplied"}
	first, advice, err := f.service.Admit(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	if advice.Replayed || first.Resolved != f.task.Requested || first.Routing == nil || first.Routing.ContextTokens != f.request.ContextTokens || first.Routing.CatalogScope != "" {
		t.Fatalf("contract or admission binding changed: attempt=%+v advice=%+v", first, advice)
	}
	if f.catalogStub.calls != 1 || f.catalogStub.agent != "codex" || f.catalogStub.scope != "" {
		t.Fatalf("catalog=%+v", f.catalogStub)
	}
	q := f.quota
	q.ID, q.Revision, q.ObservedAt = "quota-refreshed", q.Revision+1, f.now
	if err := f.store.UpdateSwarmQuota(ctx, q); err != nil {
		t.Fatal(err)
	}
	// A retry recovers durable state despite expired quota/lease and a currently
	// unavailable provider. Fresh qualification is still required before launch.
	f.now = f.now.Add(2 * time.Hour)
	f.catalogStub.err = ports.ErrAgentAuthRequired
	retry := f.request
	retry.Claim.Owner, retry.AttemptID, retry.OperationID = "restarted", "other-attempt", "other-operation"
	retry.LeaseUntil = time.Time{}
	got, replay, err := f.service.Admit(ctx, retry)
	if err != nil || !reflect.DeepEqual(got, first) || !replay.Replayed || !replay.QuotaUnknown || replay.Binding != *first.Routing || replay.Requested != first.Contract.Agent || replay.Resolved != first.Resolved {
		t.Fatalf("replay=%+v advice=%+v err=%v", got, replay, err)
	}
	if f.catalogStub.calls != 1 || !strings.Contains(strings.Join(replay.Reasons, " "), "not launch permission") {
		t.Fatalf("replay discovered provider or claimed permission: calls=%d advice=%+v", f.catalogStub.calls, replay)
	}
	checkAdmissionReservation(t, f, f.task.ReserveTokens, 1)
	for _, tc := range []struct {
		name   string
		change func(*AdmissionRequest)
	}{
		{"run", func(r *AdmissionRequest) { r.Claim.RunID = "different-run" }},
		{"task", func(r *AdmissionRequest) { r.Claim.TaskID = "different-task" }},
		{"version", func(r *AdmissionRequest) { r.Claim.Version++ }},
		{"capability", func(r *AdmissionRequest) { r.Claim.CapabilityFingerprint = strings.Repeat("d", 64) }},
		{"context", func(r *AdmissionRequest) { r.ContextTokens++ }},
		{"scope", func(r *AdmissionRequest) { r.Scope = "credential:other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := retry
			tc.change(&changed)
			if _, _, err := f.service.Admit(ctx, changed); !errors.Is(err, domain.ErrSwarmConflict) {
				t.Fatalf("changed request accepted: %v", err)
			}
			if f.catalogStub.calls != 1 {
				t.Fatal("conflicting replay called catalog")
			}
		})
	}
	checkAdmissionReservation(t, f, f.task.ReserveTokens, 1)
}

func TestAdmitCatalogFailuresAndCancellationDoNotReserve(t *testing.T) {
	for _, cancelDuringRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "auth failure", true: "cancelled during discovery"}[cancelDuringRead], func(t *testing.T) {
			f := newAdmissionFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := ports.ErrAgentAuthRequired
			f.catalogStub.err = want
			if cancelDuringRead {
				want = context.Canceled
				f.catalogStub.err = nil
				f.catalogStub.onRead = cancel
			}
			if _, _, err := f.service.Admit(ctx, f.request); !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			if f.catalogStub.calls != 1 {
				t.Fatalf("catalog calls=%d", f.catalogStub.calls)
			}
			checkAdmissionReservation(t, f, 0, 0)
		})
	}
}

func TestAdmitUsesCatalogScopeAndClockAfterRevalidation(t *testing.T) {
	f := newAdmissionFixture(t)
	f.request.Scope = "credential:subscription"
	f.catalogStub.onRead = func() {
		f.now = f.now.Add(time.Second)
		f.catalogStub.catalog.ValidatedAt = f.now
	}
	a, advice, err := f.service.Admit(context.Background(), f.request)
	if err != nil || a.Routing == nil || a.Routing.CatalogScope != f.request.Scope || advice.Binding.CatalogScope != f.request.Scope || f.catalogStub.scope != f.request.Scope {
		t.Fatalf("scope/timing: attempt=%+v advice=%+v err=%v", a, advice, err)
	}
}

type concurrentAdmissionStore struct {
	Store
	lookups, claims int
	commitWinner    func() error
}

func (s *concurrentAdmissionStore) GetSwarmAttemptByRequest(ctx context.Context, id string) (domain.SwarmAttempt, error) {
	s.lookups++
	return s.Store.GetSwarmAttemptByRequest(ctx, id)
}

func (s *concurrentAdmissionStore) ClaimSwarmTask(ctx context.Context, claim domain.SwarmClaim, id, operation string, now, until time.Time) (domain.SwarmAttempt, error) {
	s.claims++
	if err := s.commitWinner(); err != nil {
		return domain.SwarmAttempt{}, err
	}
	return s.Store.ClaimSwarmTask(ctx, claim, id, operation, now, until)
}

func TestAdmitRecoversConcurrentCommitWithoutRetryingCatalog(t *testing.T) {
	for _, refreshQuota := range []bool{false, true} {
		t.Run(map[bool]string{false: "same quota", true: "refreshed quota"}[refreshQuota], func(t *testing.T) {
			f := newAdmissionFixture(t)
			ctx := context.Background()
			var winner domain.SwarmAttempt
			race := &concurrentAdmissionStore{Store: f.store}
			race.commitWinner = func() error {
				if refreshQuota {
					q := f.quota
					q.ID, q.Revision, q.ObservedAt = "concurrent-refresh", q.Revision+1, f.now
					if err := f.store.UpdateSwarmQuota(ctx, q); err != nil {
						return err
					}
				}
				other := f.service
				other.Catalog = &admissionCatalog{catalog: f.catalog}
				r := f.request
				r.Claim.Owner, r.AttemptID, r.OperationID = "winner", "winner-attempt", "winner-operation"
				var err error
				winner, _, err = other.Admit(ctx, r)
				return err
			}
			service := f.service
			service.Store = race
			got, advice, err := service.Admit(ctx, f.request)
			if err != nil || !reflect.DeepEqual(got, winner) || !advice.Replayed {
				t.Fatalf("concurrent replay=%+v advice=%+v err=%v", got, advice, err)
			}
			wantLookups := 1
			if refreshQuota {
				wantLookups = 2
			}
			if f.catalogStub.calls != 1 || race.lookups != wantLookups || race.claims != 1 {
				t.Fatalf("calls: catalog=%d lookups=%d claims=%d", f.catalogStub.calls, race.lookups, race.claims)
			}
			checkAdmissionReservation(t, f, f.task.ReserveTokens, 1)
		})
	}
}

func TestSwarmSpawnConfigDisablesCosmeticCallsOnly(t *testing.T) {
	base := ports.SpawnConfig{
		ProjectID: "project", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Prompt: "task", DisplayName: "Deterministic title",
		NameFromPrompt: true, KeepDisplayName: true, AgentConfig: ports.AgentConfig{Model: "worker", Effort: "standard"},
	}
	want := base
	want.NameFromPrompt, want.DisableCosmeticModelCalls = false, true
	got := SwarmSpawnConfig(base)
	if !reflect.DeepEqual(got, want) || !base.NameFromPrompt || base.DisableCosmeticModelCalls {
		t.Fatalf("spawn settings changed: base=%+v got=%+v", base, got)
	}
}
