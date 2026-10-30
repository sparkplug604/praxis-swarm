package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// FencedStopResult records a stop request, not terminal absence. Acknowledged
// means the exact host checked this session/generation at the effect boundary.
// Cleanup describes its PTY close only; even succeeded cannot prove descendant
// termination or release scheduling capacity. Lost replies retain uncertainty.
type FencedStopResult struct {
	SessionID    domain.SessionID
	Generation   string
	Acknowledged bool
	Effect       RuntimeEffectOutcome
	Cleanup      RuntimeCleanupOutcome
}

// FencedRuntimeStopper stops only the requested immutable launch identity. A
// separate probe followed by ordinary Destroy does not implement this contract.
// Unsupported/mismatched/unknown identities must never fall back to an unfenced
// kill, infer absence, delete source/evidence, or authorize a replacement launch.
type FencedRuntimeStopper interface {
	StopFencedRuntime(context.Context, FencedRuntimeRef) (FencedStopResult, error)
}
