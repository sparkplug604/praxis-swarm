//go:build darwin

package conpty

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// These fixtures deliberately stay in the original PTY process group. They
// establish no guarantee for detached descendants or reused process identities.
func TestDarwinSwarmPTYCloseReapsStubbornChildAndGrandchild(t *testing.T) {
	conn, pids := startDarwinSwarmGroupFixture(t, false)
	if _, exited := conn.ExitCode(); exited {
		t.Fatal("fixture parent exited before Close")
	}
	assertDarwinSwarmCloseReapsGroup(t, conn, pids)
}

func TestDarwinSwarmPTYCloseRefusesGroupAfterParentExit(t *testing.T) {
	conn, pids := startDarwinSwarmGroupFixture(t, true)
	select {
	case <-conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("fixture parent did not exit before Close")
	}
	if code, exited := conn.ExitCode(); !exited || code != 0 {
		t.Fatalf("fixture parent exit = (%d, %v), want (0, true)", code, exited)
	}
	for _, role := range []string{"child", "grandchild"} {
		if err := syscall.Kill(pids[role], 0); err != nil {
			t.Fatalf("%s pid %d did not survive its parent: %v", role, pids[role], err)
		}
	}
	closeErr := conn.Close()
	if !errors.Is(closeErr, ports.ErrRuntimeProbeInconclusive) {
		t.Fatalf("Close after parent exit = %v, want unproven group ownership", closeErr)
	}
	if err := conn.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("repeated Close lost original uncertainty: first=%v second=%v", closeErr, err)
	}
	for _, role := range []string{"child", "grandchild"} {
		if err := syscall.Kill(pids[role], 0); err != nil {
			t.Fatalf("Close signaled or lost the unowned %s pid %d: %v", role, pids[role], err)
		}
	}
	t.Logf("parent-first limitation observed: group=%d close=inconclusive descendants_survive=true", conn.PID())
	// The test knows these freshly observed descendants belong to its fixture.
	// This explicit cleanup is not the runtime's ownership strategy.
	if err := syscall.Kill(-conn.PID(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !errors.Is(syscall.Kill(-conn.PID(), 0), syscall.ESRCH) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	for role, pid := range pids {
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("fixture cleanup left %s pid %d: %v", role, pid, err)
		}
	}
	if err := syscall.Kill(-conn.PID(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("fixture cleanup left group %d: %v", conn.PID(), err)
	}
	if err := conn.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("fixture cleanup erased the original Close uncertainty: %v", err)
	}
	t.Log("fixture_cleanup=confirmed original_close_uncertainty=retained")
}

func TestDarwinSwarmPTYCloseAfterParentExitWithAbsentGroup(t *testing.T) {
	conn, err := newConPTY(t.TempDir(), "/bin/sh", []string{"-c", "exit 0"}, initialConPTYColumns, initialConPTYRows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	select {
	case <-conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("fixture parent did not exit")
	}
	if err := syscall.Kill(-conn.PID(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("fixture group is not absent before Close: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close of absent group: %v", err)
	}
}

func startDarwinSwarmGroupFixture(t *testing.T, parentExits bool) (ptyConn, map[string]int) {
	t.Helper()
	dir := t.TempDir()
	scripts := map[string]string{
		"parent.sh": `trap '' HUP TERM
/bin/sh "$1/child.sh" "$1" &
printf '%s\n' "$$" > "$1/parent.pid"
if [ "$2" = exit ]; then
  while [ ! -s "$1/grandchild.pid" ]; do /bin/sleep 0.01; done
  exit 0
fi
wait
`,
		"child.sh": `trap '' HUP TERM
/bin/sh "$1/grandchild.sh" "$1" &
printf '%s\n' "$$" > "$1/child.pid"
wait
`,
		"grandchild.sh": `trap '' HUP TERM
printf '%s\n' "$$" > "$1/grandchild.pid"
exec /bin/sleep 30
`,
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mode := "wait"
	if parentExits {
		mode = "exit"
	}
	conn, err := newConPTY(dir, "/bin/sh", []string{filepath.Join(dir, "parent.sh"), dir, mode}, initialConPTYColumns, initialConPTYRows)
	if err != nil {
		t.Fatal(err)
	}
	pgid := conn.PID()
	if pgid <= 0 {
		_ = conn.Close()
		t.Fatalf("fixture has invalid process group %d", pgid)
	}
	t.Cleanup(func() {
		// This fixture creates only original-group descendants. Cleanup never
		// scans or signals unrelated processes, even when readiness fails.
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = conn.Close()
		deadline := time.Now().Add(2 * time.Second)
		for !errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if err := syscall.Kill(-pgid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("fixture cleanup could not prove process group %d absent: %v", pgid, err)
		}
	})
	pids := map[string]int{}
	deadline := time.Now().Add(5 * time.Second)
	for len(pids) < 3 && time.Now().Before(deadline) {
		for _, role := range []string{"parent", "child", "grandchild"} {
			data, err := os.ReadFile(filepath.Join(dir, role+".pid"))
			if err != nil {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && pid > 0 {
				pids[role] = pid
			}
		}
		if len(pids) < 3 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if len(pids) != 3 || pids["parent"] != pgid || pids["child"] == pids["grandchild"] {
		t.Fatalf("incomplete or invalid fixture readiness: group=%d pids=%v", pgid, pids)
	}
	for _, role := range []string{"child", "grandchild"} {
		actualGroup, err := syscall.Getpgid(pids[role])
		if err != nil || actualGroup != pgid {
			t.Fatalf("%s pid %d group=(%d,%v), want %d", role, pids[role], actualGroup, err, pgid)
		}
	}
	t.Logf("fixture ready: original_group=%d parent=%d child=%d grandchild=%d parent_exits=%v", pgid, pids["parent"], pids["child"], pids["grandchild"], parentExits)
	return conn, pids
}

func assertDarwinSwarmCloseReapsGroup(t *testing.T, conn ptyConn, pids map[string]int) {
	t.Helper()
	closed := make(chan error, 1)
	go func() { closed <- conn.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close exceeded the fixture's bounded teardown deadline")
	}
	// Verify the OS observation directly rather than using the implementation's
	// group helper or the parent-only Done channel as the assertion.
	absent := true
	for role, pid := range pids {
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			absent = false
			t.Errorf("%s pid %d remains after Close: %v", role, pid, err)
		}
	}
	if err := syscall.Kill(-conn.PID(), 0); !errors.Is(err, syscall.ESRCH) {
		absent = false
		t.Errorf("original process group %d remains after Close: %v", conn.PID(), err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("repeated successful Close: %v", err)
	}
	t.Logf("fixture teardown observed: group=%d all_recorded_pids_absent=%v", conn.PID(), absent)
}
