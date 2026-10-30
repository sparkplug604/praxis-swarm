//go:build darwin

package conpty

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/conpty/ptyregistry"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type pinnedChildObservation struct {
	Argv []string
	Env  []string
	CWD  string
}

// TestPinnedChildProbe is re-executed inside the native PTY. Its file contains
// only the explicit fixture environment, never the test runner's environment.
func TestPinnedChildProbe(t *testing.T) {
	path := os.Getenv("AO_PINNED_CHILD_PROBE")
	if path == "" {
		t.Skip("native child fixture only")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	value := pinnedChildObservation{Argv: os.Args, Env: os.Environ(), CWD: cwd}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// One successful binding per test process, matching the constructor contract.
func TestPinnedRuntimeContracts(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registryDir := filepath.Join(root, "registry")
	if err := os.Mkdir(registryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(root, "pinned-host")
	dest, err := os.OpenFile(host, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(dest, hash), source)
	closeErr := errors.Join(source.Close(), dest.Close())
	if err := errors.Join(copyErr, closeErr); err != nil {
		t.Fatal(err)
	}
	opts := PinnedOptions{RunFilePath: filepath.Join(registryDir, "running.json"), HostExecutable: host, HostSHA256: hex.EncodeToString(hash.Sum(nil))}
	t.Cleanup(func() { ptyregistry.SetRunFilePath("") })

	t.Run("invalid constructor inputs do not bind", func(t *testing.T) {
		cases := []struct {
			name string
			edit func(*PinnedOptions)
		}{
			{"missing registry", func(o *PinnedOptions) { o.RunFilePath = "" }},
			{"relative host", func(o *PinnedOptions) { o.HostExecutable = "host" }},
			{"absent parent", func(o *PinnedOptions) { o.RunFilePath = filepath.Join(root, "missing", "running.json") }},
			{"bad hash", func(o *PinnedOptions) { o.HostSHA256 = "invalid" }},
			{"wrong hash", func(o *PinnedOptions) { o.HostSHA256 = hex.EncodeToString(make([]byte, sha256.Size)) }},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				candidate := opts
				test.edit(&candidate)
				if _, err := NewPinnedRuntime(ctx, candidate); err == nil {
					t.Fatal("invalid constructor succeeded")
				}
			})
		}
		link := filepath.Join(root, "host-link")
		if err := os.Symlink(host, link); err != nil {
			t.Fatal(err)
		}
		candidate := opts
		candidate.HostExecutable = link
		if _, err := NewPinnedRuntime(ctx, candidate); err == nil {
			t.Fatal("symlink host accepted")
		}
		t.Run("global AO aliases", func(t *testing.T) {
			home := filepath.Join(root, "fake-home")
			global := filepath.Join(home, ".ao")
			child := filepath.Join(global, "child")
			if err := os.MkdirAll(child, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			aliases := []string{global, child}
			caseAlias := filepath.Join(home, ".AO", "child")
			if original, err := os.Stat(child); err == nil {
				if alias, err := os.Stat(caseAlias); err == nil && os.SameFile(original, alias) {
					aliases = append(aliases, caseAlias)
				}
				dataAlias := "/System/Volumes/Data" + child
				if alias, err := os.Stat(dataAlias); err == nil && os.SameFile(original, alias) {
					aliases = append(aliases, dataAlias)
				}
			}
			for _, alias := range aliases {
				candidate := opts
				candidate.RunFilePath = filepath.Join(alias, "running.json")
				if _, err := NewPinnedRuntime(ctx, candidate); err == nil || !strings.Contains(err.Error(), "outside the user's .ao") {
					t.Fatalf("global AO alias %q = %v, want ancestry refusal", alias, err)
				}
			}
			t.Logf("refused %d existing global AO aliases", len(aliases))
		})
	})

	runtime, err := NewPinnedRuntime(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	good := ports.RuntimeConfig{SessionID: "pinned-contract", WorkspacePath: root, Argv: []string{host}, Env: map[string]string{runtimeLaunchIDEnv: "pinned-generation"}, ExitOnCommandCompletion: true}
	assertNoEffect := func(t *testing.T, cfg ports.RuntimeConfig) {
		t.Helper()
		_, err := runtime.Create(ctx, cfg)
		var effect ports.RuntimeEffectError
		if !errors.As(err, &effect) || effect.EffectOutcome() != ports.RuntimeEffectNone || effect.PossibleHandle().ID != "" {
			t.Fatalf("Create = %v, want no-effect refusal", err)
		}
	}

	t.Run("binding immutable and unique", func(t *testing.T) {
		binding := runtime.Binding()
		if binding.Backend != "darwin-direct-pty" || binding.HostExecutable != host || binding.HostSHA256 != opts.HostSHA256 || binding.RegistryPath != filepath.Join(registryDir, "windows-pty-hosts.json") || binding.EnvironmentSemanticsVersion != "explicit-full-env-v1" {
			t.Fatalf("binding = %+v", binding)
		}
		binding.HostExecutable = "/changed"
		if runtime.Binding().HostExecutable != host || runtime.direct.destroyWait < 2*time.Second {
			t.Fatal("mutable binding or insufficient cleanup grace")
		}
		if _, err := NewPinnedRuntime(ctx, opts); err == nil {
			t.Fatal("second pinned runtime accepted")
		}
	})

	t.Run("exact config required", func(t *testing.T) {
		cases := []func(*ports.RuntimeConfig){
			func(c *ports.RuntimeConfig) { c.Env = nil },
			func(c *ports.RuntimeConfig) { c.Env = map[string]string{} },
			func(c *ports.RuntimeConfig) { c.Env = map[string]string{runtimeLaunchIDEnv: strings.Repeat("x", 257)} },
			func(c *ports.RuntimeConfig) { c.SessionID = domain.SessionID(strings.Repeat("x", 257)) },
			func(c *ports.RuntimeConfig) { c.Env = map[string]string{runtimeLaunchIDEnv: "x", "BAD=KEY": "value"} },
			func(c *ports.RuntimeConfig) {
				c.Env = map[string]string{runtimeLaunchIDEnv: "x", "DYLD_INSERT_LIBRARIES": "/tmp/override.dylib"}
			},
			func(c *ports.RuntimeConfig) {
				c.Env = map[string]string{runtimeLaunchIDEnv: "x", "LD_PRELOAD": "/tmp/override.so"}
			},
			func(c *ports.RuntimeConfig) { c.Argv = []string{"relative"} },
			func(c *ports.RuntimeConfig) { c.Argv = []string{"/usr/bin/env", "X=1", "/bin/true"} },
			func(c *ports.RuntimeConfig) { c.StartOnAttach = true },
			func(c *ports.RuntimeConfig) { c.ExitOnCommandCompletion = false },
			func(c *ports.RuntimeConfig) { c.WorkspacePath = "relative" },
		}
		for _, edit := range cases {
			cfg := good
			edit(&cfg)
			assertNoEffect(t, cfg)
		}
	})

	t.Run("refuse host content and identity drift", func(t *testing.T) {
		info, err := os.Stat(host)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(host, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte("drift")); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		assertNoEffect(t, good)
		if err := os.Truncate(host, info.Size()); err != nil {
			t.Fatal(err)
		}
		backup := host + ".original"
		if err := os.Rename(host, backup); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(host, []byte("replacement"), 0o700); err != nil {
			t.Fatal(err)
		}
		assertNoEffect(t, good)
		if err := os.Rename(backup, host); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("refuse registry drift", func(t *testing.T) {
		ptyregistry.SetRunFilePath(filepath.Join(root, "different.json"))
		assertNoEffect(t, good)
		if err := runtime.Destroy(ctx, ports.RuntimeHandle{ID: "absent"}); !errors.Is(err, ports.ErrRuntimeProbeInconclusive) {
			t.Fatalf("Destroy drift = %v", err)
		}
		ptyregistry.SetRunFilePath(opts.RunFilePath)
		path := runtime.Binding().RegistryPath
		if err := os.Symlink(filepath.Join(root, "external.json"), path); err != nil {
			t.Fatal(err)
		}
		assertNoEffect(t, good)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		backup := registryDir + ".original"
		if err := os.Rename(registryDir, backup); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(registryDir, 0o700); err != nil {
			t.Fatal(err)
		}
		assertNoEffect(t, good)
		if err := os.Remove(registryDir); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(backup, registryDir); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("absence is unknown until containment qualified", func(t *testing.T) {
		ref := ports.FencedRuntimeRef{Handle: ports.RuntimeHandle{ID: "absent"}, SessionID: "absent", Generation: "generation"}
		if got := runtime.direct.ProbeFencedRuntime(ctx, ref); got.Liveness != ports.FencedDead {
			t.Fatalf("fixture underlying probe = %+v, want dead", got)
		}
		if got := runtime.ProbeFencedRuntime(ctx, ref); got.Liveness != ports.FencedUnknown || got.Reason != ports.FencedReasonProbeFailed {
			t.Fatalf("pinned probe = %+v, want unknown/probe_failed", got)
		}
		if alive, err := runtime.IsAlive(ctx, ref.Handle); alive || !errors.Is(err, ports.ErrRuntimeProbeInconclusive) {
			t.Fatalf("IsAlive = (%v,%v), want false/inconclusive", alive, err)
		}
	})

	t.Run("recovered and reaped ownership cannot destroy", func(t *testing.T) {
		entry := ptyregistry.Entry{SessionID: "pinned-recovered", PtyHostPID: os.Getpid(), PipePath: "127.0.0.1:1", LaunchID: "old-generation", RetainUntilUnregistered: true}
		if err := ptyregistry.Register(ctx, entry); err != nil {
			t.Fatal(err)
		}
		killCalls := 0
		originalKill := runtime.direct.killHost
		runtime.direct.killHost = func(string) error { killCalls++; return nil }
		defer func() { runtime.direct.killHost = originalKill }()
		handle := ports.RuntimeHandle{ID: entry.SessionID}
		if err := runtime.Destroy(ctx, handle); !errors.Is(err, ports.ErrRuntimeProbeInconclusive) {
			t.Fatalf("unowned registry handle Destroy = %v", err)
		}
		if _, err := runtime.direct.resolveWithEvidence(ctx, handle.ID); err != nil {
			t.Fatal(err)
		}
		if err := runtime.Destroy(ctx, handle); !errors.Is(err, ports.ErrRuntimeProbeInconclusive) {
			t.Fatalf("recovered memory handle Destroy = %v", err)
		}
		// An original process object that has been reaped must never be
		// replaced with os.FindProcess(PID), even for currentOwner sessions.
		cmd := exec.Command("/usr/bin/true")
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		runtime.owned[handle.ID] = pinnedOwnedHost{process: cmd.Process, addr: entry.PipePath, launchID: entry.LaunchID}
		runtime.direct.sessions[handle.ID] = &hostSession{pid: cmd.Process.Pid, addr: entry.PipePath, launchID: entry.LaunchID, currentOwner: true}
		if err := runtime.Destroy(ctx, handle); !errors.Is(err, ports.ErrRuntimeProbeInconclusive) {
			t.Fatalf("reaped original handle Destroy = %v", err)
		}
		if killCalls != 0 {
			t.Fatalf("unsafe kill calls = %d", killCalls)
		}
		entries, complete, err := ptyregistry.Scan(ctx)
		if err != nil || !complete || len(entries) != 1 || entries[0].SessionID != entry.SessionID {
			t.Fatalf("refused cleanup lost registry evidence: %+v complete=%v err=%v", entries, complete, err)
		}
		delete(runtime.direct.sessions, handle.ID)
		if err := ptyregistry.Unregister(ctx, handle.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("native exact environment argv cwd", func(t *testing.T) {
		t.Setenv("AO_PINNED_AMBIENT_SENTINEL", "must-not-leak")
		t.Setenv("TERM", "ambient-term")
		t.Setenv("COLORTERM", "ambient-color")
		for _, explicitTerminal := range []bool{true, false} {
			id := "pinned-env-absent"
			if explicitTerminal {
				id = "pinned-env-explicit"
			}
			probePath := filepath.Join(root, id+".json")
			cfg := good
			cfg.SessionID = domain.SessionID(id)
			cfg.Argv = []string{host, "-test.run=^TestPinnedChildProbe$", "--", "literal $HOME $(false)", "a b", ""}
			cfg.Env = map[string]string{runtimeLaunchIDEnv: id, "AO_PINNED_CHILD_PROBE": probePath}
			if explicitTerminal {
				cfg.Env["TERM"] = "explicit-terminal"
				cfg.Env["COLORTERM"] = "explicit-color"
			}
			handle, err := runtime.Create(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtime.Destroy(context.Background(), handle) })
			var data []byte
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				data, err = os.ReadFile(probePath)
				if err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err != nil {
				t.Fatalf("child fixture output: %v", err)
			}
			var got pinnedChildObservation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			slices.Sort(got.Env)
			if !reflect.DeepEqual(got.Argv, cfg.Argv) || !reflect.DeepEqual(got.Env, explicitUnixEnvironment(cfg.Env)) || got.CWD != root {
				t.Fatalf("child configuration differs: got %+v; argv=%q env=%q cwd=%q", got, cfg.Argv, explicitUnixEnvironment(cfg.Env), root)
			}
			if err := runtime.Destroy(ctx, handle); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Destroy(ctx, handle); !errors.Is(err, ports.ErrRuntimeProbeInconclusive) {
				t.Fatalf("stale completed handle Destroy = %v", err)
			}
			assertNoEffect(t, cfg) // an old handle can never target a new launch
			ref := ports.FencedRuntimeRef{Handle: handle, SessionID: cfg.SessionID, Generation: id}
			if got := runtime.ProbeFencedRuntime(ctx, ref); got.Liveness != ports.FencedUnknown {
				t.Fatalf("post-cleanup probe = %+v, want retained uncertainty", got)
			}
		}
	})

	t.Run("native generation-fenced stop", func(t *testing.T) {
		testPinnedFencedStop(t, runtime, root)
	})

	t.Run("native owned diagnostic disposal", func(t *testing.T) {
		testPinnedDiagnosticDisposal(t, runtime, root, false, false)
	})

	t.Run("native registration failure starts diagnostic disposal", func(t *testing.T) {
		t.Run("cleanup completes", func(t *testing.T) {
			testPinnedDiagnosticDisposal(t, runtime, root, true, false)
		})
		t.Run("cleanup timeout retains uncertainty", func(t *testing.T) {
			testPinnedDiagnosticDisposal(t, runtime, root, true, true)
		})
	})

	t.Run("post-start failure retains possible handle", func(t *testing.T) {
		cfg := good
		cfg.SessionID = "pinned-start-failure"
		cfg.Argv = []string{filepath.Join(root, "missing-command")}
		_, err := runtime.Create(ctx, cfg)
		var effect ports.RuntimeEffectError
		if !errors.As(err, &effect) || effect.EffectOutcome() != ports.RuntimeEffectPossible || effect.PossibleHandle().ID != string(cfg.SessionID) || effect.CleanupOutcome() != ports.RuntimeCleanupFailed {
			t.Fatalf("post-start failure = %v, want retained possible handle", err)
		}
		entries, complete, err := ptyregistry.Scan(ctx)
		if err != nil || !complete || len(entries) != 1 || entries[0].PtyHostPID <= 0 || entries[0].PipePath != unresolvedHostAddress || !entries[0].RetainUntilUnregistered {
			t.Fatalf("post-start registry = %+v complete=%v err=%v", entries, complete, err)
		}
		if got := runtime.ProbeFencedRuntime(ctx, ports.FencedRuntimeRef{Handle: effect.PossibleHandle(), SessionID: cfg.SessionID, Generation: cfg.Env[runtimeLaunchIDEnv]}); got.Liveness != ports.FencedUnknown {
			t.Fatalf("post-start probe = %+v", got)
		}
		if err := runtime.Destroy(ctx, effect.PossibleHandle()); !errors.Is(err, ports.ErrRuntimeProbeInconclusive) {
			t.Fatalf("reaped failed-start Destroy = %v, want retained unknown", err)
		}
		entries, complete, err = ptyregistry.Scan(ctx)
		if err != nil || !complete || len(entries) != 1 || entries[0].SessionID != string(cfg.SessionID) {
			t.Fatalf("failed-start refusal lost evidence: %+v complete=%v err=%v", entries, complete, err)
		}
	})
}

// This fixture owns only one PTY worker and its original host. It verifies
// diagnostic disposal and registration-failure cleanup, not containment of
// arbitrary descendants or safety of recovered process identities.
func testPinnedDiagnosticDisposal(t *testing.T, runtime *PinnedRuntime, root string, failRegistration, delayAbsenceObservation bool) {
	t.Helper()
	ctx := context.Background()
	id := "pinned-diagnostic-disposal"
	if failRegistration {
		id = "pinned-registration-disposal"
	}
	if delayAbsenceObservation {
		id += "-timeout"
	}
	ready := filepath.Join(root, id+".ready")
	cleaned := filepath.Join(root, id+".cleaned")
	// The race runtime otherwise sleeps for a second during host exit, longer
	// than Create's cleanup deadline. Keep native fixture disposal prompt.
	cfg := ports.RuntimeConfig{
		SessionID: domain.SessionID(id), WorkspacePath: root,
		Argv:                    []string{"/bin/sh", "-c", `trap 'printf cleaned > "$2"; exit 0' HUP TERM; printf '%s\n' "$$" > "$1"; while :; do IFS= read -r line; done`, "fixture", ready, cleaned},
		Env:                     map[string]string{runtimeLaunchIDEnv: id, "GORACE": "atexit_sleep_ms=0"},
		ExitOnCommandCompletion: true,
	}
	var workerPID int
	var owned pinnedOwnedHost
	t.Cleanup(func() {
		runtime.ownedMu.Lock()
		owned = runtime.owned[id]
		runtime.ownedMu.Unlock()
		if owned.process != nil && pinnedProcessAlive(owned.process) {
			_ = owned.process.Signal(syscall.SIGTERM)
		}
		deadline := time.Now().Add(3 * time.Second)
		for pinnedProcessAlive(owned.process) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if pinnedProcessAlive(owned.process) {
			_ = owned.process.Kill()
			t.Error("fixture host needed emergency cleanup")
		}
		if workerPID > 0 && !errors.Is(syscall.Kill(workerPID, 0), syscall.ESRCH) {
			_ = syscall.Kill(-workerPID, syscall.SIGKILL)
			t.Error("fixture worker needed emergency cleanup")
		}
		_ = ptyregistry.Unregister(ctx, id)
		runtime.direct.mu.Lock()
		delete(runtime.direct.sessions, id)
		runtime.direct.mu.Unlock()
	})
	waitReady := func(addr string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if data, err := os.ReadFile(ready); err == nil {
				workerPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				if workerPID > 0 {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		status, alive, err := clientStatusContext(ctx, addr)
		if workerPID <= 0 || err != nil || !alive || !status.Alive || status.PID != workerPID {
			t.Fatalf("worker readiness: pid=%d status=%+v hostAlive=%v err=%v", workerPID, status, alive, err)
		}
		if group, err := syscall.Getpgid(workerPID); err != nil || group != workerPID {
			t.Fatalf("fixture worker group = (%d,%v), want %d", group, err, workerPID)
		}
	}
	originalFinder := runtime.direct.processFinder
	forceLookups := 0
	runtime.direct.processFinder = func(pid int) (processKiller, error) {
		forceLookups++
		return originalFinder(pid)
	}
	defer func() { runtime.direct.processFinder = originalFinder }()
	// Model a delayed process-exit observation independently of race-runtime
	// exit timing. Native cleanup still runs; an unresolved observation must
	// preserve timeout/registry uncertainty until independently reconciled.
	originalAlive := runtime.direct.pidIsAlive
	var observationReady time.Time
	if delayAbsenceObservation {
		runtime.direct.pidIsAlive = func(pid int) bool {
			return time.Now().Before(observationReady) || originalAlive(pid)
		}
		defer func() { runtime.direct.pidIsAlive = originalAlive }()
	}
	injected := errors.New("injected post-start registration failure")
	originalRegister := runtime.direct.registerHost
	if failRegistration {
		runtime.direct.registerHost = func(ctx context.Context, entry ptyregistry.Entry) error {
			if entry.PtyHostPID > 0 {
				waitReady(entry.PipePath)
				observationReady = time.Now().Add(1500 * time.Millisecond)
				return injected
			}
			return originalRegister(ctx, entry)
		}
		defer func() { runtime.direct.registerHost = originalRegister }()
	}
	handle, createErr := runtime.Create(ctx, cfg)
	runtime.ownedMu.Lock()
	owned = runtime.owned[id]
	runtime.ownedMu.Unlock()
	if owned.process == nil {
		t.Fatalf("fixture did not start an owned host: %v", createErr)
	}
	if failRegistration {
		var effect ports.RuntimeEffectError
		if !errors.Is(createErr, injected) || !errors.As(createErr, &effect) || effect.EffectOutcome() != ports.RuntimeEffectPossible || effect.PossibleHandle().ID != id {
			t.Fatalf("registration failure = %v", createErr)
		}
		wantCleanup := ports.RuntimeCleanupSucceeded
		if delayAbsenceObservation {
			wantCleanup = ports.RuntimeCleanupFailed
			if !errors.Is(createErr, context.DeadlineExceeded) {
				t.Fatalf("cleanup timeout lost its cause: %v", createErr)
			}
		}
		if effect.CleanupOutcome() != wantCleanup {
			t.Fatalf("cleanup = %s, want %s: %v", effect.CleanupOutcome(), wantCleanup, createErr)
		}
	} else {
		if createErr != nil {
			t.Fatal(createErr)
		}
		waitReady(owned.addr)
		cleanupCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := runtime.Destroy(cleanupCtx, handle); err != nil {
			t.Fatalf("owned diagnostic disposal: %v", err)
		}
	}
	if data, err := os.ReadFile(cleaned); err != nil || string(data) != "cleaned" {
		t.Fatalf("worker did not observe graceful PTY cleanup: %q %v", data, err)
	}
	if forceLookups != 0 {
		t.Fatalf("graceful disposal entered force-kill path %d times", forceLookups)
	}
	entries, complete, err := ptyregistry.Scan(ctx)
	if err != nil || !complete {
		t.Fatalf("registry scan: complete=%v err=%v", complete, err)
	}
	retained := false
	for _, entry := range entries {
		retained = retained || entry.SessionID == id
	}
	if retained != delayAbsenceObservation {
		t.Fatalf("cleanup registry retained=%v, timeout=%v", retained, delayAbsenceObservation)
	}
	deadline := time.Now().Add(3 * time.Second)
	for (pinnedProcessAlive(owned.process) || !errors.Is(syscall.Kill(workerPID, 0), syscall.ESRCH)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pinnedProcessAlive(owned.process) || !errors.Is(syscall.Kill(workerPID, 0), syscall.ESRCH) {
		t.Fatalf("diagnostic disposal left its fixture host or worker alive: host=%d worker=%d", owned.process.Pid, workerPID)
	}
	t.Logf("diagnostic_host=%d worker=%d graceful_cleanup_observed=true force_kill=false fixture_processes_absent=true registration_failure=%v timeout_evidence_retained=%v", owned.process.Pid, workerPID, failRegistration, delayAbsenceObservation)
}
