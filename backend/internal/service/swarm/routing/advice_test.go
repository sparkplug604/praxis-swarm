package routing

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type routeFixture struct {
	task    Task
	catalog ports.AgentModelCatalog
	quota   domain.SwarmQuotaSnapshot
	policy  domain.SwarmRoutingPolicy
	now     time.Time
}

func int64Value(value int64) *int64       { return &value }
func float64Value(value float64) *float64 { return &value }

func newRouteFixture() routeFixture {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	worker := domain.SwarmSelection{Harness: "codex", Model: "worker", Effort: "standard"}
	return routeFixture{
		task: Task{Role: "worker", Requested: worker, ContextTokens: 200, ReserveTokens: 100},
		catalog: ports.AgentModelCatalog{
			AgentID: "codex", SelectionMode: ports.ModelSelectionCatalog, InputFingerprint: "catalog-v1",
			FetchedAt: now.Add(-time.Minute), ValidatedAt: now.Add(-time.Minute),
			Models: []ports.AgentModelInfo{
				{ID: "worker", ContextWindow: 1000, Efforts: []string{"light", "standard", "deep", "max"}, DefaultEffort: "standard"},
				{ID: "premium", ContextWindow: 1000, Efforts: []string{"light", "standard", "deep", "max"}, DefaultEffort: "deep"},
			},
			RunnableModels: []string{"worker", "premium"},
		},
		quota: domain.SwarmQuotaSnapshot{
			ID: "quota-1", Bucket: "budget", Revision: 1, ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
			Windows: []domain.SwarmQuotaWindow{
				{ID: "overall", Scope: "*", LimitTokens: int64Value(10000), UsedTokens: int64Value(200), ResetAt: now.Add(time.Hour)},
				{ID: "worker-meter", Scope: "worker", LimitTokens: int64Value(5000), UsedTokens: int64Value(100), ResetAt: now.Add(time.Hour)},
				{ID: "premium-meter", Scope: "premium", LimitTokens: int64Value(5000), UsedTokens: int64Value(100), ResetAt: now.Add(time.Hour)},
			},
		},
		policy: domain.SwarmRoutingPolicy{
			ID: "policy-1", AccountID: "test-account", Bucket: "budget", PremiumTokenLimit: 2000,
			Defaults: map[string]domain.SwarmSelection{"worker": worker},
			Models: []domain.SwarmRouteModel{
				{Harness: "codex", Model: "worker", Roles: []string{"worker", "reviewer"}, QuotaScope: "worker", MinEffort: "standard", MaxEffort: "deep"},
				{Harness: "codex", Model: "premium", Roles: []string{"planner", "worker"}, Premium: true, QuotaScope: "premium", MinEffort: "light", MaxEffort: "max"},
			},
		},
		now: now,
	}
}

func (f routeFixture) recommend() (RouteAdvice, error) {
	return RecommendRoute(f.task, f.catalog, f.quota, f.policy, f.now)
}

func TestRecommendRouteExplicitSelectionAndBinding(t *testing.T) {
	f := newRouteFixture()
	before := domain.SwarmHash([]any{f.task, f.catalog, f.quota, f.policy})
	advice, err := f.recommend()
	if err != nil {
		t.Fatal(err)
	}
	if advice.Requested != f.task.Requested || advice.Resolved != f.task.Requested || advice.QuotaUnknown {
		t.Fatalf("selection was changed: %+v", advice)
	}
	wantBinding := domain.SwarmRouteBinding{
		Role: "worker", Premium: false, QuotaScope: "worker", PolicyHash: domain.SwarmHash(f.policy),
		QuotaHash: domain.SwarmHash(f.quota), CatalogFingerprint: "catalog-v1", ContextTokens: f.task.ContextTokens,
	}
	if advice.Binding != wantBinding || len(advice.Reasons) == 0 {
		t.Fatalf("missing durable provenance: %+v", advice)
	}
	if before != domain.SwarmHash([]any{f.task, f.catalog, f.quota, f.policy}) {
		t.Fatal("recommendation mutated its inputs")
	}
}

func TestRecommendRouteRejectsExplicitOverrideBypasses(t *testing.T) {
	tests := []struct {
		name string
		edit func(*routeFixture)
		want error
	}{
		{"unknown model", func(f *routeFixture) { f.task.Requested.Model = "unadvertised" }, domain.ErrSwarmInvalid},
		{"unadvertised allowed model", func(f *routeFixture) {
			f.task.Requested.Model = "premium"
			f.catalog.Models = f.catalog.Models[:1]
		}, domain.ErrSwarmNotReady},
		{"unknown effort", func(f *routeFixture) { f.task.Requested.Effort = "ultra" }, domain.ErrSwarmInvalid},
		{"effort below policy", func(f *routeFixture) { f.task.Requested.Effort = "light" }, domain.ErrSwarmInvalid},
		{"effort above policy", func(f *routeFixture) { f.task.Requested.Effort = "max" }, domain.ErrSwarmInvalid},
		{"effort omitted", func(f *routeFixture) { f.task.Requested.Effort = "" }, domain.ErrSwarmInvalid},
		{"explicit shift", func(f *routeFixture) { f.task.EffortShift = 1 }, domain.ErrSwarmInvalid},
		{"role not allowed", func(f *routeFixture) { f.task.Role = "planner" }, domain.ErrSwarmInvalid},
		{"invalid role", func(f *routeFixture) { f.task.Role = "summary" }, domain.ErrSwarmInvalid},
		{"partial selection", func(f *routeFixture) { f.task.Requested.Harness = "" }, domain.ErrSwarmInvalid},
		{"reversed policy bounds", func(f *routeFixture) {
			f.policy.Models[0].MinEffort, f.policy.Models[0].MaxEffort = "deep", "standard"
		}, domain.ErrSwarmInvalid},
		{"unknown policy bound", func(f *routeFixture) { f.policy.Models[0].MaxEffort = "ultra" }, domain.ErrSwarmInvalid},
		{"duplicate effort", func(f *routeFixture) { f.catalog.Models[0].Efforts = []string{"standard", "standard"} }, domain.ErrSwarmInvalid},
		{"duplicate model", func(f *routeFixture) { f.catalog.Models = append(f.catalog.Models, f.catalog.Models[0]) }, domain.ErrSwarmInvalid},
		{"wrong bucket", func(f *routeFixture) { f.quota.Bucket = "other" }, domain.ErrSwarmInvalid},
		{"quota NaN", func(f *routeFixture) { f.quota.Windows[0].UsedPercent = float64Value(math.NaN()) }, domain.ErrSwarmInvalid},
		{"quota positive infinity", func(f *routeFixture) { f.quota.Windows[0].UsedPercent = float64Value(math.Inf(1)) }, domain.ErrSwarmInvalid},
		{"quota negative infinity", func(f *routeFixture) { f.quota.Windows[0].UsedPercent = float64Value(math.Inf(-1)) }, domain.ErrSwarmInvalid},
		{"zero reserve", func(f *routeFixture) { f.task.ReserveTokens = 0 }, domain.ErrSwarmInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRouteFixture()
			tt.edit(&f)
			if _, err := f.recommend(); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRecommendRouteDefaultEffortFollowsProviderOrderAndPolicy(t *testing.T) {
	f := newRouteFixture()
	f.task.Requested = domain.SwarmSelection{}
	f.policy.Defaults["worker"] = domain.SwarmSelection{Harness: "codex", Model: "worker"}
	for _, tt := range []struct {
		shift int
		want  string
	}{{0, "standard"}, {-1, "standard"}, {1, "deep"}, {math.MaxInt, "deep"}, {math.MinInt, "standard"}} {
		f.task.EffortShift = tt.shift
		advice, err := f.recommend()
		if err != nil || advice.Resolved.Effort != tt.want || advice.Resolved.Model != "worker" || advice.Requested != (domain.SwarmSelection{}) {
			t.Fatalf("shift %d: advice=%+v err=%v", tt.shift, advice, err)
		}
	}
	f.catalog.Models[0].DefaultEffort = ""
	if _, err := f.recommend(); !errors.Is(err, domain.ErrSwarmInvalid) {
		t.Fatalf("missing advertised default was inferred: %v", err)
	}
}

func TestRecommendRouteNoQuotaDrivenUpgradeOrModelFallback(t *testing.T) {
	f := newRouteFixture()
	f.task.Requested = domain.SwarmSelection{}
	baseline, err := f.recommend()
	if err != nil {
		t.Fatal(err)
	}
	for i := range f.quota.Windows {
		f.quota.Windows[i].UsedTokens = int64Value(0)
		f.quota.Windows[i].ResetAt = f.now.Add(time.Second)
	}
	spare, err := f.recommend()
	if err != nil || spare.Resolved != baseline.Resolved || spare.Binding.Premium {
		t.Fatalf("spare quota upgraded route: %+v, %v", spare, err)
	}
	f.catalog.Models = f.catalog.Models[1:]
	if _, err := f.recommend(); !errors.Is(err, domain.ErrSwarmNotReady) {
		t.Fatalf("missing default silently fell back to premium: %v", err)
	}
}

func TestRecommendRouteRequiresUsableCatalogAndContext(t *testing.T) {
	tests := []struct {
		name string
		edit func(*routeFixture)
		want error
	}{
		{"auth required", func(f *routeFixture) { f.catalog.WarningCode = ports.ModelCatalogWarningAuthRequired }, ports.ErrAgentAuthRequired},
		{"auth expired", func(f *routeFixture) { f.catalog.WarningCode = ports.ModelCatalogWarningAuthExpired }, ports.ErrAgentAuthRequired},
		{"stale", func(f *routeFixture) { f.catalog.Stale = true }, domain.ErrSwarmStale},
		{"refresh queued", func(f *routeFixture) { f.catalog.RefreshState = "queued" }, domain.ErrSwarmStale},
		{"refresh in progress", func(f *routeFixture) { f.catalog.RefreshState = "refreshing" }, domain.ErrSwarmStale},
		{"refresh failed", func(f *routeFixture) { f.catalog.RefreshState = "error" }, domain.ErrSwarmStale},
		{"refresh recommended", func(f *routeFixture) { f.catalog.RefreshRecommended = true }, domain.ErrSwarmStale},
		{"missing fingerprint", func(f *routeFixture) { f.catalog.InputFingerprint = "" }, domain.ErrSwarmStale},
		{"wrong harness", func(f *routeFixture) { f.catalog.AgentID = "other" }, domain.ErrSwarmStale},
		{"future catalog", func(f *routeFixture) { f.catalog.ValidatedAt = f.now.Add(time.Second) }, domain.ErrSwarmStale},
		{"mode catalog", func(f *routeFixture) { f.catalog.SelectionMode = ports.ModelSelectionModeList }, domain.ErrSwarmNotReady},
		{"not runnable", func(f *routeFixture) { f.catalog.RunnableModels = []string{"premium"} }, domain.ErrSwarmNotReady},
		{"unknown context capacity", func(f *routeFixture) { f.catalog.Models[0].ContextWindow = 0 }, domain.ErrSwarmNotReady},
		{"context exceeded", func(f *routeFixture) { f.task.ContextTokens = 1001 }, domain.ErrSwarmCapacity},
		{"unknown context demand", func(f *routeFixture) { f.task.ContextTokens = 0 }, domain.ErrSwarmInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRouteFixture()
			tt.edit(&f)
			if _, err := f.recommend(); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
	f := newRouteFixture()
	f.task.ContextTokens = 1000
	f.catalog.RunnableModels = nil // No list is unknown; qualification checks launch auth.
	if _, err := f.recommend(); err != nil {
		t.Fatalf("exact context bound rejected: %v", err)
	}
}

func TestRecommendRouteUnknownQuotaRemainsVisible(t *testing.T) {
	for _, shape := range []string{"no windows", "percent only", "unrelated windows", "overall only", "overall and unrelated meter", "partially known"} {
		t.Run(shape, func(t *testing.T) {
			f := newRouteFixture()
			switch shape {
			case "no windows":
				f.quota.Windows = nil
			case "percent only":
				f.quota.Windows = []domain.SwarmQuotaWindow{{ID: "percent", Scope: "worker", UsedPercent: float64Value(10), ResetAt: f.now.Add(time.Hour)}}
			case "unrelated windows":
				f.quota.Windows = f.quota.Windows[2:]
			case "overall only":
				f.quota.Windows = f.quota.Windows[:1]
			case "overall and unrelated meter":
				f.quota.Windows = []domain.SwarmQuotaWindow{f.quota.Windows[0], f.quota.Windows[2]}
			case "partially known":
				f.quota.Windows[1].UsedTokens = nil
			}
			advice, err := f.recommend()
			if !errors.Is(err, domain.ErrSwarmNotReady) || !advice.QuotaUnknown {
				t.Fatalf("unknown quota was treated as capacity: %+v %v", advice, err)
			}
			f.policy.AllowUnknownQuota = true
			advice, err = f.recommend()
			if err != nil || !advice.QuotaUnknown || advice.Resolved != f.task.Requested {
				t.Fatalf("explicit unknown allowance hid uncertainty or changed route: %+v %v", advice, err)
			}
		})
	}
}

func TestRecommendRouteQuotaFreshnessAndIndependentScopes(t *testing.T) {
	f := newRouteFixture()
	f.quota.Windows[2].UsedTokens = int64Value(5000) // Exhausted premium meter.
	if advice, err := f.recommend(); err != nil || advice.Binding.Premium {
		t.Fatalf("independent worker quota was blocked: %+v %v", advice, err)
	}
	f.task.Requested.Model = "premium"
	if _, err := f.recommend(); !errors.Is(err, domain.ErrSwarmCapacity) {
		t.Fatalf("exhausted selected meter allowed: %v", err)
	}
	f = newRouteFixture()
	f.quota.Windows[0].UsedTokens = int64Value(10000)
	for _, model := range []string{"worker", "premium"} {
		f.task.Requested.Model = model
		if _, err := f.recommend(); !errors.Is(err, domain.ErrSwarmCapacity) {
			t.Fatalf("overall exhaustion failed to block %s: %v", model, err)
		}
	}
	for _, edit := range []func(*routeFixture){
		func(f *routeFixture) { f.quota.Windows[1].ResetAt = f.now },
		func(f *routeFixture) { f.quota.ExpiresAt = f.now },
		func(f *routeFixture) { f.quota.ObservedAt = f.now.Add(time.Second) },
	} {
		f = newRouteFixture()
		f.policy.AllowUnknownQuota = true
		edit(&f)
		if _, err := f.recommend(); !errors.Is(err, domain.ErrSwarmStale) {
			t.Fatalf("stale quota accepted under unknown allowance: %v", err)
		}
	}
	f = newRouteFixture()
	f.quota.Windows[1].UsedPercent = float64Value(100)
	f.quota.Windows[1].UsedTokens = nil
	f.policy.AllowUnknownQuota = true
	if _, err := f.recommend(); !errors.Is(err, domain.ErrSwarmCapacity) {
		t.Fatalf("unknown token count bypassed exhausted percentage: %v", err)
	}
}

func TestRecommendRouteModelsWithoutEffort(t *testing.T) {
	f := newRouteFixture()
	f.task.Requested.Effort = ""
	f.catalog.Models[0].Efforts = nil
	f.catalog.Models[0].DefaultEffort = ""
	f.policy.Models[0].MinEffort, f.policy.Models[0].MaxEffort = "", ""
	if _, err := f.recommend(); err != nil {
		t.Fatalf("model with no effort setting rejected: %v", err)
	}
	f.task.Requested.Effort = "standard"
	if _, err := f.recommend(); !errors.Is(err, domain.ErrSwarmInvalid) {
		t.Fatalf("unsupported effort accepted: %v", err)
	}
}

func TestEffortHelpersUseCatalogOrderAndRejectAmbiguity(t *testing.T) {
	order := []string{"low", "medium", "high", "xhigh"}
	for _, tt := range []struct {
		current string
		delta   int
		want    string
	}{{"low", -1, "low"}, {"medium", 1, "high"}, {"high", -1, "medium"}, {"low", math.MaxInt, "xhigh"}, {"xhigh", math.MinInt, "low"}} {
		got, err := ShiftEffort(order, tt.current, tt.delta)
		if err != nil || got != tt.want {
			t.Fatalf("shift %+v: %q %v", tt, got, err)
		}
	}
	for _, supported := range [][]string{{"high", "low"}, {"low", "high"}} {
		got, err := ClampEffort(order, supported, "medium")
		if err != nil || got != "low" {
			t.Fatalf("lower tie was not chosen: %q %v", got, err)
		}
	}
	for _, bad := range [][]string{nil, {""}, {"low", "low"}, {" low"}} {
		if _, err := ShiftEffort(bad, "low", 0); !errors.Is(err, domain.ErrSwarmInvalid) {
			t.Fatalf("bad order %v accepted: %v", bad, err)
		}
	}
	for _, supported := range [][]string{nil, {"unknown"}, {"low", "low"}} {
		if _, err := ClampEffort(order, supported, "medium"); !errors.Is(err, domain.ErrSwarmInvalid) {
			t.Fatalf("ambiguous supported values %v accepted: %v", supported, err)
		}
	}
	if _, err := ShiftEffort(order, "unknown", 1); !errors.Is(err, domain.ErrSwarmInvalid) {
		t.Fatalf("unknown effort assumed a default: %v", err)
	}
}

func TestClampEffortSelectsKnownValueAtCatalogBoundaries(t *testing.T) {
	for _, tt := range []struct {
		order, supported []string
		current, want    string
	}{
		{[]string{"only"}, []string{"only"}, "only", "only"},
		{[]string{"low", "medium", "high"}, []string{"high"}, "low", "high"},
		{[]string{"low", "medium", "high"}, []string{"low"}, "high", "low"},
		{[]string{"low", "medium", "high"}, []string{"high", "medium", "low"}, "medium", "medium"},
	} {
		got, err := ClampEffort(tt.order, tt.supported, tt.current)
		if err != nil || got != tt.want {
			t.Fatalf("clamp %+v: got %q, %v", tt, got, err)
		}
	}
}

func TestWorstWindowPreservesUnknownAndRejectsInvalidObservations(t *testing.T) {
	f := newRouteFixture()
	f.quota.Windows[0].UsedPercent = float64Value(40)
	f.quota.Windows[1].UsedTokens = int64Value(3000)
	before := domain.SwarmHash(f.quota)
	worst, err := WorstWindow(f.quota)
	if err != nil || worst == nil || *worst != 60 || before != domain.SwarmHash(f.quota) {
		t.Fatalf("worst observation: %v %v", worst, err)
	}
	*worst = 99
	if before != domain.SwarmHash(f.quota) {
		t.Fatal("returned pointer aliases the snapshot")
	}
	f.quota.Windows = nil
	if worst, err := WorstWindow(f.quota); err != nil || worst != nil {
		t.Fatalf("empty observations became zero: %v %v", worst, err)
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1), -1} {
		f = newRouteFixture()
		f.quota.Windows[0].UsedPercent = float64Value(invalid)
		if _, err := WorstWindow(f.quota); !errors.Is(err, domain.ErrSwarmInvalid) {
			t.Fatalf("invalid percentage %v accepted: %v", invalid, err)
		}
	}
	f = newRouteFixture()
	f.quota.Windows = append(f.quota.Windows, f.quota.Windows[0])
	if _, err := WorstWindow(f.quota); !errors.Is(err, domain.ErrSwarmInvalid) {
		t.Fatalf("duplicate window identity accepted: %v", err)
	}
}

func TestRecommendRouteDeterministic(t *testing.T) {
	f := newRouteFixture()
	first, err := f.recommend()
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.recommend()
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("same inputs produced different advice: %+v %+v %v", first, second, err)
	}
}
