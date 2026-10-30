//go:build darwin

package conpty

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/conpty/ptyregistry"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// StopFencedRuntime never signals a numeric PID or invokes ordinary Destroy.
// The registry is only an address hint: the contacted host must independently
// match the immutable session/launch generation before it closes its PTY. This
// permits recovered ownership without trusting a reused PID/listening address.
// The listener and registry remain as replayable evidence, including on failure.
// Acknowledged PTY cleanup never establishes descendant absence.
func (r *PinnedRuntime) StopFencedRuntime(ctx context.Context, ref ports.FencedRuntimeRef) (ports.FencedStopResult, error) {
	out := ports.FencedStopResult{SessionID: ref.SessionID, Generation: ref.Generation,
		Effect: ports.RuntimeEffectNone, Cleanup: ports.RuntimeCleanupNotAttempted}
	fail := func(err error) (ports.FencedStopResult, error) {
		return out, errors.Join(ports.ErrRuntimeProbeInconclusive, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if ref.NativeIdentity != "" || ref.Handle.ID != string(ref.SessionID) || !validSessionID.MatchString(ref.Handle.ID) ||
		!validFencedStopRequest(FencedStopRequest{Version: FencedStopProtocolVersion, SessionID: string(ref.SessionID), Generation: ref.Generation}) {
		return fail(errors.New("pinned conpty: complete supported stop identity required"))
	}
	if err := r.validateRegistry(); err != nil {
		return fail(err)
	}
	entries, complete, err := ptyregistry.Scan(ctx)
	if err != nil || !complete {
		return fail(errors.Join(err, errors.New("pinned conpty: incomplete stop registry")))
	}
	var found *ptyregistry.Entry
	for i := range entries {
		if entries[i].SessionID == ref.Handle.ID {
			if found != nil {
				return fail(errors.New("pinned conpty: ambiguous stop registry"))
			}
			found = &entries[i]
		}
	}
	if found == nil || !found.RetainUntilUnregistered || found.LaunchID != ref.Generation {
		return fail(errors.New("pinned conpty: stop identity missing or changed"))
	}
	// Reject malformed/nonloopback registry routes without attempting a dial.
	host, port, err := net.SplitHostPort(found.PipePath)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || host != "127.0.0.1" || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return fail(errors.New("pinned conpty: unsupported stop address"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	// Once transport begins a lost reply can hide a successful stop. There is
	// no kill fallback, automatic retry, registry removal, or absence inference.
	out.Effect = ports.RuntimeEffectPossible
	reply, err := clientFencedStop(ctx, found.PipePath, FencedStopRequest{
		Version: FencedStopProtocolVersion, SessionID: string(ref.SessionID), Generation: ref.Generation,
	})
	if err != nil {
		return fail(err)
	}
	if reply.Version != FencedStopProtocolVersion || reply.SessionID != string(ref.SessionID) || reply.Generation != ref.Generation {
		return fail(errors.New("pinned conpty: stop acknowledgement identity mismatch"))
	}
	if !reply.Accepted {
		if reply.Cleanup == string(ports.RuntimeCleanupNotAttempted) {
			out.Effect = ports.RuntimeEffectNone
		}
		return fail(errors.New("pinned conpty: host refused fenced stop"))
	}
	switch ports.RuntimeCleanupOutcome(reply.Cleanup) {
	case ports.RuntimeCleanupSucceeded:
		if reply.Error != "" {
			return fail(errors.New("pinned conpty: contradictory stop acknowledgement"))
		}
	case ports.RuntimeCleanupFailed:
		if reply.Error == "" {
			return fail(errors.New("pinned conpty: missing stop failure evidence"))
		}
	default:
		return fail(errors.New("pinned conpty: invalid stop cleanup outcome"))
	}
	out.Acknowledged, out.Effect, out.Cleanup = true, ports.RuntimeEffectApplied, ports.RuntimeCleanupOutcome(reply.Cleanup)
	if out.Cleanup == ports.RuntimeCleanupFailed {
		return fail(fmt.Errorf("pinned conpty: host PTY cleanup failed: %s", reply.Error))
	}
	return out, nil
}
