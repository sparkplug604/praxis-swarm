package conpty

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type fencedStopTestPTY struct {
	*fakePTY
	closeCalls atomic.Int32
	closeErr   error
	started    chan struct{}
	release    chan struct{}
	startOnce  sync.Once
}

func (p *fencedStopTestPTY) Close() error {
	p.closeCalls.Add(1)
	p.startOnce.Do(func() { close(p.started) })
	if p.release != nil {
		<-p.release
	}
	_ = p.fakePTY.Close()
	return p.closeErr
}

type fencedStopFixture struct {
	host   *host
	pty    *fencedStopTestPTY
	addr   string
	cancel context.CancelFunc
	done   chan error
}

func startFencedStopFixture(t *testing.T, fenced bool, closeErr error, release chan struct{}) *fencedStopFixture {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &fencedStopTestPTY{fakePTY: newFakePTY(1001), closeErr: closeErr, started: make(chan struct{}), release: release}
	h := newHost(ServeConfig{
		SessionID: "session-fenced", LaunchGeneration: "launch-original", RequireFencedStop: fenced,
		Listener: ln, PTY: p, Ring: NewRing(), LazySurface: true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	f := &fencedStopFixture{host: h, pty: p, addr: ln.Addr().String(), cancel: cancel, done: done}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("fenced test host did not shut down")
		}
	})
	return f
}

func fencedStopTestRequest() FencedStopRequest {
	return FencedStopRequest{Version: FencedStopProtocolVersion, SessionID: "session-fenced", Generation: "launch-original"}
}

func rawFencedStop(t *testing.T, addr string, payload []byte) FencedStopResponse {
	t.Helper()
	client := newTestClient(t, addr)
	defer client.close()
	if err := client.send(MsgFencedStopReq, payload); err != nil {
		t.Fatal(err)
	}
	for {
		typ, payload := client.readFrame(t)
		if typ == MsgFencedStopRes {
			var res FencedStopResponse
			if err := json.Unmarshal(payload, &res); err != nil {
				t.Fatal(err)
			}
			return res
		}
	}
}

func TestFencedStopRefusesBeforeClose(t *testing.T) {
	valid := fencedStopTestRequest()
	marshal := func(req FencedStopRequest) []byte { b, _ := json.Marshal(req); return b }
	tests := []struct {
		name    string
		fenced  bool
		payload []byte
	}{
		{"legacy host", false, marshal(valid)},
		{"malformed", true, []byte("{")},
		{"unknown field", true, []byte(`{"version":1,"sessionId":"session-fenced","generation":"launch-original","force":true}`)},
		{"oversized", true, []byte(strings.Repeat(" ", maxFencedStopRequestBytes+1))},
		{"wrong session", true, marshal(FencedStopRequest{1, "other-session", valid.Generation})},
		{"stale generation", true, marshal(FencedStopRequest{1, valid.SessionID, "launch-old"})},
		{"empty session", true, marshal(FencedStopRequest{1, "", valid.Generation})},
		{"empty generation", true, marshal(FencedStopRequest{1, valid.SessionID, ""})},
		{"blank generation", true, marshal(FencedStopRequest{1, valid.SessionID, "  "})},
		{"long generation", true, marshal(FencedStopRequest{1, valid.SessionID, strings.Repeat("x", maxFencedStopGenerationBytes+1)})},
		{"unsupported version", true, marshal(FencedStopRequest{2, valid.SessionID, valid.Generation})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := startFencedStopFixture(t, tt.fenced, nil, nil)
			res := rawFencedStop(t, f.addr, tt.payload)
			if res.Accepted || res.Cleanup != string(ports.RuntimeCleanupNotAttempted) || res.Error == "" {
				t.Fatalf("invalid refusal: %+v", res)
			}
			if n := f.pty.closeCalls.Load(); n != 0 {
				t.Fatalf("refusal closed PTY %d times", n)
			}
		})
	}
}

func TestFencedStopRejectsLegacyKillAndRetainsListener(t *testing.T) {
	f := startFencedStopFixture(t, true, nil, nil)
	client := newTestClient(t, f.addr)
	defer client.close()
	if err := client.send(MsgKillReq, nil); err != nil {
		t.Fatal(err)
	}
	// Same connection preserves request order: receiving this status proves the
	// preceding kill was dispatched without scheduling shutdown.
	if err := client.send(MsgStatusReq, nil); err != nil {
		t.Fatal(err)
	}
	if typ, _ := client.readFrame(t); typ != MsgStatusRes {
		t.Fatalf("response type %x", typ)
	}
	if n := f.pty.closeCalls.Load(); n != 0 {
		t.Fatalf("legacy kill closed PTY %d times", n)
	}
	res, err := clientFencedStop(context.Background(), f.addr, fencedStopTestRequest())
	if err != nil || !res.Accepted || res.Cleanup != string(ports.RuntimeCleanupSucceeded) {
		t.Fatalf("stop = %+v, %v", res, err)
	}
	replay, err := clientFencedStop(context.Background(), f.addr, fencedStopTestRequest())
	if err != nil || replay != res || f.pty.closeCalls.Load() != 1 {
		t.Fatalf("replay = %+v, %v; closes %d", replay, err, f.pty.closeCalls.Load())
	}
}

func TestFencedStopRetainsCleanupFailureThroughReplayAndShutdown(t *testing.T) {
	closeErr := errors.New("injected PTY cleanup failure")
	f := startFencedStopFixture(t, true, closeErr, nil)
	res, err := clientFencedStop(context.Background(), f.addr, fencedStopTestRequest())
	if err != nil || !res.Accepted || res.Cleanup != string(ports.RuntimeCleanupFailed) || res.Error != closeErr.Error() {
		t.Fatalf("stop = %+v, %v", res, err)
	}
	replay, err := clientFencedStop(context.Background(), f.addr, fencedStopTestRequest())
	if err != nil || replay != res {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	f.cancel()
	select {
	case err := <-f.done:
		if !errors.Is(err, closeErr) {
			t.Fatalf("Serve error = %v", err)
		}
		f.done <- err // cleanup also waits for completion
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	if n := f.pty.closeCalls.Load(); n != 1 {
		t.Fatalf("stop plus shutdown closed PTY %d times", n)
	}
}

func TestFencedStopConcurrentReplayDoesNotHoldHostMutex(t *testing.T) {
	release := make(chan struct{})
	f := startFencedStopFixture(t, true, nil, release)
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	const count = 8
	results := make(chan FencedStopResponse, count)
	errorsC := make(chan error, count)
	for range count {
		go func() {
			res, err := clientFencedStop(context.Background(), f.addr, fencedStopTestRequest())
			results <- res
			errorsC <- err
		}()
	}
	select {
	case <-f.pty.started:
	case <-time.After(2 * time.Second):
		t.Fatal("close was not entered")
	}
	// A blocking Close must not obstruct status/attach or a mismatched request.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, alive, err := clientStatusContext(ctx, f.addr); err != nil || !alive {
		t.Fatalf("status during close: alive=%v err=%v", alive, err)
	}
	wrong := fencedStopTestRequest()
	wrong.Generation = "stale"
	res, err := clientFencedStop(ctx, f.addr, wrong)
	if err != nil || res.Accepted || res.Cleanup != string(ports.RuntimeCleanupNotAttempted) {
		t.Fatalf("mismatch during close = %+v, %v", res, err)
	}
	releaseOnce.Do(func() { close(release) })
	want := FencedStopResponse{Version: 1, SessionID: "session-fenced", Generation: "launch-original", Accepted: true, Cleanup: string(ports.RuntimeCleanupSucceeded)}
	for range count {
		if err := <-errorsC; err != nil {
			t.Fatal(err)
		}
		if got := <-results; got != want {
			t.Fatalf("response = %+v", got)
		}
	}
	if n := f.pty.closeCalls.Load(); n != 1 {
		t.Fatalf("concurrent stop closed PTY %d times", n)
	}
}

func TestFencedStopLostReplyCanBeReplayed(t *testing.T) {
	release := make(chan struct{})
	f := startFencedStopFixture(t, true, nil, release)
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := clientFencedStop(ctx, f.addr, fencedStopTestRequest())
		finished <- err
	}()
	<-f.pty.started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost response error = %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	res, err := clientFencedStop(context.Background(), f.addr, fencedStopTestRequest())
	if err != nil || !res.Accepted || res.Cleanup != string(ports.RuntimeCleanupSucceeded) || f.pty.closeCalls.Load() != 1 {
		t.Fatalf("recovered response = %+v, %v; closes=%d", res, err, f.pty.closeCalls.Load())
	}
}

func TestFencedStopContextShutdownSharesInProgressClose(t *testing.T) {
	release := make(chan struct{})
	closeErr := errors.New("injected concurrent cleanup failure")
	f := startFencedStopFixture(t, true, closeErr, release)
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	finished := make(chan struct{})
	go func() {
		_, _ = clientFencedStop(context.Background(), f.addr, fencedStopTestRequest())
		close(finished)
	}()
	<-f.pty.started
	f.cancel()
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-f.done:
		if !errors.Is(err, closeErr) {
			t.Fatalf("Serve error = %v", err)
		}
		f.done <- err
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	<-finished
	if n := f.pty.closeCalls.Load(); n != 1 {
		t.Fatalf("concurrent shutdown closed PTY %d times", n)
	}
}

func TestServeOrdinaryShutdownReturnsCleanupError(t *testing.T) {
	closeErr := errors.New("ordinary close failed")
	f := startFencedStopFixture(t, false, closeErr, nil)
	f.cancel()
	select {
	case err := <-f.done:
		if !errors.Is(err, closeErr) {
			t.Fatalf("Serve error = %v", err)
		}
		f.done <- err
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not complete")
	}
}

func TestFencedStopOptionsRoundTrip(t *testing.T) {
	opts := HostOptions{StartOnAttach: true, LazySurface: true, RequireFencedStop: true}
	args := ptyHostArgs("session", "/tmp", []string{"sh", "--stop=fenced"}, opts)
	got, positional := splitHostOptionArgs(args[1:])
	if got != opts || !slices.Equal(positional, []string{"session", "/tmp", "sh", "--stop=fenced"}) {
		t.Fatalf("options=%+v positional=%q", got, positional)
	}
	legacy, _ := splitHostOptionArgs(ptyHostArgs("session", "/tmp", []string{"sh"}, HostOptions{})[1:])
	if legacy.RequireFencedStop {
		t.Fatal("ordinary host opted in to fenced stop")
	}
}

func TestRunHostRejectsInvalidFencedGenerationBeforeEffects(t *testing.T) {
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, generation := range []string{"", "  ", strings.Repeat("x", maxFencedStopGenerationBytes+1), "invalid\xff"} {
		t.Setenv(runtimeLaunchIDEnv, generation)
		// chdir itself is an effect and must not occur for this rejected launch.
		code := RunHost([]string{fencedStopArg, "session", t.TempDir(), "unused-command"}, io.Discard)
		if code != 1 {
			t.Fatalf("RunHost = %d", code)
		}
		cwd, err := os.Getwd()
		if err != nil || cwd != oldCWD {
			_ = os.Chdir(oldCWD)
			t.Fatalf("RunHost changed directory: %q, %v", cwd, err)
		}
	}
}

func TestFencedStopEmptyHostGenerationCannotClose(t *testing.T) {
	p := &fencedStopTestPTY{fakePTY: newFakePTY(1002), started: make(chan struct{})}
	h := newHost(ServeConfig{
		SessionID: "session-fenced", RequireFencedStop: true,
		PTY: p, Ring: NewRing(), LazySurface: true,
	})
	t.Cleanup(h.surface.stop)
	t.Cleanup(func() { _ = p.fakePTY.Close() })
	data, _ := json.Marshal(fencedStopTestRequest())
	h.handleFencedStop(nil, data)
	if n := p.closeCalls.Load(); n != 0 {
		t.Fatalf("empty host generation closed PTY %d times", n)
	}
}

func TestFencedStopBoundsCleanupError(t *testing.T) {
	f := startFencedStopFixture(t, true, errors.New(strings.Repeat("x", maxFencedStopErrorBytes+100)), nil)
	res, err := clientFencedStop(context.Background(), f.addr, fencedStopTestRequest())
	if err != nil || !res.Accepted || res.Cleanup != string(ports.RuntimeCleanupFailed) || len(res.Error) > maxFencedStopErrorBytes {
		t.Fatalf("bounded failure receipt=%+v err=%v", res, err)
	}
}
