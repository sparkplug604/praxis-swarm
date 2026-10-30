package sessionmanager

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ErrSwarmExactLiveUnsupported rejects live launches until concrete confinement is qualified.
var ErrSwarmExactLiveUnsupported = errors.New("exact swarm launch requires qualified concrete runtime confinement; live execution is unavailable")
var swarmRuntimeSessionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// SwarmExecutionStore is the existing AO SQLite authority, not an additional
// session registry. Authorization is a durable single-use launch decision.
type SwarmExecutionStore interface {
	AuthorizeSwarmExecution(context.Context, domain.SwarmFence, domain.SwarmExecutionReceipt, time.Time) (bool, error)
	RecordSwarmExecutionResult(context.Context, domain.SwarmExecutionResult) error
	GetSwarmExecution(context.Context, string) (domain.SwarmExecutionReceipt, error)
	GetSwarmExecutionResult(context.Context, string) (domain.SwarmExecutionResult, error)
}

// SwarmExactLauncher is the internal, test-qualified bridge to AO's Runtime
// port. It bypasses interactive Spawn provisioning, configuration inheritance,
// naming and rollback, so ordinary AO lifecycle operations do not own these
// copied workspaces. It never deletes a workspace or retries Runtime.Create.
// There is no new subprocess implementation here.
//
// Live remains disabled: AO runtimes inherit ambient environment and the hybrid
// runtime can fall back after an ambiguous Create. Concrete backend identity,
// process containment and cancellation must be qualified before enabling it.
// No ordinary AO session/UI record is created by this infrastructure bridge.
type SwarmExactLauncher struct {
	Store   SwarmExecutionStore
	Runtime ports.Runtime
	// Validate is coordinator-owned and rechecks the packet, workspace and final
	// qualification against fresh source/state. It cannot rewrite launch inputs.
	Validate func(context.Context, domain.SwarmExecutionReceipt, ports.RuntimeConfig) error
	Now      func() time.Time
}

func (l *SwarmExactLauncher) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// Start returns an existing durable outcome on identical replay; an authorized
// operation without an outcome is inconclusive and never relaunched. A process
// may outlive a failed/cancelled request, so its result is saved independently of
// that request's cancellation. Failure to save it remains an explicit error.
func (l *SwarmExactLauncher) Start(ctx context.Context, receipt domain.SwarmExecutionReceipt, cfg ports.RuntimeConfig) (domain.SwarmExecutionResult, error) {
	var result domain.SwarmExecutionResult
	if ctx == nil || l.Store == nil || l.Runtime == nil || l.Validate == nil {
		return result, domain.ErrSwarmInvalid
	}
	if receipt.EvidenceMode != "test" {
		return result, ErrSwarmExactLiveUnsupported
	}
	if err := domain.ValidateSwarmExecutionReceipt(receipt); err != nil {
		return result, err
	}
	cfg = cloneSwarmRuntimeConfig(cfg)
	if cfg.SessionID != domain.SessionID(receipt.SessionID) || cfg.WorkspacePath != receipt.WorkspaceRoot ||
		!filepath.IsAbs(cfg.WorkspacePath) || len(cfg.Argv) == 0 || !filepath.IsAbs(cfg.Argv[0]) ||
		!swarmRuntimeSessionID.MatchString(receipt.SessionID) || cfg.Env["AO_RUNTIME_LAUNCH_ID"] != domain.SwarmHash(receipt.OperationID) ||
		!cfg.ExitOnCommandCompletion || cfg.StartOnAttach || domain.SwarmHash(cfg) != receipt.ConfigHash {
		return result, domain.ErrSwarmConflict
	}
	// Replays do not need pristine worker files or fresh qualification. They
	// recover ownership only; no replay path is permission to launch again.
	prior, err := l.Store.GetSwarmExecution(ctx, receipt.AttemptID)
	if err == nil {
		if domain.SwarmHash(prior) != domain.SwarmHash(receipt) {
			return result, domain.ErrSwarmConflict
		}
		return l.replay(ctx, receipt.AttemptID)
	}
	if !errors.Is(err, domain.ErrSwarmNotFound) {
		return result, err
	}
	if err := l.Validate(ctx, receipt, cloneSwarmRuntimeConfig(cfg)); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	won, err := l.Store.AuthorizeSwarmExecution(ctx, domain.SwarmFence{AttemptID: receipt.AttemptID, Owner: receipt.Owner, Generation: receipt.Generation}, receipt, l.now())
	if err != nil {
		return result, err
	}
	if !won {
		return l.replay(ctx, receipt.AttemptID)
	}
	result = domain.SwarmExecutionResult{AttemptID: receipt.AttemptID, ReceiptHash: domain.SwarmHash(receipt), SessionID: receipt.SessionID}
	if err = ctx.Err(); err == nil && !receipt.ExpiresAt.After(l.now()) {
		err = domain.ErrSwarmStale
	}
	if err != nil {
		result.Outcome = "no_effect"
	} else {
		var handle ports.RuntimeHandle
		handle, err = l.Runtime.Create(ctx, cloneSwarmRuntimeConfig(cfg))
		result.HandleID = handle.ID
		result.Outcome = "unknown"
		if err == nil && handle.ID != "" {
			result.Outcome = "created"
		} else if err == nil {
			err = errors.New("runtime returned no owned handle")
		} else {
			recordSwarmRuntimeError(&result, err)
		}
	}
	result.CreatedAt = l.now()
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return result, errors.Join(err, l.Store.RecordSwarmExecutionResult(saveCtx, result))
}

func recordSwarmRuntimeError(result *domain.SwarmExecutionResult, err error) {
	returnedHandle := result.HandleID
	var handles []string
	var effect ports.RuntimeEffectError
	if errors.As(err, &effect) {
		result.RuntimeEffect, result.RuntimeCleanup = string(effect.EffectOutcome()), string(effect.CleanupOutcome())
		handles = append(handles, effect.PossibleHandle().ID)
	}
	// errors.As selects only the first branch. A composite runtime can return a
	// no-effect error followed by an uncertain fallback, so traverse the complete
	// error tree before drawing any absence conclusion or discarding ownership.
	joined := false
	var visit func(error)
	visit = func(current error) {
		if current == nil {
			return
		}
		var typed ports.RuntimeEffectError
		if errors.As(current, &typed) {
			handles = append(handles, typed.PossibleHandle().ID)
		}
		// Visit single wrappers before looking for joins so effect-bearing
		// intermediate wrappers cannot lose their own possible handles.
		if child := errors.Unwrap(current); child != nil {
			visit(child)
			return
		}
		var wrapped interface{ Unwrap() []error }
		if errors.As(current, &wrapped) {
			joined = true
			for _, child := range wrapped.Unwrap() {
				visit(child)
			}
		}
	}
	visit(err)
	handles = append(handles, returnedHandle)
	result.HandleID = ""
	seen := map[string]bool{}
	for _, handle := range handles {
		if handle == "" || seen[handle] {
			continue
		}
		seen[handle] = true
		if result.HandleID == "" {
			result.HandleID = handle
		} else {
			result.AdditionalHandles = append(result.AdditionalHandles, handle)
		}
	}
	if joined || effect == nil {
		return // unknown even when each joined branch claims no effect
	}
	switch effect.EffectOutcome() {
	case ports.RuntimeEffectNone:
		if result.HandleID == "" {
			result.Outcome = "no_effect"
		}
	case ports.RuntimeEffectPossible, ports.RuntimeEffectApplied:
		result.Outcome = "possible"
	}
}

func (l *SwarmExactLauncher) replay(ctx context.Context, attemptID string) (domain.SwarmExecutionResult, error) {
	result, err := l.Store.GetSwarmExecutionResult(ctx, attemptID)
	if errors.Is(err, domain.ErrSwarmNotFound) {
		return result, domain.ErrSwarmNotReady
	}
	return result, err
}

func cloneSwarmRuntimeConfig(cfg ports.RuntimeConfig) ports.RuntimeConfig {
	cfg.Argv = slices.Clone(cfg.Argv)
	cfg.Env = maps.Clone(cfg.Env)
	return cfg
}
