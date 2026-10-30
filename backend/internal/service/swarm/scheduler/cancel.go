package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/qualification"
)

// Stopper is optional: ordinary Runtime implementations cannot be stopped by
// guessing a PID or falling back to AO's unfenced Destroy/Manager.Kill paths.
// A stop acknowledgement is not an Observation of terminal absence.
type Stopper interface {
	Stop(context.Context, domain.SwarmAttempt) (Observation, error)
}

// CancellationAuthority is required in addition to immutable execution receipts.
// A cached attempt or a receipt's original lease cannot authorize a current stop.
type CancellationAuthority interface {
	GetSwarmAttempt(context.Context, string) (domain.SwarmAttempt, error)
	GetSwarmRun(context.Context, string) (domain.SwarmRun, error)
}

// Cancel acts only after CancelSwarmRun has durably recorded irreversible run
// cancellation. It neither creates cancellation intent nor deletes workspaces.
func (s *Service) Cancel(ctx context.Context, id string) error {
	if ctx == nil || !domain.SwarmValidID(s.Owner) || !domain.SwarmValidID(id) || s.Store == nil || s.Runtime == nil {
		return domain.ErrSwarmInvalid
	}
	if s.Runtime.Mode() == qualification.LiveEvidence {
		return errors.Join(qualification.ErrUnverified, ValidateAOSpawnBoundary())
	}
	if s.Runtime.Mode() != qualification.TestEvidence {
		return qualification.ErrUnverified
	}
	stopper, ok := s.Runtime.(Stopper)
	if !ok {
		return qualification.ErrUnverified
	}
	if !s.beginOperation(id) {
		return domain.ErrSwarmNotReady
	}
	defer s.endOperation(id)
	if s.OperationTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.OperationTimeout)
		defer cancel()
	}
	a, err := s.Store.GetSwarmAttempt(ctx, id)
	if err != nil {
		return err
	}
	if a.Owner != s.Owner {
		return domain.ErrSwarmStale
	}
	if a.ID != id {
		return domain.ErrSwarmConflict
	}
	if err = cancellationAuthority(ctx, s.Store, a, s.now); err != nil {
		return err
	}
	observed, err := stopper.Stop(ctx, a)
	if err != nil {
		return err
	}
	return s.recordObservation(ctx, a, observed)
}

// Stop rechecks the current coordinator epoch immediately before the external
// effect. SQLite lease changes and runtime I/O cannot be atomic. Safety across
// that interval comes from stopping only an irreversibly cancelled run, using
// the immutable launch identity SwarmHash(OperationID) across lease takeovers.
// The runtime must check that identity at its own effect boundary. The current
// coordinator lease still fences every resulting durable state transition.
func (r *ExactRuntime) Stop(ctx context.Context, a domain.SwarmAttempt) (Observation, error) {
	unknown := Observation{OperationID: a.OperationID, State: "unknown"}
	if ctx == nil || r.Launcher.Store == nil || r.Launcher.Runtime == nil {
		return unknown, qualification.ErrUnverified
	}
	authority, ok := r.Launcher.Store.(CancellationAuthority)
	if !ok {
		return unknown, qualification.ErrUnverified
	}
	if err := cancellationAuthority(ctx, authority, a, r.now); err != nil {
		return unknown, err
	}
	receipt, err := r.Launcher.Store.GetSwarmExecution(ctx, a.ID)
	if err != nil {
		return unknown, errors.Join(qualification.ErrUnverified, err)
	}
	result, err := r.Launcher.Store.GetSwarmExecutionResult(ctx, a.ID)
	if err != nil {
		return unknown, errors.Join(qualification.ErrUnverified, err)
	}
	if err = validateExactExecution(a, receipt, result); err != nil {
		return unknown, err
	}
	if result.Outcome == "no_effect" {
		// A durable final no_effect result consumes the single-use authorization;
		// unlike an absent receipt/probe, it rules out a pending or future Start.
		if err = cancellationAuthority(ctx, authority, a, r.now); err != nil {
			return unknown, err
		}
		return r.Observe(ctx, a)
	}
	if result.Outcome != "created" || len(result.AdditionalHandles) != 0 {
		return unknown, qualification.ErrUnverified
	}
	stopper, ok := r.Launcher.Runtime.(ports.FencedRuntimeStopper)
	if !ok {
		return unknown, qualification.ErrUnverified
	}
	// Runtime handles are opaque. The immutable result binds this one handle to
	// the receipt; the stop port checks its session/generation at the effect.
	ref := ports.FencedRuntimeRef{Handle: ports.RuntimeHandle{ID: result.HandleID}, SessionID: domain.SessionID(receipt.SessionID), Generation: domain.SwarmHash(receipt.OperationID)}
	if err = cancellationAuthority(ctx, authority, a, r.now); err != nil {
		return unknown, err
	}
	stopped, stopErr := stopper.StopFencedRuntime(ctx, ref)
	var observationErr error
	observed := unknown
	if stopped.Acknowledged {
		// Always observe an acknowledged stop, including error/mismatch replies.
		// PTY cleanup alone never establishes descendant termination.
		observed, observationErr = r.Observe(ctx, a)
	}
	if stopErr != nil || !stopped.Acknowledged || stopped.SessionID != ref.SessionID || stopped.Generation != ref.Generation ||
		stopped.Effect != ports.RuntimeEffectApplied || stopped.Cleanup != ports.RuntimeCleanupSucceeded {
		return unknown, errors.Join(qualification.ErrUnverified, stopErr, observationErr)
	}
	return observed, observationErr
}

func cancellationAuthority(ctx context.Context, store CancellationAuthority, a domain.SwarmAttempt, now func() time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domain.SwarmValidID(a.ID) || !domain.SwarmValidID(a.RunID) || !domain.SwarmValidID(a.OperationID) || !domain.SwarmValidID(a.Owner) || a.Generation < 1 {
		return domain.ErrSwarmInvalid
	}
	run, err := store.GetSwarmRun(ctx, a.RunID)
	if err != nil {
		return err
	}
	if run.ID != a.RunID {
		return domain.ErrSwarmConflict
	}
	if !run.Cancelled {
		return domain.ErrSwarmNotReady
	}
	current, err := store.GetSwarmAttempt(ctx, a.ID)
	if err != nil {
		return err
	}
	if domain.SwarmHash(current) != domain.SwarmHash(a) || !current.LeaseUntil.After(now()) {
		return domain.ErrSwarmStale
	}
	if current.State != "dispatching" && current.State != "running" {
		return domain.ErrSwarmNotReady
	}
	return ctx.Err()
}

func validateExactExecution(a domain.SwarmAttempt, receipt domain.SwarmExecutionReceipt, result domain.SwarmExecutionResult) error {
	if domain.ValidateSwarmExecutionReceipt(receipt) != nil || domain.ValidateSwarmExecutionResult(result) != nil {
		return domain.ErrSwarmConflict
	}
	if receipt.EvidenceMode != string(qualification.TestEvidence) || receipt.AttemptID != a.ID || receipt.OperationID != a.OperationID ||
		receipt.Generation > a.Generation || (receipt.Generation == a.Generation && receipt.Owner != a.Owner) ||
		receipt.CapabilityFingerprint != a.CapabilityFingerprint || receipt.SessionID != exactSessionID(a.OperationID) ||
		(a.SessionID != "" && a.SessionID != receipt.SessionID) || receipt.CreatedAt.Before(a.CreatedAt) ||
		a.Schedule == nil || receipt.WorkspaceRoot != a.Schedule.WorkspaceRoot || receipt.WorkspaceIdentityHash != a.Schedule.WorkspaceIdentityHash ||
		receipt.SourceManifestHash != a.Schedule.SourceManifestHash || result.AttemptID != a.ID || result.SessionID != receipt.SessionID ||
		result.ReceiptHash != domain.SwarmHash(receipt) || result.CreatedAt.Before(receipt.CreatedAt) {
		return domain.ErrSwarmConflict
	}
	// Contradictory summaries and composite outcomes never become absence proof.
	if result.Outcome == "no_effect" && result.RuntimeEffect != "" && result.RuntimeEffect != string(ports.RuntimeEffectNone) {
		return qualification.ErrUnverified
	}
	if result.Outcome == "created" && (len(result.AdditionalHandles) != 0 || (result.RuntimeEffect != "" && result.RuntimeEffect != string(ports.RuntimeEffectApplied))) {
		return qualification.ErrUnverified
	}
	if (result.Outcome == "created" || result.Outcome == "no_effect") && result.RuntimeCleanup != "" && result.RuntimeCleanup != string(ports.RuntimeCleanupNotAttempted) {
		return qualification.ErrUnverified
	}
	return nil
}
