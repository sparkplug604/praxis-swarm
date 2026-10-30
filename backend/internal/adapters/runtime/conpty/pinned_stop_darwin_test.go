//go:build darwin

package conpty

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/conpty/ptyregistry"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Called within the single constructor test, preserving the one-binding rule.
func testPinnedFencedStop(t *testing.T, runtime *PinnedRuntime, root string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ready := filepath.Join(root, "fenced-stop-ready")
	cfg := ports.RuntimeConfig{SessionID: "pinned-fenced-stop", WorkspacePath: root,
		Argv: []string{"/bin/sh", "-c", `trap '' HUP TERM; printf ready > "$1"; exec /bin/sleep 30`, "fixture", ready},
		Env:  map[string]string{runtimeLaunchIDEnv: "fenced-original"}, ExitOnCommandCompletion: true}
	handle, err := runtime.Create(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime.ownedMu.Lock()
	owned := runtime.owned[handle.ID]
	runtime.ownedMu.Unlock()
	// Explicit fixture-only teardown uses the original owned host process. The
	// product Stop method neither terminates that receipt host nor drops registry.
	t.Cleanup(func() {
		_ = owned.process.Signal(syscall.SIGTERM)
		deadline := time.Now().Add(3 * time.Second)
		for pinnedProcessAlive(owned.process) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if pinnedProcessAlive(owned.process) {
			_ = owned.process.Kill()
		}
		deadline = time.Now().Add(time.Second)
		for pinnedProcessAlive(owned.process) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if pinnedProcessAlive(owned.process) {
			t.Error("owned fixture host did not exit")
		}
		_ = ptyregistry.Unregister(context.Background(), handle.ID)
		runtime.direct.mu.Lock()
		delete(runtime.direct.sessions, handle.ID)
		runtime.direct.mu.Unlock()
		runtime.ownedMu.Lock()
		owned.destroyed = true
		runtime.owned[handle.ID] = owned
		runtime.ownedMu.Unlock()
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.ReadFile(ready); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if data, err := os.ReadFile(ready); err != nil || string(data) != "ready" {
		t.Fatalf("fixture not ready: %q %v", data, err)
	}
	ref := ports.FencedRuntimeRef{Handle: handle, SessionID: cfg.SessionID, Generation: cfg.Env[runtimeLaunchIDEnv]}
	entries, complete, err := ptyregistry.Scan(ctx)
	if err != nil || !complete {
		t.Fatal(err)
	}
	var entry ptyregistry.Entry
	for _, e := range entries {
		if e.SessionID == handle.ID {
			entry = e
		}
	}
	if entry.PipePath == "" {
		t.Fatal("missing owned registry entry")
	}
	for _, bad := range []ports.FencedRuntimeRef{
		{Handle: handle, SessionID: cfg.SessionID, Generation: "wrong"},
		{Handle: handle, SessionID: "wrong", Generation: ref.Generation},
		{Handle: handle, SessionID: cfg.SessionID, Generation: ref.Generation, NativeIdentity: "unsupported"},
	} {
		result, err := runtime.StopFencedRuntime(ctx, bad)
		if !errors.Is(err, ports.ErrRuntimeProbeInconclusive) || result.Acknowledged || result.Effect != ports.RuntimeEffectNone {
			t.Fatalf("bad ref got %+v %v", result, err)
		}
	}
	// Forge the address hint's generation: only host-side comparison can stop
	// this stale request from targeting the actual process at this address.
	forged := entry
	forged.LaunchID = "wrong"
	if err := ptyregistry.Register(ctx, forged); err != nil {
		t.Fatal(err)
	}
	wrong := ref
	wrong.Generation = forged.LaunchID
	result, err := runtime.StopFencedRuntime(ctx, wrong)
	if !errors.Is(err, ports.ErrRuntimeProbeInconclusive) || result.Acknowledged || result.Effect != ports.RuntimeEffectNone {
		t.Fatalf("forged registry got %+v %v", result, err)
	}
	if err := ptyregistry.Register(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := clientKill(entry.PipePath); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	status, alive, err := clientStatusContext(ctx, entry.PipePath)
	if err != nil || !alive || !status.Alive {
		t.Fatalf("mismatch/legacy kill affected worker: %+v %v %v", status, alive, err)
	}
	// Recovery deliberately drops in-memory process ownership. Stop must rely
	// on the exact host receipt, never a recovered PID or an ordinary Destroy.
	runtime.ownedMu.Lock()
	delete(runtime.owned, handle.ID)
	runtime.ownedMu.Unlock()
	runtime.direct.mu.Lock()
	delete(runtime.direct.sessions, handle.ID)
	runtime.direct.mu.Unlock()
	var wg sync.WaitGroup
	results := make([]ports.FencedStopResult, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = runtime.StopFencedRuntime(ctx, ref) }(i)
	}
	wg.Wait()
	for i, result := range results {
		if errs[i] != nil || !result.Acknowledged || result.Generation != ref.Generation || result.SessionID != ref.SessionID || result.Effect != ports.RuntimeEffectApplied || result.Cleanup != ports.RuntimeCleanupSucceeded {
			t.Fatalf("stop %d: %+v %v", i, result, errs[i])
		}
	}
	if !pinnedProcessAlive(owned.process) {
		t.Fatal("stop discarded receipt host")
	}
	if got := runtime.ProbeFencedRuntime(ctx, ref); got.Liveness != ports.FencedUnknown {
		t.Fatalf("stop claimed terminal absence: %+v", got)
	}
	entries, complete, err = ptyregistry.Scan(ctx)
	if err != nil || !complete {
		t.Fatal(err)
	}
	retained := false
	for _, e := range entries {
		if e == entry {
			retained = true
		}
	}
	if !retained {
		t.Fatal("stop changed or removed durable ownership")
	}
	if data, err := os.ReadFile(ready); err != nil || string(data) != "ready" {
		t.Fatal("stop removed evidence workspace")
	}
	if err := syscall.Kill(status.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("original native child remains: %v", err)
	}
	t.Logf("native_child=%d wrong_generation_no_effect=true legacy_kill_refused=true recovered_stop_acknowledged=true replay_count=4 registry_retained=true terminal_absence=unknown", status.PID)
}
