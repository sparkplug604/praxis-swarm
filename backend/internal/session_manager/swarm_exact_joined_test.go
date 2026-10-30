package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestSwarmExactJoinedRuntimeErrorsRetainUncertaintyAndHandles(t *testing.T) {
	effect := func(outcome ports.RuntimeEffectOutcome, handle string) error {
		return swarmExactEffectError{cause: errors.New("runtime fixture failure"), handle: ports.RuntimeHandle{ID: handle},
			effect: outcome, cleanup: ports.RuntimeCleanupNotAttempted}
	}
	for _, tc := range []struct {
		name, returned, primary, outcome string
		err                              error
		additional                       []string
	}{
		{name: "none followed by possible", err: errors.Join(effect(ports.RuntimeEffectNone, ""), effect(ports.RuntimeEffectPossible, "fallback")), primary: "fallback", outcome: "unknown"},
		{name: "none followed by unclassified", err: errors.Join(effect(ports.RuntimeEffectNone, ""), errors.New("unclassified fallback")), outcome: "unknown"},
		{name: "all branches claim none", err: errors.Join(effect(ports.RuntimeEffectNone, ""), effect(ports.RuntimeEffectNone, "")), outcome: "unknown"},
		{name: "single branch multi unwrap", err: errors.Join(effect(ports.RuntimeEffectNone, "")), outcome: "unknown"},
		{name: "nested join", err: fmt.Errorf("outer: %w", errors.Join(effect(ports.RuntimeEffectNone, ""), fmt.Errorf("inner: %w", errors.Join(effect(ports.RuntimeEffectApplied, "nested"))))), primary: "nested", outcome: "unknown"},
		{name: "all handles", err: errors.Join(effect(ports.RuntimeEffectPossible, "direct"), effect(ports.RuntimeEffectApplied, "fallback")), returned: "returned", primary: "direct", additional: []string{"fallback", "returned"}, outcome: "unknown"},
		{name: "deduplicate handles", err: errors.Join(effect(ports.RuntimeEffectPossible, "same"), effect(ports.RuntimeEffectApplied, "same")), returned: "same", primary: "same", outcome: "unknown"},
		{name: "returned handle without typed evidence", err: errors.Join(errors.New("one"), errors.New("two")), returned: "returned", primary: "returned", outcome: "unknown"},
		{name: "single effect retains returned handle", err: fmt.Errorf("wrapper: %w", effect(ports.RuntimeEffectPossible, "possible")), returned: "returned", primary: "possible", additional: []string{"returned"}, outcome: "possible"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt, cfg, now := swarmExactFixture(t)
			store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
			runtime.create = func(context.Context, ports.RuntimeConfig) (ports.RuntimeHandle, error) {
				return ports.RuntimeHandle{ID: tc.returned}, tc.err
			}
			launcher := swarmExactLauncher(t, store, runtime, now)
			result, err := launcher.Start(context.Background(), receipt, cfg)
			if !errors.Is(err, tc.err) || result.Outcome != tc.outcome || result.HandleID != tc.primary || !reflect.DeepEqual(result.AdditionalHandles, tc.additional) {
				t.Fatalf("result=%+v error=%v; want outcome=%s handle=%q additional=%v", result, err, tc.outcome, tc.primary, tc.additional)
			}
			if err := domain.ValidateSwarmExecutionResult(result); err != nil {
				t.Fatalf("result cannot be persisted: %v", err)
			}
			if store.result == nil || !reflect.DeepEqual(*store.result, result) {
				t.Fatal("runtime ownership evidence was not recorded")
			}
			replayed, err := launcher.Start(context.Background(), receipt, cfg)
			if err != nil || !reflect.DeepEqual(replayed, result) || runtime.creates.Load() != 1 {
				t.Fatalf("replay changed evidence or relaunched: %+v %v creates=%d", replayed, err, runtime.creates.Load())
			}
		})
	}
}
