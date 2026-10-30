package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// These tests exercise only the launch boundary with in-memory authority and
// runtime doubles. They provide no live-model or process-containment evidence.
type swarmExactStore struct {
	mu             sync.Mutex
	receipt        *domain.SwarmExecutionReceipt
	result         *domain.SwarmExecutionResult
	reads          int
	authorizations int
	saves          int
	saveContextErr error
	readErr        error
	authorizeErr   error
	saveErr        error
	afterAuthorize func()
}

func (s *swarmExactStore) GetSwarmExecution(_ context.Context, id string) (domain.SwarmExecutionReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.readErr != nil {
		return domain.SwarmExecutionReceipt{}, s.readErr
	}
	if s.receipt == nil || s.receipt.AttemptID != id {
		return domain.SwarmExecutionReceipt{}, domain.ErrSwarmNotFound
	}
	return *s.receipt, nil
}

func (s *swarmExactStore) AuthorizeSwarmExecution(_ context.Context, fence domain.SwarmFence, receipt domain.SwarmExecutionReceipt, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorizations++
	if s.authorizeErr != nil {
		return false, s.authorizeErr
	}
	if fence.AttemptID != receipt.AttemptID || fence.Owner != receipt.Owner || fence.Generation != receipt.Generation {
		return false, domain.ErrSwarmConflict
	}
	if s.receipt != nil {
		if domain.SwarmHash(*s.receipt) != domain.SwarmHash(receipt) {
			return false, domain.ErrSwarmConflict
		}
		return false, nil
	}
	if !receipt.ExpiresAt.After(now) {
		return false, domain.ErrSwarmStale
	}
	s.receipt = &receipt
	if s.afterAuthorize != nil {
		s.afterAuthorize()
	}
	return true, nil
}

func (s *swarmExactStore) RecordSwarmExecutionResult(ctx context.Context, result domain.SwarmExecutionResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	s.saveContextErr = ctx.Err()
	if s.saveErr != nil {
		return s.saveErr
	}
	if s.result != nil && !reflect.DeepEqual(*s.result, result) {
		return domain.ErrSwarmConflict
	}
	s.result = &result
	return nil
}

func (s *swarmExactStore) GetSwarmExecutionResult(_ context.Context, id string) (domain.SwarmExecutionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result == nil || s.result.AttemptID != id {
		return domain.SwarmExecutionResult{}, domain.ErrSwarmNotFound
	}
	return *s.result, nil
}

type swarmExactRuntime struct {
	ports.Runtime
	creates atomic.Int32
	create  func(context.Context, ports.RuntimeConfig) (ports.RuntimeHandle, error)
}

func (r *swarmExactRuntime) Create(ctx context.Context, cfg ports.RuntimeConfig) (ports.RuntimeHandle, error) {
	r.creates.Add(1)
	if r.create != nil {
		return r.create(ctx, cfg)
	}
	return ports.RuntimeHandle{ID: string(cfg.SessionID)}, nil
}

// Embed the cleanup trap separately so the launch runtime overrides only
// Create; every other unsupported runtime action still fails the test.
type swarmExactCleanupTrap struct {
	ports.Runtime
	destroys atomic.Int32
}

func (r *swarmExactCleanupTrap) Destroy(context.Context, ports.RuntimeHandle) error {
	r.destroys.Add(1)
	return errors.New("unexpected runtime cleanup")
}

func swarmExactFixture(t *testing.T) (domain.SwarmExecutionReceipt, ports.RuntimeConfig, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	operation := "operation-exact"
	cfg := ports.RuntimeConfig{
		SessionID: "swarm-exact", WorkspacePath: t.TempDir(),
		Argv:                    []string{"/test-only/agent", "--prompt", "exact packet"},
		Env:                     map[string]string{EnvRuntimeLaunchID: domain.SwarmHash(operation), "BOUND_VALUE": "original"},
		ExitOnCommandCompletion: true,
	}
	receipt := domain.SwarmExecutionReceipt{
		Version: 1, AttemptID: "attempt-exact", OperationID: operation,
		Owner: "coordinator-exact", SessionID: string(cfg.SessionID), Generation: 1,
		CapabilityFingerprint: domain.SwarmHash("capability"), QualificationBinding: domain.SwarmHash("qualification"),
		PacketHash: domain.SwarmHash("packet"), WorkspaceIdentityHash: domain.SwarmHash("workspace"),
		SourceManifestHash: domain.SwarmHash("source"), WorkspaceRoot: cfg.WorkspacePath,
		ConfigHash: domain.SwarmHash(cfg), PermissionReceiptID: "permission-exact", EvidenceMode: "test",
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := domain.ValidateSwarmExecutionReceipt(receipt); err != nil {
		t.Fatalf("invalid test receipt: %v", err)
	}
	return receipt, cfg, now
}

func swarmExactLauncher(t *testing.T, store *swarmExactStore, runtime *swarmExactRuntime, now time.Time) *SwarmExactLauncher {
	t.Helper()
	trap := &swarmExactCleanupTrap{}
	runtime.Runtime = trap
	t.Cleanup(func() {
		if got := trap.destroys.Load(); got != 0 {
			t.Errorf("launcher attempted %d runtime cleanup calls", got)
		}
	})
	return &SwarmExactLauncher{
		Store: store, Runtime: runtime, Now: func() time.Time { return now },
		Validate: func(context.Context, domain.SwarmExecutionReceipt, ports.RuntimeConfig) error { return nil },
	}
}

func TestSwarmExactPreservesFinalConfigAndWorkspace(t *testing.T) {
	receipt, cfg, now := swarmExactFixture(t)
	want := cfg
	want.Argv = append([]string(nil), cfg.Argv...)
	want.Env = make(map[string]string, len(cfg.Env))
	for key, value := range cfg.Env {
		want.Env[key] = value
	}
	sentinel := filepath.Join(cfg.WorkspacePath, "candidate-evidence")
	if err := os.WriteFile(sentinel, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
	launcher := swarmExactLauncher(t, store, runtime, now)
	launcher.Validate = func(_ context.Context, gotReceipt domain.SwarmExecutionReceipt, got ports.RuntimeConfig) error {
		if gotReceipt != receipt || !reflect.DeepEqual(got, want) {
			t.Error("validation did not receive the exact qualified inputs")
		}
		gotReceipt.WorkspaceRoot = "/changed"
		got.WorkspacePath = "/changed"
		got.Argv[0] = "/changed"
		got.Env["BOUND_VALUE"] = "changed by validation"
		return nil
	}
	runtime.create = func(_ context.Context, got ports.RuntimeConfig) (ports.RuntimeHandle, error) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("runtime inputs changed: got %#v, want %#v", got, want)
		}
		got.Argv[0] = "/runtime-mutation"
		got.Env["BOUND_VALUE"] = "changed by runtime"
		return ports.RuntimeHandle{ID: "owned-runtime"}, nil
	}
	result, err := launcher.Start(context.Background(), receipt, cfg)
	if err != nil || result.Outcome != "created" || result.HandleID != "owned-runtime" {
		t.Fatalf("Start = %#v, %v", result, err)
	}
	if !reflect.DeepEqual(cfg, want) || *store.receipt != receipt {
		t.Fatal("caller config or durable receipt was mutated")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "preserve me" {
		t.Fatalf("workspace evidence changed: %q, %v", data, err)
	}
}

func TestSwarmExactReplayCreatesOnce(t *testing.T) {
	receipt, cfg, now := swarmExactFixture(t)
	store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
	launcher := swarmExactLauncher(t, store, runtime, now)
	first, err := launcher.Start(context.Background(), receipt, cfg)
	if err != nil {
		t.Fatal(err)
	}
	launcher.Now = func() time.Time { return receipt.ExpiresAt.Add(time.Hour) }
	launcher.Validate = func(context.Context, domain.SwarmExecutionReceipt, ports.RuntimeConfig) error {
		t.Error("replay revalidated permission to launch")
		return domain.ErrSwarmStale
	}
	for range 3 {
		got, err := launcher.Start(context.Background(), receipt, cfg)
		if err != nil || !reflect.DeepEqual(got, first) {
			t.Fatalf("replay = %#v, %v; want %#v", got, err, first)
		}
	}
	if runtime.creates.Load() != 1 || store.authorizations != 1 || store.saves != 1 {
		t.Fatalf("duplicate effects: creates=%d authorizations=%d saves=%d", runtime.creates.Load(), store.authorizations, store.saves)
	}
}

func TestSwarmExactConcurrentLaunchCreatesOnce(t *testing.T) {
	receipt, cfg, now := swarmExactFixture(t)
	store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
	launcher := swarmExactLauncher(t, store, runtime, now)
	const callers = 12
	var ready, finished sync.WaitGroup
	ready.Add(callers)
	finished.Add(callers)
	gate := make(chan struct{})
	launcher.Validate = func(context.Context, domain.SwarmExecutionReceipt, ports.RuntimeConfig) error {
		ready.Done()
		<-gate
		return nil
	}
	type answer struct {
		result domain.SwarmExecutionResult
		err    error
	}
	answers := make(chan answer, callers)
	for range callers {
		go func() {
			defer finished.Done()
			result, err := launcher.Start(context.Background(), receipt, cfg)
			answers <- answer{result, err}
		}()
	}
	ready.Wait()
	close(gate)
	finished.Wait()
	close(answers)
	successes := 0
	for got := range answers {
		if got.err == nil {
			successes++
			if got.result.Outcome != "created" || got.result.HandleID != string(cfg.SessionID) {
				t.Errorf("unexpected successful result: %#v", got.result)
			}
		} else if !errors.Is(got.err, domain.ErrSwarmNotReady) {
			t.Errorf("concurrent result: %v", got.err)
		}
	}
	if successes == 0 || runtime.creates.Load() != 1 || store.saves != 1 {
		t.Fatalf("successes=%d creates=%d saves=%d", successes, runtime.creates.Load(), store.saves)
	}
}

type swarmExactEffectError struct {
	cause   error
	handle  ports.RuntimeHandle
	effect  ports.RuntimeEffectOutcome
	cleanup ports.RuntimeCleanupOutcome
}

func (e swarmExactEffectError) Error() string                             { return e.cause.Error() }
func (e swarmExactEffectError) Unwrap() error                             { return e.cause }
func (e swarmExactEffectError) PossibleHandle() ports.RuntimeHandle       { return e.handle }
func (e swarmExactEffectError) EffectOutcome() ports.RuntimeEffectOutcome { return e.effect }
func (e swarmExactEffectError) CleanupOutcome() ports.RuntimeCleanupOutcome {
	return e.cleanup
}

func TestSwarmExactPreservesRuntimeEffectEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		effect  ports.RuntimeEffectOutcome
		cleanup ports.RuntimeCleanupOutcome
		handle  string
		outcome string
	}{
		{"ambiguous", ports.RuntimeEffectPossible, ports.RuntimeCleanupFailed, "possible-owner", "possible"},
		{"applied-cleaned", ports.RuntimeEffectApplied, ports.RuntimeCleanupSucceeded, "prior-owner", "possible"},
		{"no-effect", ports.RuntimeEffectNone, ports.RuntimeCleanupNotAttempted, "", "no_effect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt, cfg, now := swarmExactFixture(t)
			failure := errors.New("test runtime failure")
			store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
			runtime.create = func(context.Context, ports.RuntimeConfig) (ports.RuntimeHandle, error) {
				return ports.RuntimeHandle{}, swarmExactEffectError{failure, ports.RuntimeHandle{ID: tc.handle}, tc.effect, tc.cleanup}
			}
			launcher := swarmExactLauncher(t, store, runtime, now)
			result, err := launcher.Start(context.Background(), receipt, cfg)
			if !errors.Is(err, failure) || result.Outcome != tc.outcome || result.HandleID != tc.handle ||
				result.RuntimeEffect != string(tc.effect) || result.RuntimeCleanup != string(tc.cleanup) {
				t.Fatalf("lost runtime evidence: %#v, %v", result, err)
			}
			if store.result == nil || !reflect.DeepEqual(*store.result, result) {
				t.Fatal("runtime evidence was not persisted")
			}
			if _, err := launcher.Start(context.Background(), receipt, cfg); err != nil {
				t.Fatalf("evidence replay failed: %v", err)
			}
			if runtime.creates.Load() != 1 {
				t.Fatal("failed launch was repeated")
			}
		})
	}
}

func TestSwarmExactRetainsNestedJoinedRuntimeHandles(t *testing.T) {
	failure := errors.New("runtime failure")
	noEffect := swarmExactEffectError{failure, ports.RuntimeHandle{}, ports.RuntimeEffectNone, ports.RuntimeCleanupNotAttempted}
	possible := func(id string, cause error) error {
		return swarmExactEffectError{cause, ports.RuntimeHandle{ID: id}, ports.RuntimeEffectPossible, ports.RuntimeCleanupFailed}
	}
	nested := fmt.Errorf("outer context: %w", possible("outer", possible("middle", errors.Join(
		noEffect, fmt.Errorf("first fallback: %w", possible("first", failure)), possible("second", failure),
	))))
	result := domain.SwarmExecutionResult{Outcome: "unknown", HandleID: "returned"}
	recordSwarmRuntimeError(&result, nested)
	if result.Outcome != "unknown" || result.HandleID != "outer" || !reflect.DeepEqual(result.AdditionalHandles, []string{"middle", "first", "second", "returned"}) {
		t.Fatalf("nested effect ownership lost: %+v", result)
	}
	joinedNone := domain.SwarmExecutionResult{Outcome: "unknown"}
	recordSwarmRuntimeError(&joinedNone, fmt.Errorf("joined context: %w", errors.Join(noEffect, noEffect)))
	if joinedNone.Outcome != "unknown" || joinedNone.HandleID != "" || len(joinedNone.AdditionalHandles) != 0 {
		t.Fatalf("joined no-effect claims established absence: %+v", joinedNone)
	}
}

func TestSwarmExactCancelledCreatePersistsWithDetachedContext(t *testing.T) {
	receipt, cfg, now := swarmExactFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
	runtime.create = func(context.Context, ports.RuntimeConfig) (ports.RuntimeHandle, error) {
		cancel()
		return ports.RuntimeHandle{ID: "possibly-running"}, context.Canceled
	}
	launcher := swarmExactLauncher(t, store, runtime, now)
	result, err := launcher.Start(ctx, receipt, cfg)
	if !errors.Is(err, context.Canceled) || result.Outcome != "unknown" || result.HandleID != "possibly-running" {
		t.Fatalf("cancelled launch lost ownership: %#v, %v", result, err)
	}
	if store.saves != 1 || store.saveContextErr != nil || store.result == nil || !reflect.DeepEqual(*store.result, result) {
		t.Fatalf("cancelled result not durably retained: saves=%d context=%v result=%#v", store.saves, store.saveContextErr, store.result)
	}
}

func TestSwarmExactPendingAuthorizationNeverLaunches(t *testing.T) {
	receipt, cfg, now := swarmExactFixture(t)
	store, runtime := &swarmExactStore{receipt: &receipt}, &swarmExactRuntime{}
	launcher := swarmExactLauncher(t, store, runtime, now)
	launcher.Validate = func(context.Context, domain.SwarmExecutionReceipt, ports.RuntimeConfig) error {
		t.Error("pending replay attempted launch validation")
		return nil
	}
	for range 2 {
		if _, err := launcher.Start(context.Background(), receipt, cfg); !errors.Is(err, domain.ErrSwarmNotReady) {
			t.Fatalf("pending replay = %v", err)
		}
	}
	if runtime.creates.Load() != 0 || store.authorizations != 0 || store.saves != 0 {
		t.Fatal("pending authorization caused a new effect")
	}
}

func TestSwarmExactStoreErrorsPropagate(t *testing.T) {
	for _, phase := range []string{"read", "authorize", "save"} {
		t.Run(phase, func(t *testing.T) {
			receipt, cfg, now := swarmExactFixture(t)
			failure := errors.New("test storage failure")
			store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
			wantCreates := int32(0)
			switch phase {
			case "read":
				store.readErr = failure
			case "authorize":
				store.authorizeErr = failure
			case "save":
				store.saveErr = failure
				wantCreates = 1
			}
			launcher := swarmExactLauncher(t, store, runtime, now)
			if _, err := launcher.Start(context.Background(), receipt, cfg); !errors.Is(err, failure) {
				t.Fatalf("storage error hidden: %v", err)
			}
			if phase == "save" {
				if _, err := launcher.Start(context.Background(), receipt, cfg); !errors.Is(err, domain.ErrSwarmNotReady) {
					t.Fatalf("lost result was not kept inconclusive: %v", err)
				}
			}
			if runtime.creates.Load() != wantCreates {
				t.Fatalf("creates=%d, want %d", runtime.creates.Load(), wantCreates)
			}
		})
	}
}

func TestSwarmExactLiveRejectedBeforeAuthorityOrRuntime(t *testing.T) {
	receipt, cfg, now := swarmExactFixture(t)
	receipt.EvidenceMode = "live"
	store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
	launcher := swarmExactLauncher(t, store, runtime, now)
	launcher.Validate = func(context.Context, domain.SwarmExecutionReceipt, ports.RuntimeConfig) error {
		t.Error("live request reached validation")
		return nil
	}
	if _, err := launcher.Start(context.Background(), receipt, cfg); !errors.Is(err, ErrSwarmExactLiveUnsupported) {
		t.Fatalf("live request = %v", err)
	}
	if store.reads != 0 || store.authorizations != 0 || store.saves != 0 || runtime.creates.Load() != 0 {
		t.Fatal("live request reached durable authority or runtime")
	}
}

func TestSwarmExactExpiresAfterAuthorizationWithoutCreate(t *testing.T) {
	receipt, cfg, now := swarmExactFixture(t)
	store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
	launcher := swarmExactLauncher(t, store, runtime, now)
	clock := now
	launcher.Now = func() time.Time { return clock }
	store.afterAuthorize = func() { clock = receipt.ExpiresAt }
	result, err := launcher.Start(context.Background(), receipt, cfg)
	if !errors.Is(err, domain.ErrSwarmStale) || result.Outcome != "no_effect" || result.HandleID != "" {
		t.Fatalf("expired authorization = %#v, %v", result, err)
	}
	if runtime.creates.Load() != 0 || store.saves != 1 || store.result == nil || !reflect.DeepEqual(*store.result, result) {
		t.Fatal("expired launch created a runtime or lost its no-effect result")
	}
}

func TestSwarmExactRejectsChangedFinalInputs(t *testing.T) {
	for _, name := range []string{"config-hash", "workspace", "session", "session-alphabet", "relative-executable", "generation", "start-on-attach", "recovery-shell"} {
		t.Run(name, func(t *testing.T) {
			receipt, cfg, now := swarmExactFixture(t)
			switch name {
			case "config-hash":
				cfg.Env["BOUND_VALUE"] = "changed"
			case "workspace":
				cfg.WorkspacePath = t.TempDir()
			case "session":
				cfg.SessionID = "another-session"
			case "session-alphabet":
				cfg.SessionID = "bad.session"
				receipt.SessionID = string(cfg.SessionID)
			case "relative-executable":
				cfg.Argv[0] = "agent"
			case "generation":
				cfg.Env[EnvRuntimeLaunchID] = "wrong-generation"
			case "start-on-attach":
				cfg.StartOnAttach = true
			case "recovery-shell":
				cfg.ExitOnCommandCompletion = false
			}
			if name != "config-hash" {
				receipt.ConfigHash = domain.SwarmHash(cfg)
			}
			store, runtime := &swarmExactStore{}, &swarmExactRuntime{}
			launcher := swarmExactLauncher(t, store, runtime, now)
			launcher.Validate = func(context.Context, domain.SwarmExecutionReceipt, ports.RuntimeConfig) error {
				t.Error("invalid final inputs reached validation")
				return nil
			}
			if _, err := launcher.Start(context.Background(), receipt, cfg); err == nil {
				t.Fatal("invalid final inputs were accepted")
			}
			if store.reads != 0 || store.authorizations != 0 || runtime.creates.Load() != 0 {
				t.Fatal("invalid final inputs reached authority or runtime")
			}
		})
	}
}
