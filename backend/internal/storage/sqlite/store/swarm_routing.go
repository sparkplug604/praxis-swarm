package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// ConfigureSwarmRouting is coordinator-owned setup, before the bucket's first
// attempt. An existing policy cannot be relabelled to evade premium accounting.
func (s *Store) ConfigureSwarmRouting(ctx context.Context, p domain.SwarmRoutingPolicy) error {
	if err := domain.ValidateSwarmRoutingPolicy(p); err != nil {
		return err
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, e := q.GetSwarmRoutingAccount(ctx, p.Bucket)
		if e == nil {
			if prior.PolicyJson == swarmJSON(p) {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		_, e = q.GetSwarmRoutingAccountByIdentity(ctx, p.AccountID)
		if e == nil {
			return domain.ErrSwarmConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		b, e := q.GetSwarmBudget(ctx, p.Bucket)
		if e != nil {
			return swarmErr(e)
		}
		if p.PremiumTokenLimit > b.TokenLimit {
			return domain.ErrSwarmInvalid
		}
		rows, e := q.ListSwarmBudgetUsage(ctx, p.Bucket)
		if e != nil {
			return e
		}
		if len(rows) != 0 {
			return domain.ErrSwarmConflict
		}
		return q.InsertSwarmRoutingAccount(ctx, gen.InsertSwarmRoutingAccountParams{Bucket: p.Bucket, AccountID: p.AccountID, PolicyJson: swarmJSON(p)})
	})
}

func (s *Store) UpdateSwarmQuota(ctx context.Context, quota domain.SwarmQuotaSnapshot) error {
	if err := domain.ValidateSwarmQuota(quota); err != nil {
		return err
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		row, e := q.GetSwarmRoutingAccount(ctx, quota.Bucket)
		if e != nil {
			return swarmErr(e)
		}
		if row.QuotaJson != "" {
			var old domain.SwarmQuotaSnapshot
			if e = json.Unmarshal([]byte(row.QuotaJson), &old); e != nil {
				return e
			}
			if domain.SwarmHash(old) == domain.SwarmHash(quota) {
				return nil
			}
			if quota.Revision != old.Revision+1 || !quota.ObservedAt.After(old.ObservedAt) {
				return domain.ErrSwarmStale
			}
		} else if quota.Revision != 1 {
			return domain.ErrSwarmStale
		}
		return q.UpdateSwarmQuota(ctx, gen.UpdateSwarmQuotaParams{Bucket: quota.Bucket, QuotaJson: swarmJSON(quota)})
	})
}

func (s *Store) GetSwarmRoutingAccount(ctx context.Context, bucket string) (domain.SwarmRoutingAccount, error) {
	row, err := s.qr.GetSwarmRoutingAccount(ctx, bucket)
	if err != nil {
		return domain.SwarmRoutingAccount{}, swarmErr(err)
	}
	return decodeRoutingAccount(row)
}
func decodeRoutingAccount(row gen.SwarmRoutingAccount) (a domain.SwarmRoutingAccount, err error) {
	if err = json.Unmarshal([]byte(row.PolicyJson), &a.Policy); err != nil {
		return a, err
	}
	if row.QuotaJson == "" {
		return a, domain.ErrSwarmNotReady
	}
	err = json.Unmarshal([]byte(row.QuotaJson), &a.Quota)
	return
}

// checkSwarmRoute runs inside the SAME transaction as the task/resource/budget
// reservation. Catalog validation remains the routing service's responsibility.
func checkSwarmRoute(ctx context.Context, q *gen.Queries, c domain.SwarmClaim, t domain.SwarmTaskContract, bucket string, now time.Time) error {
	row, err := q.GetSwarmRoutingAccount(ctx, bucket)
	if errors.Is(err, sql.ErrNoRows) && c.Routing == nil {
		return nil
	}
	if err != nil {
		return swarmErr(err)
	}
	a, err := decodeRoutingAccount(row)
	if err != nil {
		return err
	}
	r := c.Routing
	if c.Resolved != t.Agent {
		return domain.ErrSwarmConflict
	}
	if r == nil || r.Role != domain.SwarmTaskRole(t) || r.ContextTokens <= 0 || (r.CatalogScope != "" && !domain.SwarmValidID(r.CatalogScope)) || r.PolicyHash != domain.SwarmHash(a.Policy) || r.QuotaHash != domain.SwarmHash(a.Quota) || !domain.SwarmValidID(r.CatalogFingerprint) {
		return domain.ErrSwarmStale
	}
	m, ok := domain.SwarmPolicyModel(a.Policy, r.Role, c.Resolved)
	if !ok || m.Premium != r.Premium || m.QuotaScope != r.QuotaScope {
		return domain.ErrSwarmInvalid
	}
	report, scopes, err := swarmUsageProjection(ctx, q, bucket)
	if err != nil {
		return err
	}
	if report.ModelMismatches > 0 {
		return domain.ErrSwarmConflict
	}
	if !swarmFits(report.Total.Limit, report.Total.Settled, report.Total.Reserved, t.ReserveTokens) {
		return domain.ErrSwarmCapacity
	}
	if r.Premium && !swarmFits(a.Policy.PremiumTokenLimit, report.Premium.Settled, report.Premium.Reserved, t.ReserveTokens) {
		return domain.ErrSwarmCapacity
	}
	scope := scopes[r.QuotaScope]
	return domain.CheckSwarmQuota(a.Quota, r.QuotaScope, now, domain.SwarmQuotaUsage{TotalSpent: report.Total.Settled, TotalReserved: report.Total.Reserved, ScopeSpent: scope.Settled, ScopeReserved: scope.Reserved}, t.ReserveTokens, a.Policy.AllowUnknownQuota)
}

func swarmFits(limit, spent, reserved, requested int64) bool {
	return limit >= 0 && spent >= 0 && reserved >= 0 && requested > 0 && spent <= limit && reserved <= limit-spent && requested <= limit-spent-reserved
}
func swarmAdd(dst *int64, value int64) error {
	if value < 0 || *dst > math.MaxInt64-value {
		return domain.ErrSwarmInvalid
	}
	*dst += value
	return nil
}

func (s *Store) RecordSwarmUsageReceipt(ctx context.Context, in domain.SwarmUsageReceipt) error {
	if err := domain.ValidateSwarmUsageReceipt(in); err != nil {
		return err
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		a, e := loadSwarmAttempt(ctx, q, in.AttemptID)
		if e != nil {
			return e
		}
		if a.OperationID != in.OperationID || a.Routing == nil {
			return domain.ErrSwarmConflict
		}
		u, e := q.GetSwarmUsage(ctx, in.AttemptID)
		if e != nil {
			return e
		}
		prior, e := q.GetSwarmUsageReceipt(ctx, in.ID)
		if e == nil {
			if prior.RecordJson == swarmJSON(in) {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		prior, e = q.GetSwarmResponseReceipt(ctx, gen.GetSwarmResponseReceiptParams{Bucket: u.Bucket, ProviderID: in.ProviderID, ResponseID: in.ResponseID})
		if e == nil {
			var old domain.SwarmUsageReceipt
			if e = json.Unmarshal([]byte(prior.RecordJson), &old); e != nil {
				return e
			}
			old.ID = in.ID // transport redelivery may have a new receipt ID
			if domain.SwarmHash(old) == domain.SwarmHash(in) {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		observed, e := swarmObservedAttempt(ctx, q, in.AttemptID)
		if e != nil {
			return e
		}
		if e = swarmAdd(&observed, domain.SwarmReceiptTokens(in)); e != nil {
			return e
		}
		// Late observations are retained even if they contradict final totals.
		// The projection holds their excess against further admissions.
		return q.InsertSwarmUsageReceipt(ctx, gen.InsertSwarmUsageReceiptParams{ID: in.ID, Bucket: u.Bucket, AttemptID: in.AttemptID, OperationID: in.OperationID, ProviderID: in.ProviderID, ResponseID: in.ResponseID, RecordJson: swarmJSON(in)})
	})
}

func swarmObservedAttempt(ctx context.Context, q *gen.Queries, id string) (int64, error) {
	rows, err := q.ListSwarmAttemptReceipts(ctx, id)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, r := range rows {
		var v domain.SwarmUsageReceipt
		if err = json.Unmarshal([]byte(r.RecordJson), &v); err != nil {
			return 0, err
		}
		if err = swarmAdd(&total, domain.SwarmReceiptTokens(v)); err != nil {
			return 0, err
		}
	}
	return total, nil
}

// Settled is the authoritative final total; Observed is response evidence and
// must not be added to it. Reserved includes observed overruns of open totals.
func (s *Store) GetSwarmUsageReport(ctx context.Context, bucket string) (out domain.SwarmUsageReport, err error) {
	err = s.swarmWrite(ctx, func(q *gen.Queries) error { var e error; out, _, e = swarmUsageProjection(ctx, q, bucket); return e })
	return
}
func swarmUsageProjection(ctx context.Context, q *gen.Queries, bucket string) (domain.SwarmUsageReport, map[string]domain.SwarmUsageTotals, error) {
	out := domain.SwarmUsageReport{Bucket: bucket}
	scopes := map[string]domain.SwarmUsageTotals{}
	b, err := q.GetSwarmBudget(ctx, bucket)
	if err != nil {
		return out, scopes, swarmErr(err)
	}
	out.Total.Limit = b.TokenLimit
	account, err := q.GetSwarmRoutingAccount(ctx, bucket)
	if err != nil {
		return out, scopes, swarmErr(err)
	}
	var p domain.SwarmRoutingPolicy
	if err = json.Unmarshal([]byte(account.PolicyJson), &p); err != nil {
		return out, scopes, err
	}
	out.Premium.Limit = p.PremiumTokenLimit
	rows, err := q.ListSwarmBudgetUsage(ctx, bucket)
	if err != nil {
		return out, scopes, err
	}
	receipts, err := q.ListSwarmUsageReceipts(ctx, bucket)
	if err != nil {
		return out, scopes, err
	}
	byAttempt := map[string][]domain.SwarmUsageReceipt{}
	for _, row := range receipts {
		var r domain.SwarmUsageReceipt
		if err = json.Unmarshal([]byte(row.RecordJson), &r); err != nil {
			return out, scopes, err
		}
		byAttempt[r.AttemptID] = append(byAttempt[r.AttemptID], r)
	}
	for _, row := range rows {
		var a domain.SwarmAttempt
		if err = json.Unmarshal([]byte(row.SpecJson), &a); err != nil {
			return out, scopes, err
		}
		if a.Routing == nil {
			return out, scopes, domain.ErrSwarmConflict
		}
		if row.ReportedModel != "" && row.ReportedModel != a.Resolved.Model {
			out.ModelMismatches++
		}
		x := domain.SwarmUsageTotals{}
		for _, r := range byAttempt[row.AttemptID] {
			if err = swarmAdd(&x.Observed, domain.SwarmReceiptTokens(r)); err != nil {
				return out, scopes, err
			}
			if r.InputTokens == nil || r.OutputTokens == nil {
				x.UnknownResponses++
			}
			if r.ReportedModel != "" && r.ReportedModel != a.Resolved.Model {
				out.ModelMismatches++
			}
		}
		if row.Actual < 0 {
			x.Reserved = max(row.Reserved, x.Observed)
			x.UnsettledAttempts = 1
		} else {
			x.Settled = row.Actual
			if x.Observed > row.Actual {
				x.Reserved = x.Observed - row.Actual
				x.UnsettledAttempts = 1
			}
		}
		if err = addSwarmTotals(&out.Total, x); err != nil {
			return out, scopes, err
		}
		if a.Routing.Premium {
			if err = addSwarmTotals(&out.Premium, x); err != nil {
				return out, scopes, err
			}
		}
		scope := scopes[a.Routing.QuotaScope]
		if err = addSwarmTotals(&scope, x); err != nil {
			return out, scopes, err
		}
		scopes[a.Routing.QuotaScope] = scope
	}
	return out, scopes, nil
}
func addSwarmTotals(dst *domain.SwarmUsageTotals, v domain.SwarmUsageTotals) error {
	for _, pair := range []struct {
		to   *int64
		from int64
	}{{&dst.Reserved, v.Reserved}, {&dst.Settled, v.Settled}, {&dst.Observed, v.Observed}, {&dst.UnknownResponses, v.UnknownResponses}, {&dst.UnsettledAttempts, v.UnsettledAttempts}} {
		if err := swarmAdd(pair.to, pair.from); err != nil {
			return err
		}
	}
	return nil
}
