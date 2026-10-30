package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// Every swarm mutation acquires a database writer reservation before reading.
// This also serializes independent Store handles/processes, not just goroutines.
// No runtime/model callback is ever invoked inside this transaction.
func (s *Store) swarmWrite(ctx context.Context, fn func(*gen.Queries) error) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "swarm transaction", func(q *gen.Queries) error {
		if err := q.LockSwarmWriter(ctx); err != nil {
			return err
		}
		return fn(q)
	})
}

func swarmJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func swarmErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrSwarmNotFound
	}
	return err
}
func swarmRun(row gen.SwarmRun) (domain.SwarmRun, error) {
	r := domain.SwarmRun{ID: row.ID, RequestID: row.RequestID, Cancelled: row.Cancelled, IntegrationHead: row.IntegrationHead, CreatedAt: time.Unix(0, row.CreatedNs).UTC()}
	err := json.Unmarshal([]byte(row.PlanJson), &r.Plan)
	return r, err
}
func swarmAttempt(row gen.SwarmAttempt) (domain.SwarmAttempt, error) {
	var a domain.SwarmAttempt
	if err := json.Unmarshal([]byte(row.SpecJson), &a); err != nil {
		return a, err
	}
	a.State, a.Owner, a.Generation = row.State, row.Owner, row.Generation
	a.LeaseUntil = time.Unix(0, row.LeaseNs).UTC()
	a.SessionID, a.ReportedModel, a.SubmissionCommit = row.SessionID, row.ReportedModel, row.SubmissionCommit
	return a, nil
}
func saveSwarmFacts(ctx context.Context, q *gen.Queries, a domain.SwarmAttempt) error {
	return q.UpdateSwarmAttemptFacts(ctx, gen.UpdateSwarmAttemptFactsParams{State: a.State, Owner: a.Owner, Generation: a.Generation, LeaseNs: a.LeaseUntil.UnixNano(), SessionID: a.SessionID, ReportedModel: a.ReportedModel, SubmissionCommit: a.SubmissionCommit, ID: a.ID})
}
func loadSwarmAttempt(ctx context.Context, q *gen.Queries, id string) (domain.SwarmAttempt, error) {
	row, err := q.GetSwarmAttempt(ctx, id)
	if err != nil {
		return domain.SwarmAttempt{}, swarmErr(err)
	}
	return swarmAttempt(row)
}
func swarmFence(a domain.SwarmAttempt, f domain.SwarmFence, now time.Time) error {
	if a.Owner != f.Owner || a.Generation != f.Generation || !a.LeaseUntil.After(now) {
		return domain.ErrSwarmStale
	}
	return nil
}

// CreateSwarmRun atomically stores a normalized plan and its initial contracts, or replays the same request.
func (s *Store) CreateSwarmRun(ctx context.Context, id, requestID string, input domain.SwarmPlan, now time.Time) (out domain.SwarmRun, err error) {
	p, err := domain.NormalizeSwarmPlan(input)
	if err != nil {
		return out, err
	}
	if !domain.SwarmValidID(id) || !domain.SwarmValidID(requestID) || p.Version != 1 {
		return out, domain.ErrSwarmInvalid
	}
	for _, t := range p.Tasks {
		if t.Version != 1 {
			return out, domain.ErrSwarmInvalid
		}
	}
	hash := domain.SwarmHash(p)
	err = s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, e := q.GetSwarmRunByRequest(ctx, requestID)
		if e == nil {
			if prior.RequestHash != hash {
				return domain.ErrSwarmConflict
			}
			out, e = swarmRun(prior)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e := q.InsertSwarmBudget(ctx, gen.InsertSwarmBudgetParams{Bucket: p.BudgetBucket, TokenLimit: p.TokenLimit}); e != nil {
			return e
		}
		budget, e := q.GetSwarmBudget(ctx, p.BudgetBucket)
		if e != nil {
			return e
		}
		if budget.TokenLimit != p.TokenLimit {
			return domain.ErrSwarmConflict
		}
		if e := q.InsertSwarmRun(ctx, gen.InsertSwarmRunParams{ID: id, RequestID: requestID, RequestHash: hash, ProjectID: string(p.ProjectID), Version: p.Version, PlanJson: swarmJSON(p), IntegrationHead: p.SourceCommit, CreatedNs: now.UnixNano()}); e != nil {
			return e
		}
		for _, t := range p.Tasks {
			if e := insertSwarmContract(ctx, q, id, t); e != nil {
				return e
			}
		}
		out = domain.SwarmRun{ID: id, RequestID: requestID, Plan: p, IntegrationHead: p.SourceCommit, CreatedAt: now.UTC()}
		return nil
	})
	return out, err
}

func insertSwarmContract(ctx context.Context, q *gen.Queries, runID string, t domain.SwarmTaskContract) error {
	if err := q.InsertSwarmContract(ctx, gen.InsertSwarmContractParams{RunID: runID, TaskID: t.ID, Version: t.Version, ContractHash: domain.SwarmHash(t), ContractJson: swarmJSON(t)}); err != nil {
		return err
	}
	return q.UpsertSwarmTask(ctx, gen.UpsertSwarmTaskParams{RunID: runID, TaskID: t.ID, Version: t.Version})
}

// GetSwarmRun returns the persisted plan, cancellation flag and integration head.
func (s *Store) GetSwarmRun(ctx context.Context, id string) (domain.SwarmRun, error) {
	r, err := s.qr.GetSwarmRun(ctx, id)
	if err != nil {
		return domain.SwarmRun{}, swarmErr(err)
	}
	return swarmRun(r)
}

// ListSwarmRuns returns a bounded project run history in creation order.
func (s *Store) ListSwarmRuns(ctx context.Context, projectID domain.ProjectID, limit int64) ([]domain.SwarmRun, error) {
	if limit < 1 || limit > 1000 {
		return nil, domain.ErrSwarmInvalid
	}
	rows, err := s.qr.ListSwarmRuns(ctx, gen.ListSwarmRunsParams{ProjectID: string(projectID), Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]domain.SwarmRun, 0, len(rows))
	for _, row := range rows {
		r, e := swarmRun(row)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, nil
}

// ReviseSwarmPlan preserves old contracts. A changed predecessor requires every
// dependent contract to name the new version; NormalizeSwarmPlan validates that.
func (s *Store) ReviseSwarmPlan(ctx context.Context, runID string, expected int64, input domain.SwarmPlan) error {
	p, err := domain.NormalizeSwarmPlan(input)
	if err != nil {
		return err
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		row, e := q.GetSwarmRun(ctx, runID)
		if e != nil {
			return swarmErr(e)
		}
		old, e := swarmRun(row)
		if e != nil {
			return e
		}
		if row.Cancelled || row.Version != expected || p.Version != expected+1 {
			return domain.ErrSwarmStale
		}
		if p.ProjectID != old.Plan.ProjectID || p.SourceCommit != old.Plan.SourceCommit || p.BudgetBucket != old.Plan.BudgetBucket || p.TokenLimit != old.Plan.TokenLimit {
			return domain.ErrSwarmConflict
		}
		attempts, e := q.ListSwarmAttempts(ctx, runID)
		if e != nil {
			return e
		}
		for _, a := range attempts {
			if a.State == "reserved" || a.State == "dispatching" || a.State == "running" || a.State == "submitted" {
				return domain.ErrSwarmConflict
			}
		}
		previous := map[string]domain.SwarmTaskContract{}
		for _, t := range old.Plan.Tasks {
			previous[t.ID] = t
		}
		for _, t := range p.Tasks {
			prior, exists := previous[t.ID]
			if exists && domain.SwarmHash(prior) == domain.SwarmHash(t) {
				delete(previous, t.ID)
				continue
			}
			if (!exists && t.Version != 1) || (exists && t.Version != prior.Version+1) {
				return domain.ErrSwarmStale
			}
			if e := insertSwarmContract(ctx, q, runID, t); e != nil {
				return e
			}
			delete(previous, t.ID)
		}
		if len(previous) > 0 {
			return fmt.Errorf("%w: task removal requires explicit archival", domain.ErrSwarmInvalid)
		}
		return q.ReviseSwarmRun(ctx, gen.ReviseSwarmRunParams{Version: p.Version, PlanJson: swarmJSON(p), ID: runID})
	})
}

// ClaimSwarmTask atomically reserves an attempt, resource locks and budget after validating admission.
func (s *Store) ClaimSwarmTask(ctx context.Context, c domain.SwarmClaim, attemptID, operationID string, now, leaseUntil time.Time) (out domain.SwarmAttempt, err error) {
	if !domain.SwarmValidID(c.RunID) || !domain.SwarmValidID(c.TaskID) || !domain.SwarmValidID(c.RequestID) || !domain.SwarmValidID(c.Owner) || c.Version < 1 ||
		!domain.SwarmValidID(attemptID) || !domain.SwarmValidID(operationID) || !domain.SwarmValidID(c.Resolved.Model) || !domain.SwarmValidID(string(c.Resolved.Harness)) ||
		!domain.SwarmValidHash(c.CapabilityFingerprint, false) || !leaseUntil.After(now) {
		return out, domain.ErrSwarmInvalid
	}
	// Owner and generated IDs are delivery metadata; a restarted coordinator
	// retrieves the same logical request, then must explicitly acquire its lease.
	identity := c
	identity.Owner = ""
	hash := domain.SwarmHash(identity)
	err = s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, e := q.GetSwarmAttemptByRequest(ctx, c.RequestID)
		if e == nil {
			if prior.RequestHash != hash {
				return domain.ErrSwarmConflict
			}
			out, e = swarmAttempt(prior)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		row, e := q.GetSwarmRun(ctx, c.RunID)
		if e != nil {
			return swarmErr(e)
		}
		run, e := swarmRun(row)
		if e != nil {
			return e
		}
		if run.Cancelled {
			return domain.ErrSwarmNotReady
		}
		task, e := q.GetSwarmTask(ctx, gen.GetSwarmTaskParams{RunID: c.RunID, TaskID: c.TaskID})
		if e != nil {
			return swarmErr(e)
		}
		if task.Version != c.Version {
			return domain.ErrSwarmStale
		}
		if task.AcceptedAttempt != "" {
			return domain.ErrSwarmNotReady
		}
		_, e = q.GetSwarmOpenTaskAttempt(ctx, gen.GetSwarmOpenTaskAttemptParams{RunID: c.RunID, TaskID: c.TaskID})
		if e == nil {
			return domain.ErrSwarmConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		contract, e := q.GetSwarmContract(ctx, gen.GetSwarmContractParams{RunID: c.RunID, TaskID: c.TaskID, Version: c.Version})
		if e != nil {
			return e
		}
		var t domain.SwarmTaskContract
		if e := json.Unmarshal([]byte(contract.ContractJson), &t); e != nil {
			return e
		}
		for _, dep := range t.Dependencies {
			d, e := q.GetSwarmTask(ctx, gen.GetSwarmTaskParams{RunID: c.RunID, TaskID: dep.TaskID})
			if e != nil {
				return e
			}
			if d.Version != dep.Version || d.AcceptedAttempt == "" {
				return domain.ErrSwarmNotReady
			}
		}
		active, e := q.CountSwarmActive(ctx, c.RunID)
		if e != nil {
			return e
		}
		if active >= run.Plan.MaxActive {
			return domain.ErrSwarmCapacity
		}
		if e := checkSwarmSchedule(ctx, q, c, t, run); e != nil {
			return e
		}
		resources := swarmClaimResources(t, c.Schedule)
		for _, resource := range resources {
			rows, e := q.ListSwarmResources(ctx, resource.Namespace)
			if e != nil {
				return e
			}
			for _, r := range rows {
				if domain.SwarmResourcesConflict(resource, domain.SwarmResource{Namespace: r.Namespace, Kind: r.Kind, Key: r.ResourceKey}) {
					return domain.ErrSwarmConflict
				}
			}
		}
		// Subtract only after bounds checks: an actual overrun may leave spent
		// above the admission limit, including near int64's maximum.
		budget, e := q.GetSwarmBudget(ctx, run.Plan.BudgetBucket)
		if e != nil {
			return e
		}
		if e := checkSwarmRoute(ctx, q, c, t, run.Plan.BudgetBucket, now); e != nil {
			return e
		}
		if budget.Spent > budget.TokenLimit || budget.Reserved > budget.TokenLimit-budget.Spent || t.ReserveTokens > budget.TokenLimit-budget.Spent-budget.Reserved {
			return domain.ErrSwarmCapacity
		}
		reserved, e := q.ReserveSwarmBudget(ctx, gen.ReserveSwarmBudgetParams{Tokens: t.ReserveTokens, Bucket: run.Plan.BudgetBucket})
		if e != nil {
			return e
		}
		if reserved != 1 {
			return domain.ErrSwarmCapacity
		}
		out = domain.SwarmAttempt{ID: attemptID, RunID: c.RunID, TaskID: c.TaskID, RequestID: c.RequestID, OperationID: operationID, BaseCommit: run.IntegrationHead, Contract: t, ContractHash: contract.ContractHash, Resolved: c.Resolved, CapabilityFingerprint: c.CapabilityFingerprint, State: "reserved", Owner: c.Owner, Generation: 1, LeaseUntil: leaseUntil.UTC(), CreatedAt: now.UTC()}
		out.Routing = c.Routing
		out.Schedule = c.Schedule
		if e := q.InsertSwarmAttempt(ctx, gen.InsertSwarmAttemptParams{ID: out.ID, RunID: out.RunID, TaskID: out.TaskID, Version: t.Version, RequestID: out.RequestID, RequestHash: hash, OperationID: out.OperationID, SpecJson: swarmJSON(out), Owner: out.Owner, LeaseNs: out.LeaseUntil.UnixNano(), CreatedNs: out.CreatedAt.UnixNano()}); e != nil {
			return e
		}
		if e := reserveSwarmSchedule(ctx, q, out, run.Plan.ProjectID); e != nil {
			return e
		}
		for _, r := range resources {
			if e := q.InsertSwarmResource(ctx, gen.InsertSwarmResourceParams{AttemptID: out.ID, Namespace: r.Namespace, Kind: r.Kind, ResourceKey: r.Key}); e != nil {
				return e
			}
		}
		return q.InsertSwarmUsage(ctx, gen.InsertSwarmUsageParams{AttemptID: out.ID, Bucket: run.Plan.BudgetBucket, Reserved: t.ReserveTokens})
	})
	return out, err
}

// GetSwarmAttempt reads the current durable attempt facts and lease.
func (s *Store) GetSwarmAttempt(ctx context.Context, id string) (domain.SwarmAttempt, error) {
	return loadSwarmAttempt(ctx, s.qr, id)
}

// GetSwarmAttemptByRequest resolves a logical claim request without creating another attempt.
func (s *Store) GetSwarmAttemptByRequest(ctx context.Context, id string) (domain.SwarmAttempt, error) {
	if !domain.SwarmValidID(id) {
		return domain.SwarmAttempt{}, domain.ErrSwarmInvalid
	}
	r, err := s.qr.GetSwarmAttemptByRequest(ctx, id)
	if err != nil {
		return domain.SwarmAttempt{}, swarmErr(err)
	}
	return swarmAttempt(r)
}

// GetSwarmContract reads a preserved contract version without fabricating AO CDC
// events. A later UI transport can add pagination/change notifications.
func (s *Store) GetSwarmContract(ctx context.Context, runID, taskID string, version int64) (domain.SwarmTaskContract, error) {
	r, err := s.qr.GetSwarmContract(ctx, gen.GetSwarmContractParams{RunID: runID, TaskID: taskID, Version: version})
	if err != nil {
		return domain.SwarmTaskContract{}, swarmErr(err)
	}
	var out domain.SwarmTaskContract
	err = json.Unmarshal([]byte(r.ContractJson), &out)
	return out, err
}

// ListSwarmEvidence returns the preserved review and evaluation records for an attempt.
func (s *Store) ListSwarmEvidence(ctx context.Context, attemptID string) ([]domain.SwarmEvidence, error) {
	rows, err := s.qr.ListSwarmEvidence(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.SwarmEvidence, 0, len(rows))
	for _, row := range rows {
		var item domain.SwarmEvidence
		if err := json.Unmarshal([]byte(row.RecordJson), &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// GetSwarmIntegration reads the recorded combined acceptance and Git integration evidence.
func (s *Store) GetSwarmIntegration(ctx context.Context, id string) (domain.SwarmIntegration, error) {
	r, err := s.qr.GetSwarmIntegration(ctx, id)
	if err != nil {
		return domain.SwarmIntegration{}, swarmErr(err)
	}
	var out domain.SwarmIntegration
	err = json.Unmarshal([]byte(r.RecordJson), &out)
	return out, err
}

// ListSwarmAttempts returns all attempts for a run in creation order.
func (s *Store) ListSwarmAttempts(ctx context.Context, runID string) ([]domain.SwarmAttempt, error) {
	rows, e := s.qr.ListSwarmAttempts(ctx, runID)
	if e != nil {
		return nil, e
	}
	return swarmAttempts(rows)
}

// ListSwarmDispatches returns a bounded set of reserved, dispatching and running attempts for reconciliation.
func (s *Store) ListSwarmDispatches(ctx context.Context, limit int64) ([]domain.SwarmAttempt, error) {
	if limit < 1 || limit > 1000 {
		return nil, domain.ErrSwarmInvalid
	}
	rows, e := s.qr.ListSwarmDispatches(ctx, limit)
	if e != nil {
		return nil, e
	}
	return swarmAttempts(rows)
}
func swarmAttempts(rows []gen.SwarmAttempt) ([]domain.SwarmAttempt, error) {
	out := make([]domain.SwarmAttempt, 0, len(rows))
	for _, r := range rows {
		a, e := swarmAttempt(r)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, nil
}

// BeginSwarmDispatch grants launch only once. A crash after this transaction
// leaves 'dispatching'; recovery must query AO by OperationID, never launch again.
func (s *Store) BeginSwarmDispatch(ctx context.Context, f domain.SwarmFence, now time.Time) (out domain.SwarmAttempt, launch bool, err error) {
	err = s.swarmWrite(ctx, func(q *gen.Queries) error {
		a, e := loadSwarmAttempt(ctx, q, f.AttemptID)
		if e != nil {
			return e
		}
		if e := swarmFence(a, f, now); e != nil {
			return e
		}
		r, e := q.GetSwarmRun(ctx, a.RunID)
		if e != nil {
			return e
		}
		if r.Cancelled {
			return domain.ErrSwarmNotReady
		}
		out = a
		if a.State != "reserved" {
			return nil
		}
		a.State = "dispatching"
		if e := saveSwarmFacts(ctx, q, a); e != nil {
			return e
		}
		out = a
		launch = true
		return nil
	})
	if err != nil {
		launch = false
	}
	return out, launch, err
}

// TakeOverSwarmLease transfers an expired lease and advances its generation without authorizing another launch.
func (s *Store) TakeOverSwarmLease(ctx context.Context, f domain.SwarmFence, newOwner string, now, until time.Time) (out domain.SwarmAttempt, err error) {
	if !domain.SwarmValidID(newOwner) || !until.After(now) {
		return out, domain.ErrSwarmInvalid
	}
	err = s.swarmWrite(ctx, func(q *gen.Queries) error {
		a, e := loadSwarmAttempt(ctx, q, f.AttemptID)
		if e != nil {
			return e
		}
		if a.Owner != f.Owner || a.Generation != f.Generation || a.LeaseUntil.After(now) || a.Generation == math.MaxInt64 {
			return domain.ErrSwarmStale
		}
		if a.State != "reserved" && a.State != "dispatching" && a.State != "running" && a.State != "submitted" {
			return domain.ErrSwarmNotReady
		}
		a.Owner = newOwner
		a.Generation++
		a.LeaseUntil = until.UTC()
		out = a
		return saveSwarmFacts(ctx, q, a)
	})
	return
}

// RenewSwarmLease extends a current fenced lease without changing its owner or generation.
func (s *Store) RenewSwarmLease(ctx context.Context, f domain.SwarmFence, now, until time.Time) error {
	if !until.After(now) {
		return domain.ErrSwarmInvalid
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		a, e := loadSwarmAttempt(ctx, q, f.AttemptID)
		if e != nil {
			return e
		}
		if e := swarmFence(a, f, now); e != nil {
			return e
		}
		if a.State != "reserved" && a.State != "dispatching" && a.State != "running" && a.State != "submitted" {
			return domain.ErrSwarmNotReady
		}
		if until.Before(a.LeaseUntil) {
			return domain.ErrSwarmInvalid
		}
		a.LeaseUntil = until.UTC()
		return saveSwarmFacts(ctx, q, a)
	})
}

// ConfirmSwarmRunning is reconciliation of a provider-confirmed operation.
// Cancellation does not erase a launch that was already in flight.
func (s *Store) ConfirmSwarmRunning(ctx context.Context, f domain.SwarmFence, sessionID, reportedModel string, now time.Time) error {
	if !domain.SwarmValidID(sessionID) {
		return domain.ErrSwarmInvalid
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		a, e := loadSwarmAttempt(ctx, q, f.AttemptID)
		if e != nil {
			return e
		}
		if e := swarmFence(a, f, now); e != nil {
			return e
		}
		if a.State != "dispatching" && a.State != "running" {
			return domain.ErrSwarmNotReady
		}
		if a.SessionID != "" && a.SessionID != sessionID {
			return domain.ErrSwarmConflict
		}
		if a.ReportedModel != "" && reportedModel != "" && a.ReportedModel != reportedModel {
			return domain.ErrSwarmConflict
		}
		a.SessionID = sessionID
		if reportedModel != "" {
			a.ReportedModel = reportedModel
		}
		a.State = "running"
		return saveSwarmFacts(ctx, q, a)
	})
}

// FinishSwarmAttempt requires coordinator-confirmed runtime stop/absence. Worker self-report
// is insufficient. Unknown usage stays reserved after resources are released.
func (s *Store) FinishSwarmAttempt(ctx context.Context, f domain.SwarmFence, state, commit string, confirmedStopped bool, now time.Time) error {
	if !confirmedStopped || (state != "submitted" && state != "failed" && state != "cancelled") || (state == "submitted" && !domain.SwarmValidHash(commit, true)) || (state != "submitted" && commit != "") {
		return domain.ErrSwarmInvalid
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		a, e := loadSwarmAttempt(ctx, q, f.AttemptID)
		if e != nil {
			return e
		}
		if e := swarmFence(a, f, now); e != nil {
			return e
		}
		if a.State == state && (state != "submitted" || a.SubmissionCommit == commit) {
			return nil
		}
		if a.State == "accepted" || a.State == "failed" || a.State == "cancelled" {
			return domain.ErrSwarmNotReady
		}
		r, e := q.GetSwarmRun(ctx, a.RunID)
		if e != nil {
			return e
		}
		if r.Cancelled && state != "cancelled" {
			return domain.ErrSwarmNotReady
		}
		if state == "submitted" && (a.State != "running" || a.SessionID == "") {
			return domain.ErrSwarmNotReady
		}
		a.State = state
		if state == "submitted" {
			a.SubmissionCommit = commit
		} // Failed/cancelled submissions retain their frozen Git evidence.
		if e := saveSwarmFacts(ctx, q, a); e != nil {
			return e
		}
		return q.ReleaseSwarmResources(ctx, a.ID)
	})
}

// CancelSwarmRun records cancellation intent; it does not stop runtimes or release reservations.
func (s *Store) CancelSwarmRun(ctx context.Context, id string) error {
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		if _, e := q.GetSwarmRun(ctx, id); e != nil {
			return swarmErr(e)
		}
		return q.CancelSwarmRun(ctx, id)
	})
}

// GetSwarmBudget returns the bucket limit, outstanding reservations and settled token total.
func (s *Store) GetSwarmBudget(ctx context.Context, bucket string) (domain.SwarmBudget, error) {
	b, e := s.qr.GetSwarmBudget(ctx, bucket)
	if e != nil {
		return domain.SwarmBudget{}, swarmErr(e)
	}
	return domain.SwarmBudget{Bucket: b.Bucket, Limit: b.TokenLimit, Reserved: b.Reserved, Spent: b.Spent}, nil
}

// SettleSwarmUsage records a terminal attempt's final usage and settles its reservation, preserving overruns.
func (s *Store) SettleSwarmUsage(ctx context.Context, in domain.SwarmUsage) error {
	if !domain.SwarmValidID(in.ReceiptID) || in.ActualTokens < 0 {
		return domain.ErrSwarmInvalid
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		a, e := loadSwarmAttempt(ctx, q, in.AttemptID)
		if e != nil {
			return e
		}
		if a.State == "reserved" || a.State == "dispatching" || a.State == "running" {
			return domain.ErrSwarmNotReady
		}
		u, e := q.GetSwarmUsage(ctx, in.AttemptID)
		if e != nil {
			return e
		}
		if u.Actual >= 0 {
			if u.Actual == in.ActualTokens && u.ReceiptID.String == in.ReceiptID {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		observed, e := swarmObservedAttempt(ctx, q, in.AttemptID)
		if e != nil {
			return e
		}
		if in.ActualTokens < observed {
			return domain.ErrSwarmConflict
		}
		b, e := q.GetSwarmBudget(ctx, u.Bucket)
		if e != nil {
			return e
		}
		if b.Reserved < u.Reserved || in.ActualTokens > math.MaxInt64-b.Spent {
			return domain.ErrSwarmInvalid
		}
		if e := q.SettleSwarmUsage(ctx, gen.SettleSwarmUsageParams{Actual: in.ActualTokens, ReceiptID: sql.NullString{String: in.ReceiptID, Valid: true}, AttemptID: in.AttemptID}); e != nil {
			return e
		}
		// Actual overrun is retained honestly; it blocks future admission rather
		// than rejecting the receipt or claiming an unenforced hard spend cap.
		return q.SettleSwarmBudget(ctx, gen.SettleSwarmBudgetParams{ReservedTokens: u.Reserved, ActualTokens: in.ActualTokens, Bucket: u.Bucket})
	})
}

// RecordSwarmEvidence stores idempotent independent review or evaluation evidence bound to a submitted candidate.
func (s *Store) RecordSwarmEvidence(ctx context.Context, in domain.SwarmEvidence) error {
	if !domain.SwarmValidID(in.ID) || !domain.SwarmValidID(in.ActorID) || (in.Kind != "review" && in.Kind != "evaluation") ||
		(in.Verdict != "pass" && in.Verdict != "fail" && in.Verdict != "inconclusive") || !domain.SwarmValidHash(in.ArtifactHash, false) {
		return domain.ErrSwarmInvalid
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, e := q.GetSwarmEvidence(ctx, in.ID)
		if e == nil {
			if prior.RecordJson == swarmJSON(in) {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		a, e := loadSwarmAttempt(ctx, q, in.AttemptID)
		if e != nil {
			return e
		}
		if a.State != "submitted" || in.ActorID == a.SessionID || in.ActorID == a.ID || in.ActorID == a.Owner ||
			in.ContractHash != a.ContractHash || in.Commit != a.SubmissionCommit || in.OracleHash != a.Contract.OracleHash {
			return domain.ErrSwarmConflict
		}
		return q.InsertSwarmEvidence(ctx, gen.InsertSwarmEvidenceParams{ID: in.ID, AttemptID: in.AttemptID, RecordJson: swarmJSON(in)})
	})
}

// AcceptSwarmIntegration is a trusted integration-controller boundary. Call
// only after protected combined checks and the actual Git integration succeed.
// It validates frozen per-attempt evidence and atomically advances dependencies.
func (s *Store) AcceptSwarmIntegration(ctx context.Context, in domain.SwarmIntegration) error {
	in.AttemptIDs = append([]string(nil), in.AttemptIDs...)
	sort.Strings(in.AttemptIDs)
	if !domain.SwarmValidID(in.ID) || !domain.SwarmValidHash(in.BaseCommit, true) || !domain.SwarmValidHash(in.Commit, true) || in.BaseCommit == in.Commit ||
		len(in.AttemptIDs) == 0 || len(in.AttemptIDs) > 1000 || !domain.SwarmValidID(in.ReviewID) || !domain.SwarmValidID(in.EvaluatorID) || in.ReviewID == in.EvaluatorID ||
		!domain.SwarmValidHash(in.ReviewArtifactHash, false) || !domain.SwarmValidHash(in.EvaluationArtifactHash, false) {
		return domain.ErrSwarmInvalid
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, e := q.GetSwarmIntegration(ctx, in.ID)
		if e == nil {
			if prior.RecordJson == swarmJSON(in) {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		r, e := q.GetSwarmRun(ctx, in.RunID)
		if e != nil {
			return swarmErr(e)
		}
		if r.Cancelled || r.IntegrationHead != in.BaseCommit {
			return domain.ErrSwarmStale
		}
		for i, id := range in.AttemptIDs {
			if i > 0 && id == in.AttemptIDs[i-1] {
				return domain.ErrSwarmInvalid
			}
			a, e := loadSwarmAttempt(ctx, q, id)
			if e != nil {
				return e
			}
			if a.RunID != in.RunID || a.State != "submitted" || a.SessionID == in.ReviewID || a.SessionID == in.EvaluatorID || a.Owner == in.ReviewID || a.Owner == in.EvaluatorID {
				return domain.ErrSwarmConflict
			}
			t, e := q.GetSwarmTask(ctx, gen.GetSwarmTaskParams{RunID: in.RunID, TaskID: a.TaskID})
			if e != nil {
				return e
			}
			if t.Version != a.Contract.Version || t.AcceptedAttempt != "" {
				return domain.ErrSwarmStale
			}
			proofs, e := q.ListSwarmEvidence(ctx, id)
			if e != nil {
				return e
			}
			reviewer, evaluator := "", ""
			for _, row := range proofs {
				var proof domain.SwarmEvidence
				if e := json.Unmarshal([]byte(row.RecordJson), &proof); e != nil {
					return e
				}
				if proof.Verdict != "pass" {
					return domain.ErrSwarmNotReady
				}
				if proof.Kind == "review" {
					reviewer = proof.ActorID
				} else {
					evaluator = proof.ActorID
				}
			}
			if reviewer == "" || evaluator == "" || reviewer == evaluator {
				return domain.ErrSwarmNotReady
			}
			a.State = "accepted"
			if e := saveSwarmFacts(ctx, q, a); e != nil {
				return e
			}
			if e := q.AcceptSwarmTask(ctx, gen.AcceptSwarmTaskParams{AcceptedAttempt: id, RunID: in.RunID, TaskID: a.TaskID, Version: a.Contract.Version}); e != nil {
				return e
			}
		}
		if e := q.InsertSwarmIntegration(ctx, gen.InsertSwarmIntegrationParams{ID: in.ID, RunID: in.RunID, RecordJson: swarmJSON(in)}); e != nil {
			return e
		}
		return q.UpdateSwarmIntegrationHead(ctx, gen.UpdateSwarmIntegrationHeadParams{IntegrationHead: in.Commit, ID: in.RunID})
	})
}
