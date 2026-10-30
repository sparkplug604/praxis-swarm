package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// AuthorizeSwarmExecution admits the first final launch receipt atomically.
// Only true permits the caller's one launch call. Every exact replay returns
// false, including recovery after expiry or cancellation, and never relaunches.
// This trusted coordinator API validates bindings, not permission enforcement.
func (s *Store) AuthorizeSwarmExecution(ctx context.Context, f domain.SwarmFence, r domain.SwarmExecutionReceipt, now time.Time) (authorized bool, err error) {
	if err = domain.ValidateSwarmExecutionReceipt(r); err != nil {
		return false, err
	}
	if now.IsZero() || f.AttemptID != r.AttemptID || f.Owner != r.Owner || f.Generation != r.Generation {
		return false, domain.ErrSwarmInvalid
	}
	err = s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, e := q.FindSwarmExecutionIdentities(ctx, gen.FindSwarmExecutionIdentitiesParams{
			AttemptID: r.AttemptID, OperationID: r.OperationID, SessionID: r.SessionID,
		})
		if e != nil {
			return e
		}
		if len(prior) != 0 {
			if len(prior) == 1 && prior[0].ReceiptJson == swarmJSON(r) {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		if r.CreatedAt.After(now) || !r.ExpiresAt.After(now) {
			return domain.ErrSwarmStale
		}
		a, e := loadSwarmAttempt(ctx, q, f.AttemptID)
		if e != nil {
			return e
		}
		if e = swarmFence(a, f, now); e != nil {
			return e
		}
		if a.State != "dispatching" || a.Schedule == nil {
			return domain.ErrSwarmNotReady
		}
		if a.OperationID != r.OperationID || a.CapabilityFingerprint != r.CapabilityFingerprint ||
			a.Schedule.WorkspaceRoot != r.WorkspaceRoot || a.Schedule.WorkspaceIdentityHash != r.WorkspaceIdentityHash ||
			a.Schedule.SourceManifestHash != r.SourceManifestHash || r.CreatedAt.Before(a.CreatedAt) ||
			(a.SessionID != "" && a.SessionID != r.SessionID) {
			return domain.ErrSwarmConflict
		}
		run, e := q.GetSwarmRun(ctx, a.RunID)
		if e != nil {
			return swarmErr(e)
		}
		if run.Cancelled {
			return domain.ErrSwarmNotReady
		}
		task, e := q.GetSwarmTask(ctx, gen.GetSwarmTaskParams{RunID: a.RunID, TaskID: a.TaskID})
		if e != nil {
			return swarmErr(e)
		}
		contract, e := q.GetSwarmContract(ctx, gen.GetSwarmContractParams{RunID: a.RunID, TaskID: a.TaskID, Version: task.Version})
		if e != nil {
			return swarmErr(e)
		}
		if task.Version != a.Contract.Version || contract.ContractHash != a.ContractHash || run.IntegrationHead != a.BaseCommit {
			return domain.ErrSwarmStale
		}
		binding, e := q.GetSwarmExecutionSchedule(ctx, a.ID)
		if e != nil {
			return swarmErr(e)
		}
		if binding.WorkspaceRoot != r.WorkspaceRoot || binding.PoolID != a.Schedule.PoolID ||
			binding.ProviderKey != a.Schedule.ProviderKey || binding.ProjectID != run.ProjectID ||
			binding.SlotKind != swarmScheduleSlot(a.Contract) {
			return domain.ErrSwarmConflict
		}
		e = q.InsertSwarmExecutionReceipt(ctx, gen.InsertSwarmExecutionReceiptParams{
			AttemptID: r.AttemptID, OperationID: r.OperationID, SessionID: r.SessionID,
			ReceiptHash: domain.SwarmHash(r), ReceiptJson: swarmJSON(r),
		})
		authorized = e == nil
		return e
	})
	if err != nil {
		authorized = false
	}
	return authorized, err
}

func (s *Store) GetSwarmExecution(ctx context.Context, attemptID string) (r domain.SwarmExecutionReceipt, err error) {
	if !domain.SwarmValidID(attemptID) {
		return r, domain.ErrSwarmInvalid
	}
	row, err := s.qr.GetSwarmExecutionReceipt(ctx, attemptID)
	if err != nil {
		return r, swarmErr(err)
	}
	err = json.Unmarshal([]byte(row.ReceiptJson), &r)
	return r, err
}

// RecordSwarmExecutionResult records a launch observation even after its lease
// expired or a different coordinator took over. It cannot grant new execution.
func (s *Store) RecordSwarmExecutionResult(ctx context.Context, r domain.SwarmExecutionResult) error {
	if err := domain.ValidateSwarmExecutionResult(r); err != nil {
		return err
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, err := q.GetSwarmExecutionResult(ctx, r.AttemptID)
		if err == nil {
			if prior.ResultJson == swarmJSON(r) {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		row, err := q.GetSwarmExecutionReceipt(ctx, r.AttemptID)
		if err != nil {
			return swarmErr(err)
		}
		if row.ReceiptHash != r.ReceiptHash || row.SessionID != r.SessionID {
			return domain.ErrSwarmConflict
		}
		var receipt domain.SwarmExecutionReceipt
		if err = json.Unmarshal([]byte(row.ReceiptJson), &receipt); err != nil {
			return err
		}
		if r.CreatedAt.Before(receipt.CreatedAt) {
			return domain.ErrSwarmInvalid
		}
		return q.InsertSwarmExecutionResult(ctx, gen.InsertSwarmExecutionResultParams{
			AttemptID: r.AttemptID, ReceiptHash: r.ReceiptHash, SessionID: r.SessionID, ResultJson: swarmJSON(r),
		})
	})
}

func (s *Store) GetSwarmExecutionResult(ctx context.Context, attemptID string) (r domain.SwarmExecutionResult, err error) {
	if !domain.SwarmValidID(attemptID) {
		return r, domain.ErrSwarmInvalid
	}
	row, err := s.qr.GetSwarmExecutionResult(ctx, attemptID)
	if err != nil {
		return r, swarmErr(err)
	}
	err = json.Unmarshal([]byte(row.ResultJson), &r)
	return r, err
}
