package domain

import (
	"fmt"
	"math"
	"math/big"
	"time"
)

// SwarmRouteModel limits an allowed model by role, quota scope and effort range.
type SwarmRouteModel struct {
	Harness    AgentHarness `json:"harness"`
	Model      string       `json:"model"`
	Roles      []string     `json:"roles"`
	Premium    bool         `json:"premium"`
	QuotaScope string       `json:"quotaScope"`
	MinEffort  string       `json:"minEffort"`
	MaxEffort  string       `json:"maxEffort"`
}

// SwarmRoutingPolicy binds one immutable policy/budget bucket to a canonical external account.
// Policy rollover is not implemented: historical classification and unresolved
// commitments must not be reset by opening another bucket for that account.
type SwarmRoutingPolicy struct {
	ID                string                    `json:"id"`
	AccountID         string                    `json:"accountId"`
	Bucket            string                    `json:"bucket"`
	PremiumTokenLimit int64                     `json:"premiumTokenLimit"`
	AllowUnknownQuota bool                      `json:"allowUnknownQuota"`
	Defaults          map[string]SwarmSelection `json:"defaults"`
	Models            []SwarmRouteModel         `json:"models"`
}

// SwarmQuotaWindow records an independently observed provider usage window.
type SwarmQuotaWindow struct {
	ID          string    `json:"id"`
	Scope       string    `json:"scope"` // '*' is account-wide; otherwise explicit meter identity
	LimitTokens *int64    `json:"limitTokens,omitempty"`
	UsedTokens  *int64    `json:"usedTokens,omitempty"`
	UsedPercent *float64  `json:"usedPercent,omitempty"`
	ResetAt     time.Time `json:"resetAt"`
}

// SwarmQuotaSnapshot preserves percent-only observations as percent-only. They cannot be converted to
// token headroom without a provider-reported token capacity.
type SwarmQuotaSnapshot struct {
	ID         string             `json:"id"`
	Bucket     string             `json:"bucket"`
	Revision   int64              `json:"revision"`
	ObservedAt time.Time          `json:"observedAt"`
	ExpiresAt  time.Time          `json:"expiresAt"`
	Windows    []SwarmQuotaWindow `json:"windows"`
}

// SwarmRouteBinding pins routing policy, quota and catalog identities for an attempt.
type SwarmRouteBinding struct {
	Role               string `json:"role"`
	Premium            bool   `json:"premium"`
	QuotaScope         string `json:"quotaScope"`
	PolicyHash         string `json:"policyHash"`
	QuotaHash          string `json:"quotaHash"`
	CatalogFingerprint string `json:"catalogFingerprint"`
	CatalogScope       string `json:"catalogScope"`
	ContextTokens      int64  `json:"contextTokens"`
}

// SwarmRoutingAccount pairs an account policy with its current quota snapshot.
type SwarmRoutingAccount struct {
	Policy SwarmRoutingPolicy `json:"policy"`
	Quota  SwarmQuotaSnapshot `json:"quota"`
}

// SwarmUsageReceipt contains immutable observed response facts, not final attempt totals.
// Cache/reasoning counters are subsets; never add them to input+output again.
type SwarmUsageReceipt struct {
	ID               string `json:"id"`
	AttemptID        string `json:"attemptId"`
	OperationID      string `json:"operationId"`
	ProviderID       string `json:"providerId"`
	ResponseID       string `json:"responseId"`
	ReportedModel    string `json:"reportedModel,omitempty"`
	InputTokens      *int64 `json:"inputTokens,omitempty"`
	OutputTokens     *int64 `json:"outputTokens,omitempty"`
	CacheReadTokens  *int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int64 `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens  *int64 `json:"reasoningTokens,omitempty"`
	CostNanos        *int64 `json:"costNanos,omitempty"`
}

// SwarmUsageTotals separates reservations, settled spend and incomplete observations.
type SwarmUsageTotals struct {
	Limit             int64 `json:"limit"`
	Reserved          int64 `json:"reserved"`
	Settled           int64 `json:"settled"`
	Observed          int64 `json:"observed"`
	UnknownResponses  int64 `json:"unknownResponses"`
	UnsettledAttempts int64 `json:"unsettledAttempts"`
}

// SwarmUsageReport aggregates total and premium usage without hiding unknown responses.
type SwarmUsageReport struct {
	Bucket          string           `json:"bucket"`
	ModelMismatches int64            `json:"modelMismatches"`
	Total           SwarmUsageTotals `json:"total"`
	Premium         SwarmUsageTotals `json:"premium"`
}

// SwarmTaskRole resolves the legacy omitted role to worker.
func SwarmTaskRole(t SwarmTaskContract) string {
	if t.Role == "" {
		return "worker"
	}
	return t.Role
}

// ValidateSwarmUsageReceipt rejects invalid identities, negative counts and inconsistent subsets.
func ValidateSwarmUsageReceipt(r SwarmUsageReceipt) error {
	for _, id := range []string{r.ID, r.AttemptID, r.OperationID, r.ProviderID, r.ResponseID} {
		if !SwarmValidID(id) {
			return ErrSwarmInvalid
		}
	}
	if r.ReportedModel != "" && !SwarmValidID(r.ReportedModel) {
		return ErrSwarmInvalid
	}
	for _, v := range []*int64{r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens, r.CostNanos} {
		if v != nil && *v < 0 {
			return ErrSwarmInvalid
		}
	}
	if r.InputTokens != nil && r.OutputTokens != nil && *r.InputTokens > math.MaxInt64-*r.OutputTokens {
		return ErrSwarmInvalid
	}
	for _, v := range []*int64{r.CacheReadTokens, r.CacheWriteTokens} {
		if r.InputTokens != nil && v != nil && *v > *r.InputTokens {
			return ErrSwarmInvalid
		}
	}
	if r.ReasoningTokens != nil && r.OutputTokens != nil && *r.ReasoningTokens > *r.OutputTokens {
		return ErrSwarmInvalid
	}
	return nil
}

// SwarmReceiptTokens sums available input and output counts from a validated receipt.
func SwarmReceiptTokens(r SwarmUsageReceipt) int64 {
	var n int64
	if r.InputTokens != nil {
		n += *r.InputTokens
	}
	if r.OutputTokens != nil {
		n += *r.OutputTokens
	}
	return n
}

// SwarmQuotaUsage supplies local account and scope commitments for quota admission.
type SwarmQuotaUsage struct {
	TotalSpent, TotalReserved, ScopeSpent, ScopeReserved int64
}

// SwarmValidRole reports whether a role is supported by the swarm protocol.
func SwarmValidRole(role string) bool {
	return role == "planner" || role == "worker" || role == "reviewer" || role == "integration"
}

// ValidateSwarmRoutingPolicy checks account identity, allowed models and role defaults.
func ValidateSwarmRoutingPolicy(p SwarmRoutingPolicy) error {
	if !SwarmValidID(p.ID) || !SwarmValidID(p.AccountID) || !SwarmValidID(p.Bucket) || p.PremiumTokenLimit < 0 || len(p.Models) == 0 || len(p.Models) > 256 || len(p.Defaults) > 4 {
		return ErrSwarmInvalid
	}
	seen := map[string]bool{}
	for _, m := range p.Models {
		key := string(m.Harness) + "\x00" + m.Model
		if !SwarmValidID(string(m.Harness)) || !SwarmValidID(m.Model) || !SwarmValidID(m.QuotaScope) || m.QuotaScope == "*" || seen[key] || len(m.Roles) == 0 || len(m.Roles) > 4 || (m.MinEffort != "" && !SwarmValidID(m.MinEffort)) || (m.MaxEffort != "" && !SwarmValidID(m.MaxEffort)) {
			return ErrSwarmInvalid
		}
		seen[key] = true
		roles := map[string]bool{}
		for _, r := range m.Roles {
			if !SwarmValidRole(r) || roles[r] {
				return ErrSwarmInvalid
			}
			roles[r] = true
		}
	}
	for role, s := range p.Defaults {
		if !SwarmValidRole(role) {
			return ErrSwarmInvalid
		}
		if _, ok := SwarmPolicyModel(p, role, s); !ok {
			return ErrSwarmInvalid
		}
	}
	return nil
}

// SwarmPolicyModel finds a model allowed by the policy for the requested role.
func SwarmPolicyModel(p SwarmRoutingPolicy, role string, s SwarmSelection) (SwarmRouteModel, bool) {
	for _, m := range p.Models {
		if m.Harness == s.Harness && m.Model == s.Model {
			for _, r := range m.Roles {
				if r == role {
					return m, true
				}
			}
		}
	}
	return SwarmRouteModel{}, false
}

// ValidateSwarmQuota checks bounded windows and observation values without inferring missing usage.
func ValidateSwarmQuota(q SwarmQuotaSnapshot) error {
	if !SwarmValidID(q.ID) || !SwarmValidID(q.Bucket) || q.Revision < 1 || q.ObservedAt.IsZero() || !q.ExpiresAt.After(q.ObservedAt) || len(q.Windows) > 32 {
		return ErrSwarmInvalid
	}
	seen := map[string]bool{}
	for _, w := range q.Windows {
		if !SwarmValidID(w.ID) || !SwarmValidID(w.Scope) || seen[w.ID] || w.ResetAt.IsZero() {
			return ErrSwarmInvalid
		}
		seen[w.ID] = true
		if w.LimitTokens != nil && *w.LimitTokens <= 0 || w.UsedTokens != nil && *w.UsedTokens < 0 {
			return ErrSwarmInvalid
		}
		if w.UsedPercent != nil && (math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0) {
			return ErrSwarmInvalid
		}
	}
	return nil
}

// CheckSwarmQuota conservatively includes local spend in provider usage checks.
// Provider usage may already include local spent. Counting it again is a
// conservative overestimate until a provider supplies a reconciled watermark.
// Missing observations never become zero; an elapsed reset requires refresh.
func CheckSwarmQuota(q SwarmQuotaSnapshot, scope string, now time.Time, usage SwarmQuotaUsage, requested int64, allowUnknown bool) error {
	if err := ValidateSwarmQuota(q); err != nil {
		return err
	}
	if q.ObservedAt.After(now) || !q.ExpiresAt.After(now) {
		return fmt.Errorf("%w: refresh quota", ErrSwarmStale)
	}
	if !SwarmValidID(scope) || scope == "*" || usage.TotalSpent < 0 || usage.TotalReserved < 0 || usage.ScopeSpent < 0 || usage.ScopeReserved < 0 || requested <= 0 {
		return ErrSwarmInvalid
	}
	unknown, matched := false, false
	for _, w := range q.Windows {
		if w.Scope != "*" && w.Scope != scope {
			continue
		}
		matched = matched || w.Scope == scope
		if !w.ResetAt.After(now) {
			return fmt.Errorf("%w: refresh reset window", ErrSwarmStale)
		}
		if w.UsedPercent != nil && *w.UsedPercent >= 100 {
			return ErrSwarmCapacity
		}
		if w.LimitTokens == nil {
			unknown = true
			continue
		}
		limit, used := *w.LimitTokens, int64(0)
		if w.UsedTokens == nil {
			// Zero is only a lower bound for this calculation, never an
			// observation. Known capacity/percentage still constrain admission;
			// the missing count continues to require explicit unknown allowance.
			unknown = true
		} else {
			used = *w.UsedTokens
		}
		if w.UsedPercent != nil {
			// Rational arithmetic avoids overflow/rounding down near int64 limits.
			fraction := new(big.Rat).SetFloat64(*w.UsedPercent)
			fraction.Mul(fraction, new(big.Rat).SetInt64(limit))
			fraction.Quo(fraction, big.NewRat(100, 1))
			n, rem := new(big.Int), new(big.Int)
			n.QuoRem(fraction.Num(), fraction.Denom(), rem)
			if rem.Sign() > 0 {
				n.Add(n, big.NewInt(1))
			}
			if !n.IsInt64() {
				return ErrSwarmCapacity
			}
			used = max(used, n.Int64())
		}
		localSpent, reserved := usage.ScopeSpent, usage.ScopeReserved
		if w.Scope == "*" {
			localSpent, reserved = usage.TotalSpent, usage.TotalReserved
		}
		if used > limit || localSpent > limit-used || reserved > limit-used-localSpent || requested > limit-used-localSpent-reserved {
			return ErrSwarmCapacity
		}
	}
	if (unknown || !matched) && !allowUnknown {
		return fmt.Errorf("%w: unknown token quota", ErrSwarmNotReady)
	}
	return nil
}
