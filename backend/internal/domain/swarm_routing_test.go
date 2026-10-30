package domain

import (
	"errors"
	"math"
	"testing"
	"time"
)

func routingInt(value int64) *int64       { return &value }
func routingFloat(value float64) *float64 { return &value }

func routingQuotaFixture() (SwarmQuotaSnapshot, time.Time) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	return SwarmQuotaSnapshot{
		ID: "quota", Bucket: "budget", Revision: 1,
		ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
		Windows: []SwarmQuotaWindow{
			{ID: "overall", Scope: "*", LimitTokens: routingInt(5000), UsedTokens: routingInt(500), ResetAt: now.Add(time.Hour)},
			{ID: "worker-hour", Scope: "worker", LimitTokens: routingInt(1000), UsedTokens: routingInt(100), ResetAt: now.Add(time.Hour)},
			{ID: "premium-week", Scope: "premium", LimitTokens: routingInt(1000), UsedTokens: routingInt(100), ResetAt: now.Add(24 * time.Hour)},
		},
	}, now
}

func TestCheckSwarmQuotaRequiresExactMeterAndPreservesUnknown(t *testing.T) {
	for _, shape := range []string{"empty", "overall only", "other meter", "percent only", "incomplete overall"} {
		t.Run(shape, func(t *testing.T) {
			q, now := routingQuotaFixture()
			switch shape {
			case "empty":
				q.Windows = nil
			case "overall only":
				q.Windows = q.Windows[:1]
			case "other meter":
				q.Windows = q.Windows[2:]
			case "percent only":
				q.Windows = q.Windows[1:2]
				q.Windows[0].LimitTokens, q.Windows[0].UsedTokens = nil, nil
				q.Windows[0].UsedPercent = routingFloat(10)
			case "incomplete overall":
				q.Windows[0].UsedTokens = nil
			}
			before := SwarmHash(q)
			if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, 1, false); !errors.Is(err, ErrSwarmNotReady) {
				t.Fatalf("unknown meter accepted without allowance: %v", err)
			}
			if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, 1, true); err != nil {
				t.Fatalf("explicit unknown allowance rejected: %v", err)
			}
			if before != SwarmHash(q) {
				t.Fatal("checking unknown quota synthesized observations")
			}
		})
	}
}

func TestCheckSwarmQuotaIsolatesMetersAndAppliesOverallUsage(t *testing.T) {
	q, now := routingQuotaFixture()
	q.Windows[2].UsedTokens = routingInt(1000)
	if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, 100, false); err != nil {
		t.Fatalf("exhausted unrelated premium meter blocked worker: %v", err)
	}
	if err := CheckSwarmQuota(q, "premium", now, SwarmQuotaUsage{}, 1, false); !errors.Is(err, ErrSwarmCapacity) {
		t.Fatalf("selected exhausted meter accepted: %v", err)
	}
	q.Windows[2].ResetAt = now.Add(-time.Second)
	if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, 100, false); err != nil {
		t.Fatalf("stale unrelated meter blocked worker: %v", err)
	}
	q, now = routingQuotaFixture()
	q.Windows[0].UsedTokens = routingInt(5000)
	for _, scope := range []string{"worker", "premium"} {
		if err := CheckSwarmQuota(q, scope, now, SwarmQuotaUsage{}, 1, false); !errors.Is(err, ErrSwarmCapacity) {
			t.Fatalf("overall exhaustion did not block %s: %v", scope, err)
		}
	}
	q, now = routingQuotaFixture()
	usage := SwarmQuotaUsage{TotalSpent: 4000, TotalReserved: 400, ScopeSpent: 0, ScopeReserved: 0}
	if err := CheckSwarmQuota(q, "worker", now, usage, 100, false); err != nil {
		t.Fatalf("exact overall allowance rejected: %v", err)
	}
	if err := CheckSwarmQuota(q, "worker", now, usage, 101, false); !errors.Is(err, ErrSwarmCapacity) {
		t.Fatalf("overall counter overflow allowed: %v", err)
	}
	q.Windows = q.Windows[1:2]
	usage = SwarmQuotaUsage{TotalSpent: 9000, TotalReserved: 9000, ScopeSpent: 200, ScopeReserved: 200}
	if err := CheckSwarmQuota(q, "worker", now, usage, 500, false); err != nil {
		t.Fatalf("exact scope allowance rejected or wrong counters used: %v", err)
	}
	if err := CheckSwarmQuota(q, "worker", now, usage, 501, false); !errors.Is(err, ErrSwarmCapacity) {
		t.Fatalf("scope reservation overflow allowed: %v", err)
	}
}

func TestCheckSwarmQuotaRefreshNeverAssumesResetUsage(t *testing.T) {
	for _, change := range []struct {
		name string
		edit func(*SwarmQuotaSnapshot, time.Time)
	}{
		{"reset equality", func(q *SwarmQuotaSnapshot, now time.Time) { q.Windows[1].ResetAt = now }},
		{"reset elapsed", func(q *SwarmQuotaSnapshot, now time.Time) { q.Windows[1].ResetAt = now.Add(-time.Hour) }},
		{"overall reset elapsed", func(q *SwarmQuotaSnapshot, now time.Time) { q.Windows[0].ResetAt = now.Add(-time.Hour) }},
		{"expired", func(q *SwarmQuotaSnapshot, now time.Time) { q.ExpiresAt = now }},
		{"future observation", func(q *SwarmQuotaSnapshot, now time.Time) { q.ObservedAt = now.Add(time.Second) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			q, now := routingQuotaFixture()
			q.Windows[1].UsedTokens = routingInt(999)
			change.edit(&q, now)
			before := SwarmHash(q)
			if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, 1, true); !errors.Is(err, ErrSwarmStale) {
				t.Fatalf("stale observation accepted with unknown allowance: %v", err)
			}
			if before != SwarmHash(q) || *q.Windows[1].UsedTokens != 999 {
				t.Fatal("refresh condition reset observed usage")
			}
		})
	}
}

func TestCheckSwarmQuotaUsesConservativePercentageAndTokenCounts(t *testing.T) {
	for _, tt := range []struct {
		name      string
		limit     int64
		used      int64
		percent   *float64
		remaining int64
	}{
		{"percentage larger", 100, 1, routingFloat(75), 25},
		{"tokens larger", 100, 80, routingFloat(20), 20},
		{"fraction rounds up", 3, 0, routingFloat(50), 1},
		{"maximum integer half", math.MaxInt64, 0, routingFloat(50), math.MaxInt64 / 2},
		{"maximum integer token count", math.MaxInt64, math.MaxInt64 - 1, nil, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q, now := routingQuotaFixture()
			q.Windows = q.Windows[1:2]
			q.Windows[0].LimitTokens, q.Windows[0].UsedTokens, q.Windows[0].UsedPercent = routingInt(tt.limit), routingInt(tt.used), tt.percent
			if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, tt.remaining, false); err != nil {
				t.Fatalf("exact remaining capacity rejected: %v", err)
			}
			if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, tt.remaining+1, false); !errors.Is(err, ErrSwarmCapacity) {
				t.Fatalf("contradictory/rounded usage understated: %v", err)
			}
		})
	}
	q, now := routingQuotaFixture()
	q.Windows = q.Windows[1:2]
	q.Windows[0].UsedTokens = nil
	q.Windows[0].UsedPercent = routingFloat(100)
	if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, 1, true); !errors.Is(err, ErrSwarmCapacity) {
		t.Fatalf("unknown allowance bypassed full reported percentage: %v", err)
	}
}

func TestCheckSwarmQuotaRejectsInvalidAndOverflowingInputs(t *testing.T) {
	for _, percent := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		q, now := routingQuotaFixture()
		q.Windows[1].UsedPercent = routingFloat(percent)
		if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, 1, true); !errors.Is(err, ErrSwarmInvalid) {
			t.Fatalf("invalid percent %v accepted: %v", percent, err)
		}
	}
	for _, usage := range []SwarmQuotaUsage{{TotalSpent: -1}, {TotalReserved: -1}, {ScopeSpent: -1}, {ScopeReserved: -1}} {
		q, now := routingQuotaFixture()
		if err := CheckSwarmQuota(q, "worker", now, usage, 1, true); !errors.Is(err, ErrSwarmInvalid) {
			t.Fatalf("invalid usage accepted: %+v %v", usage, err)
		}
	}
	q, now := routingQuotaFixture()
	q.Windows = q.Windows[1:2]
	q.Windows[0].LimitTokens, q.Windows[0].UsedTokens = routingInt(math.MaxInt64), routingInt(0)
	usage := SwarmQuotaUsage{ScopeSpent: math.MaxInt64 - 1, ScopeReserved: 1}
	if err := CheckSwarmQuota(q, "worker", now, usage, 1, false); !errors.Is(err, ErrSwarmCapacity) {
		t.Fatalf("integer overflow bypassed admission: %v", err)
	}
	for _, scope := range []string{"", "*"} {
		if err := CheckSwarmQuota(q, scope, now, SwarmQuotaUsage{}, 1, true); !errors.Is(err, ErrSwarmInvalid) {
			t.Fatalf("invalid selected scope %q accepted: %v", scope, err)
		}
	}
	for _, requested := range []int64{0, -1} {
		if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, requested, true); !errors.Is(err, ErrSwarmInvalid) {
			t.Fatalf("invalid reservation accepted: %v", err)
		}
	}
}

func TestCheckSwarmQuotaUnknownAllowanceStillEnforcesKnownCapacity(t *testing.T) {
	for _, tt := range []struct {
		name      string
		percent   *float64
		remaining int64
	}{
		{"percentage lower bound", routingFloat(90), 10},
		{"capacity alone", nil, 100},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q, now := routingQuotaFixture()
			q.Windows = q.Windows[1:2]
			q.Windows[0].LimitTokens = routingInt(100)
			q.Windows[0].UsedTokens = nil
			q.Windows[0].UsedPercent = tt.percent
			before := SwarmHash(q)
			if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, tt.remaining+1, true); !errors.Is(err, ErrSwarmCapacity) {
				t.Fatalf("unknown allowance bypassed known headroom: %v", err)
			}
			if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, tt.remaining, true); err != nil {
				t.Fatalf("explicit unknown allowance within known headroom rejected: %v", err)
			}
			if err := CheckSwarmQuota(q, "worker", now, SwarmQuotaUsage{}, tt.remaining, false); !errors.Is(err, ErrSwarmNotReady) {
				t.Fatalf("missing usage count became known: %v", err)
			}
			usage := SwarmQuotaUsage{ScopeReserved: 1}
			if err := CheckSwarmQuota(q, "worker", now, usage, tt.remaining, true); !errors.Is(err, ErrSwarmCapacity) {
				t.Fatalf("existing reservation ignored with missing usage: %v", err)
			}
			if before != SwarmHash(q) || q.Windows[0].UsedTokens != nil {
				t.Fatal("unknown usage observation was replaced")
			}
		})
	}
}

func routingReceiptFixture() SwarmUsageReceipt {
	return SwarmUsageReceipt{
		ID: "receipt", AttemptID: "attempt", OperationID: "operation", ProviderID: "provider", ResponseID: "response", ReportedModel: "model",
		InputTokens: routingInt(10), OutputTokens: routingInt(5), CacheReadTokens: routingInt(6), CacheWriteTokens: routingInt(3), ReasoningTokens: routingInt(4), CostNanos: routingInt(1000),
	}
}

func TestSwarmUsageReceiptPreservesUnknownAndDoesNotAddSubsets(t *testing.T) {
	r := routingReceiptFixture()
	before := SwarmHash(r)
	if err := ValidateSwarmUsageReceipt(r); err != nil {
		t.Fatal(err)
	}
	if tokens := SwarmReceiptTokens(r); tokens != 15 {
		t.Fatalf("metadata/subsets counted as extra tokens: %d", tokens)
	}
	if before != SwarmHash(r) {
		t.Fatal("validation changed receipt evidence")
	}
	r.InputTokens, r.OutputTokens = nil, nil
	if err := ValidateSwarmUsageReceipt(r); err != nil || SwarmReceiptTokens(r) != 0 || r.InputTokens != nil || r.OutputTokens != nil {
		t.Fatalf("unknown counts were rejected or replaced: %+v %v", r, err)
	}
	r.OutputTokens = routingInt(5)
	if err := ValidateSwarmUsageReceipt(r); err != nil || SwarmReceiptTokens(r) != 5 || r.InputTokens != nil {
		t.Fatalf("partially known receipt miscounted: %+v %v", r, err)
	}
}

func TestSwarmUsageReceiptRejectsAmbiguousIdentityAndInvalidCounts(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*SwarmUsageReceipt)
	}{
		{"receipt identity", func(r *SwarmUsageReceipt) { r.ID = "" }},
		{"attempt identity", func(r *SwarmUsageReceipt) { r.AttemptID = "" }},
		{"operation identity", func(r *SwarmUsageReceipt) { r.OperationID = "" }},
		{"provider identity", func(r *SwarmUsageReceipt) { r.ProviderID = "" }},
		{"response identity", func(r *SwarmUsageReceipt) { r.ResponseID = "" }},
		{"reported identity", func(r *SwarmUsageReceipt) { r.ReportedModel = " model" }},
		{"negative input", func(r *SwarmUsageReceipt) { r.InputTokens = routingInt(-1) }},
		{"negative output", func(r *SwarmUsageReceipt) { r.OutputTokens = routingInt(-1) }},
		{"negative cost", func(r *SwarmUsageReceipt) { r.CostNanos = routingInt(-1) }},
		{"input output overflow", func(r *SwarmUsageReceipt) { r.InputTokens = routingInt(math.MaxInt64) }},
		{"cache read exceeds input", func(r *SwarmUsageReceipt) { r.CacheReadTokens = routingInt(11) }},
		{"cache write exceeds input", func(r *SwarmUsageReceipt) { r.CacheWriteTokens = routingInt(11) }},
		{"reasoning exceeds output", func(r *SwarmUsageReceipt) { r.ReasoningTokens = routingInt(6) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := routingReceiptFixture()
			tt.edit(&r)
			if err := ValidateSwarmUsageReceipt(r); !errors.Is(err, ErrSwarmInvalid) {
				t.Fatalf("invalid receipt accepted: %v", err)
			}
		})
	}
}

func TestSwarmRoutingPolicyRequiresAccountAndMeterIdentity(t *testing.T) {
	fixture := func() SwarmRoutingPolicy {
		return SwarmRoutingPolicy{
			ID: "policy", AccountID: "account", Bucket: "budget", PremiumTokenLimit: 100,
			Defaults: map[string]SwarmSelection{"worker": {Harness: "codex", Model: "model"}},
			Models:   []SwarmRouteModel{{Harness: "codex", Model: "model", QuotaScope: "worker", Roles: []string{"worker"}}},
		}
	}
	if err := ValidateSwarmRoutingPolicy(fixture()); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*SwarmRoutingPolicy){
		func(p *SwarmRoutingPolicy) { p.AccountID = "" },
		func(p *SwarmRoutingPolicy) { p.Models[0].QuotaScope = "" },
		func(p *SwarmRoutingPolicy) { p.Models[0].QuotaScope = "*" },
		func(p *SwarmRoutingPolicy) { p.Models = append(p.Models, p.Models[0]) },
		func(p *SwarmRoutingPolicy) { p.Models[0].Roles = []string{"worker", "worker"} },
		func(p *SwarmRoutingPolicy) { p.Defaults["planner"] = p.Defaults["worker"] },
	} {
		p := fixture()
		edit(&p)
		if err := ValidateSwarmRoutingPolicy(p); !errors.Is(err, ErrSwarmInvalid) {
			t.Fatalf("invalid account/meter policy accepted: %+v %v", p, err)
		}
	}
}
