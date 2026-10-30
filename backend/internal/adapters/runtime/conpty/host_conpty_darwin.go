//go:build darwin

// Modified for Praxis Swarm: task contracts, supervised execution and verification.

package conpty

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/creack/pty"
)

// darwinPTYConn is a native macOS pseudoterminal owned by the detached host.
// The child starts in its own session/process group (creack/pty's StartWithSize
// does this). Teardown targets that group while its leader is still observed
// running. After the leader exits, a retained numeric group id does not prove
// ownership and Close refuses to signal it. This does not contain detached
// descendants or establish PID/group-id-reuse-proof control.
type darwinPTYConn struct {
	pty *os.File
	cmd *exec.Cmd

	closeOnce sync.Once
	closeErr  error
	doneC     chan struct{}
	exitMu    sync.Mutex
	exitCode  int
	exited    bool
}

const darwinPTYCloseGrace = 500 * time.Millisecond

func newConPTY(cwd, shellCmd string, shellArgs []string, cols, rows uint16) (ptyConn, error) {
	// shellCmd and shellArgs are the runtime launch argv assembled by AO's
	// trusted agent adapter, not input interpreted by a shell.
	//nolint:forbidigo // Interactive ConPTY children require direct process control.
	cmd := exec.Command(shellCmd, shellArgs...) // #nosec G702 -- intentional direct argv execution
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, fmt.Errorf("darwin pty: start command: %w", err)
	}

	c := &darwinPTYConn{pty: f, cmd: cmd, doneC: make(chan struct{})}
	go c.wait()
	return c, nil
}

func (c *darwinPTYConn) wait() {
	err := c.cmd.Wait()
	code := 0
	if c.cmd.ProcessState != nil {
		code = c.cmd.ProcessState.ExitCode()
	} else if err != nil {
		code = -1
	}
	c.exitMu.Lock()
	c.exitCode = code
	c.exited = true
	c.exitMu.Unlock()
	close(c.doneC)
}

func (c *darwinPTYConn) Read(b []byte) (int, error)  { return c.pty.Read(b) }
func (c *darwinPTYConn) Write(b []byte) (int, error) { return c.pty.Write(b) }

func (c *darwinPTYConn) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 || cols > math.MaxUint16 || rows > math.MaxUint16 {
		return fmt.Errorf("darwin pty: invalid size %dx%d", cols, rows)
	}
	return pty.Setsize(c.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (c *darwinPTYConn) Close() error {
	c.closeOnce.Do(func() {
		if c.cmd.Process != nil {
			pgid := c.cmd.Process.Pid
			if _, exited := c.ExitCode(); exited {
				// Scrollback can retain this PTY long after its leader is reaped.
				// The numeric group may now belong to unrelated processes. Only
				// absence is conclusive here; never signal a surviving group.
				if err := syscall.Kill(-pgid, 0); !errors.Is(err, syscall.ESRCH) {
					c.closeErr = fmt.Errorf("darwin pty: leader exited; ownership of process group %d cannot be proven: %w", pgid, errors.Join(ports.ErrRuntimeProbeInconclusive, err))
				}
			} else {
				var signalErr error
				signalGroup := func(signal syscall.Signal) {
					if err := syscall.Kill(-pgid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
						signalErr = errors.Join(signalErr, fmt.Errorf("darwin pty: signal process group %d with %s: %w", pgid, signal, err))
					}
				}
				// SIGHUP preserves prompt terminal close for interactive shells
				// that ignore SIGTERM. Stubborn original-group members get SIGKILL.
				signalGroup(syscall.SIGHUP)
				signalGroup(syscall.SIGTERM)
				if !waitForDarwinProcessGroupExit(pgid, darwinPTYCloseGrace) {
					signalGroup(syscall.SIGKILL)
					if !waitForDarwinProcessGroupExit(pgid, darwinPTYCloseGrace) {
						c.closeErr = errors.Join(signalErr, fmt.Errorf("darwin pty: process group %d remains present after SIGKILL: %w", pgid, ports.ErrRuntimeProbeInconclusive))
					}
				}
				// Darwin can return EPERM while an exiting group contains only
				// zombies. Final ESRCH evidence settles such an intermediate signal
				// failure; otherwise preserve it with the inconclusive teardown.
			}
		}
		c.closeErr = errors.Join(c.closeErr, c.pty.Close())
	})
	return c.closeErr
}

func waitForDarwinProcessGroupExit(pgid int, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !darwinProcessGroupAlive(pgid) {
			return true
		}
		select {
		case <-deadline.C:
			return !darwinProcessGroupAlive(pgid)
		case <-ticker.C:
		}
	}
}

func darwinProcessGroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	// Only ESRCH proves absence. Permission or other probe failures must not
	// turn an uncertain teardown into success.
	return !errors.Is(err, syscall.ESRCH)
}

func (c *darwinPTYConn) Done() <-chan struct{} { return c.doneC }

func (c *darwinPTYConn) PID() int {
	if c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

func (c *darwinPTYConn) ExitCode() (int, bool) {
	c.exitMu.Lock()
	defer c.exitMu.Unlock()
	return c.exitCode, c.exited
}
