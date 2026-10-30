//go:build darwin

package macruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/conpty"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "pty-host" {
		os.Exit(conpty.RunHost(os.Args[2:], os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "swarm-sandbox-helper" {
		os.Exit(helper(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func fixture(t *testing.T) SandboxOptions {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base = filepath.Join(base, "quote\"-back\\slash-雪")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	roots := []string{"workspace", "data", "coordinator", "evaluator"}
	for i, name := range roots {
		roots[i] = filepath.Join(base, name)
		if err := os.Mkdir(roots[i], 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(roots[i], "sentinel"), []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return SandboxOptions{Workspace: roots[0], DataDir: roots[1], Coordinator: roots[2], Evaluator: roots[3]}
}

func TestDeviceIdentityPreservesSignedEncoding(t *testing.T) {
	for _, tt := range []struct {
		device int32
		want   uint64
	}{
		{-2147483648, 18446744071562067968},
		{-2, 18446744073709551614},
		{-1, 18446744073709551615},
		{0, 0},
		{1, 1},
		{2147483647, 2147483647},
	} {
		if got := deviceIdentity(tt.device); got != tt.want {
			t.Fatalf("device %d encoded as %d, want %d", tt.device, got, tt.want)
		}
	}
}

func TestSandboxRejectsDriftAndOverlap(t *testing.T) {
	ctx := context.Background()
	options := fixture(t)
	registry := filepath.Join(filepath.Dir(options.Workspace), "registry")
	if err := os.Mkdir(registry, 0o700); err != nil {
		t.Fatal(err)
	}
	host, err := identify("/bin/sh", false)
	if err != nil {
		t.Fatal(err)
	}
	binding := conpty.PinnedBinding{Backend: "darwin-direct-pty", HostExecutable: "/bin/sh", HostSHA256: host.SHA256, RegistryPath: filepath.Join(registry, "registry.json"), EnvironmentSemanticsVersion: "explicit-full-env-v1"}
	makeSandbox := func(options SandboxOptions) (*Sandbox, error) { return newSandbox(ctx, options, binding) }
	s, err := makeSandbox(options)
	if err != nil {
		t.Fatal(err)
	}
	cfg := ports.RuntimeConfig{WorkspacePath: options.Workspace, Argv: []string{"/bin/echo", "hello"}, Env: map[string]string{"X": "value"}}
	wrapped, err := s.Wrap(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if wrapped.Argv[0] != sandboxExecutable || len(s.Binding()) != 64 {
		t.Fatal("missing immutable policy binding")
	}
	wrapped.Env["X"] = "changed"
	if cfg.Env["X"] != "value" {
		t.Fatal("environment alias")
	}
	bad := options
	bad.Evaluator = filepath.Join(options.Workspace, "nested")
	if _, err := makeSandbox(bad); err == nil {
		t.Fatal("accepted overlapping protected root")
	}
	bad = options
	bad.Coordinator = "/usr/bin"
	if _, err := makeSandbox(bad); err == nil {
		t.Fatal("accepted system overlap")
	}
	bad = options
	bad.DataDir = "/"
	if _, err := makeSandbox(bad); err == nil {
		t.Fatal("accepted writable filesystem root")
	}
	// Physical volume aliases must not turn a protected root into scratch.
	alias := "/System/Volumes/Data" + options.Coordinator
	if a, err := os.Stat(alias); err == nil {
		b, err := os.Stat(options.Coordinator)
		if err == nil && os.SameFile(a, b) {
			bad = options
			bad.DataDir = alias
			if _, err := makeSandbox(bad); err == nil {
				t.Fatal("accepted firmlink root overlap")
			}
		}
	}
	link := filepath.Join(filepath.Dir(options.Workspace), "alias")
	if err := os.Symlink(options.Workspace, link); err != nil {
		t.Fatal(err)
	}
	bad = options
	bad.Workspace = link
	if _, err := makeSandbox(bad); err == nil {
		t.Fatal("accepted symlink root")
	}
	bad = options
	bad.DataDir = registry
	if _, err := makeSandbox(bad); err == nil {
		t.Fatal("accepted writable registry")
	}
	hardlink := filepath.Join(options.DataDir, "protected-hardlink")
	if err := os.Link(filepath.Join(options.Coordinator, "sentinel"), hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wrap(ctx, cfg); err == nil {
		t.Fatal("accepted protected hardlink added after binding")
	}
	if _, err := makeSandbox(options); err == nil {
		t.Fatal("accepted preexisting protected hardlink")
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(options.DataDir, options.DataDir+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(options.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wrap(ctx, cfg); err == nil {
		t.Fatal("accepted replaced root")
	}
}

// This executes real offline helper processes through the production PTY host.
// A sandbox denial or unsupported host fails visibly; it is never a silent skip.
func TestMacSandboxNativeDiagnostics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	host, err := identify(exe, false)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := conpty.NewPinnedRuntime(ctx, conpty.PinnedOptions{RunFilePath: filepath.Join(registry, "run.json"), HostExecutable: exe, HostSHA256: host.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("runtime_binding=%+v", runtime.Binding())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, readOnly := range []bool{false, true} {
		name := "worker"
		if readOnly {
			name = "reviewer"
		}
		t.Run(name, func(t *testing.T) {
			options := fixture(t)
			options.ReadOnly = readOnly
			options.Tools = []string{exe}
			s, err := NewSandbox(ctx, runtime, options)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(filepath.Dir(options.Workspace), "outside-sentinel")
			if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			// Establish that alias probes address existing, identical files; an
			// ENOENT from an unrelated spelling is not sandbox-denial evidence.
			for _, path := range []string{outside, filepath.Join(options.Coordinator, "sentinel")} {
				original, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, alias := range []string{"/System/Volumes/Data" + path, strings.ToUpper(path)} {
					info, err := os.Stat(alias)
					if err != nil || !os.SameFile(original, info) {
						t.Fatalf("alias fixture unavailable: %s: %v", alias, err)
					}
				}
			}
			argv := []string{exe, "swarm-sandbox-helper", "probe", options.Workspace, options.DataDir, options.Coordinator, options.Evaluator, outside, listener.Addr().String()}
			cfg, err := s.Wrap(ctx, ports.RuntimeConfig{SessionID: domain.SessionID("sandbox-" + name), WorkspacePath: options.Workspace, Argv: argv,
				Env: map[string]string{"AO_RUNTIME_LAUNCH_ID": name, "HOME": options.DataDir, "TMPDIR": options.DataDir}, ExitOnCommandCompletion: true})
			if err != nil {
				t.Fatal(err)
			}
			handle, err := runtime.Create(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Destroy(context.Background(), handle)
			data := waitFile(t, filepath.Join(options.DataDir, "result.json"))
			var results map[string]bool
			if err := json.Unmarshal(data, &results); err != nil {
				t.Fatal(err)
			}
			for key, value := range results {
				want := true
				if key == "workspace_write" || key == "workspace_overwrite" || key == "workspace_descendant_write" {
					want = !readOnly
				}
				if value != want {
					t.Errorf("%s=%v want %v", key, value, want)
				}
			}
			if len(results) != 23 {
				t.Fatalf("incomplete diagnostic: %s", data)
			}
			for _, root := range []string{options.Coordinator, options.Evaluator} {
				got, err := os.ReadFile(filepath.Join(root, "sentinel"))
				if err != nil || string(got) != "original" {
					t.Errorf("protected sentinel changed: %s %v", root, err)
				}
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 1 {
					t.Errorf("protected directory changed: %s %v", root, err)
				}
			}
			if readOnly {
				got, err := os.ReadFile(filepath.Join(options.Workspace, "sentinel"))
				if err != nil || string(got) != "original" {
					t.Fatal("reviewer mutated submission")
				}
			}
			t.Logf("role=%s policy=%s checks=%s", name, s.Binding(), data)
		})
	}
	t.Run("detached-descendant-remains-unqualified", func(t *testing.T) {
		options := fixture(t)
		options.Tools = []string{exe}
		s, err := NewSandbox(ctx, runtime, options)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := s.Wrap(ctx, ports.RuntimeConfig{SessionID: "sandbox-escape", WorkspacePath: options.Workspace,
			Argv: []string{exe, "swarm-sandbox-helper", "escape-parent", options.DataDir, options.Coordinator},
			Env:  map[string]string{"AO_RUNTIME_LAUNCH_ID": "escape"}, ExitOnCommandCompletion: true})
		if err != nil {
			t.Fatal(err)
		}
		handle, err := runtime.Create(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer runtime.Destroy(context.Background(), handle)
		pidText := waitFile(t, filepath.Join(options.DataDir, "detached.pid"))
		pid, err := strconv.Atoi(string(pidText))
		if err != nil || pid <= 1 {
			t.Fatalf("bad fixture PID %s", pidText)
		}
		defer syscall.Kill(pid, syscall.SIGKILL) // known disposable child only; never a runtime ownership strategy
		if got := string(waitFile(t, filepath.Join(options.DataDir, "detached-denied"))); got != "denied" {
			t.Fatal("detached child escaped file policy")
		}
		ref := ports.FencedRuntimeRef{Handle: handle, SessionID: cfg.SessionID, Generation: "escape"}
		stopped, err := runtime.StopFencedRuntime(ctx, ref)
		if err != nil || !stopped.Acknowledged || stopped.Cleanup != ports.RuntimeCleanupSucceeded {
			t.Fatalf("fenced PTY stop: %+v %v", stopped, err)
		}
		if err := syscall.Kill(pid, 0); err != nil {
			t.Fatalf("expected documented detached-child survival after fenced PTY stop, got %v", err)
		}
		probe := runtime.ProbeFencedRuntime(ctx, ref)
		if probe.Liveness != ports.FencedUnknown {
			t.Fatalf("unsafe absence claim: %+v", probe)
		}
		replayed, err := runtime.StopFencedRuntime(ctx, ref)
		if err != nil || replayed != stopped {
			t.Fatalf("fenced stop replay: %+v %v", replayed, err)
		}
		t.Logf("detached_pid=%d survives_fenced_pty_stop=true protected_write=denied stop_acknowledged=true receipt_replayed=true fenced_probe=%s containment=failed", pid, probe.Liveness)
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) && syscall.Kill(pid, 0) == nil {
			time.Sleep(20 * time.Millisecond)
		}
		if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
			t.Fatalf("fixture cleanup not confirmed: %v", err)
		}
		t.Log("fixture_cleanup=confirmed")
	})
}

func waitFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("helper did not produce %s", path)
	return nil
}

func helper(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "probe":
		w, d, c, e, outside, address := args[1], args[2], args[3], args[4], args[5], args[6]
		results := map[string]bool{}
		_, err := os.ReadFile(filepath.Join(w, "sentinel"))
		results["workspace_read"] = err == nil
		results["workspace_write"] = os.WriteFile(filepath.Join(w, "output"), []byte("ok"), 0o600) == nil
		results["workspace_overwrite"] = os.WriteFile(filepath.Join(w, "sentinel"), []byte("changed"), 0o600) == nil
		results["scratch_write"] = os.WriteFile(filepath.Join(d, "output"), []byte("ok"), 0o600) == nil
		for i, root := range []string{c, e} {
			key := fmt.Sprintf("protected_%d_", i)
			_, err := os.ReadFile(filepath.Join(root, "sentinel"))
			results[key+"read_denied"] = denied(err)
			results[key+"write_denied"] = denied(os.WriteFile(filepath.Join(root, "sentinel"), []byte("bad"), 0o600))
			results[key+"create_denied"] = denied(os.WriteFile(filepath.Join(root, "new"), []byte("bad"), 0o600))
		}
		_, err = os.ReadFile(outside)
		results["outside_read_denied"] = denied(err)
		// The data-volume alias and alternate case must not bypass protection.
		_, err = os.ReadFile("/System/Volumes/Data" + outside)
		results["outside_alias_read_denied"] = denied(err)
		_, err = os.ReadFile("/System/Volumes/Data" + filepath.Join(c, "sentinel"))
		results["protected_alias_read_denied"] = denied(err)
		results["protected_alias_write_denied"] = denied(os.WriteFile("/System/Volumes/Data"+filepath.Join(c, "sentinel"), []byte("bad"), 0o600))
		results["protected_delete_denied"] = denied(os.Remove(filepath.Join(e, "sentinel")))
		_, err = os.ReadFile(strings.ToUpper(filepath.Join(c, "sentinel")))
		results["protected_case_read_denied"] = denied(err)
		results["protected_case_write_denied"] = denied(os.WriteFile(strings.ToUpper(filepath.Join(c, "sentinel")), []byte("bad"), 0o600))
		link := filepath.Join(d, "protected-link")
		if err := os.Symlink(filepath.Join(c, "sentinel"), link); err != nil {
			return 3
		}
		results["symlink_write_denied"] = denied(os.WriteFile(link, []byte("bad"), 0o600))
		results["hardlink_denied"] = denied(os.Link(filepath.Join(c, "sentinel"), filepath.Join(d, "hardlink")))
		results["rename_denied"] = denied(os.Rename(filepath.Join(e, "sentinel"), filepath.Join(d, "renamed")))
		conn, err := net.DialTimeout("tcp", address, time.Second)
		results["network_denied"] = denied(err)
		if conn != nil {
			conn.Close()
		}
		// A child shell inherits the sandbox. Use positional args, never interpolate paths as code.
		for i, target := range []string{filepath.Join(c, "child-write"), filepath.Join(w, "child-write")} {
			ready, result := filepath.Join(d, fmt.Sprintf("child-%d-ready", i)), filepath.Join(d, fmt.Sprintf("child-%d-result", i))
			cmd := exec.Command("/bin/sh", "-c", `printf ready > "$1" || exit 41; if printf changed > "$2"; then printf allowed > "$3"; else printf denied > "$3"; fi`, "probe", ready, target, result)
			if cmd.Run() != nil {
				return 8
			}
			marker, err := os.ReadFile(ready)
			if err != nil || string(marker) != "ready" {
				return 9
			}
			status, err := os.ReadFile(result)
			if err != nil {
				return 10
			}
			if i == 0 {
				results["descendant_write_denied"] = string(status) == "denied"
			} else {
				results["workspace_descendant_write"] = string(status) == "allowed"
			}
		}
		data, _ := json.Marshal(results)
		if os.WriteFile(filepath.Join(d, "result.json"), data, 0o600) != nil {
			return 4
		}
		return 0
	case "escape-parent":
		cmd := exec.Command(os.Args[0], "swarm-sandbox-helper", "escape-child", args[1], args[2])
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return 5
		}
		_ = cmd.Process.Release()
		time.Sleep(30 * time.Second)
		return 0
	case "escape-child":
		signal.Ignore(syscall.SIGHUP, syscall.SIGTERM)
		if os.WriteFile(filepath.Join(args[1], "detached.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600) != nil {
			return 6
		}
		status := "allowed"
		if denied(os.WriteFile(filepath.Join(args[2], "escape-write"), []byte("bad"), 0o600)) {
			status = "denied"
		}
		if os.WriteFile(filepath.Join(args[1], "detached-denied"), []byte(status), 0o600) != nil {
			return 7
		}
		time.Sleep(30 * time.Second)
		return 0
	}
	return 2
}

func denied(err error) bool { return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) }
