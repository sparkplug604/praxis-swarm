package conpty

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func fencedStopReplyServer(t *testing.T, reply func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		var header [5]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			t.Errorf("read request header: %v", err)
			return
		}
		size := binary.BigEndian.Uint32(header[1:])
		if header[0] != MsgFencedStopReq || size > maxFencedStopRequestBytes {
			t.Errorf("invalid request type/size: %x/%d", header[0], size)
			return
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(conn, data); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var req FencedStopRequest
		if err := json.Unmarshal(data, &req); err != nil || req != fencedStopTestRequest() {
			t.Errorf("unexpected request %+v, %v", req, err)
			return
		}
		reply(conn)
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("fenced stop reply server did not finish")
		}
	})
	return ln.Addr().String()
}

func successfulFencedStopResponse() FencedStopResponse {
	return FencedStopResponse{Version: 1, SessionID: "session-fenced", Generation: "launch-original", Accepted: true, Cleanup: string(ports.RuntimeCleanupSucceeded)}
}

func TestClientFencedStopValidatesReceipts(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(*FencedStopResponse)
		valid bool
	}{
		{"accepted success", func(*FencedStopResponse) {}, true},
		{"accepted failed cleanup", func(r *FencedStopResponse) {
			r.Cleanup = string(ports.RuntimeCleanupFailed)
			r.Error = "cleanup failed"
		}, true},
		{"refused", func(r *FencedStopResponse) {
			r.Accepted = false
			r.Cleanup = string(ports.RuntimeCleanupNotAttempted)
			r.Error = "mismatch"
		}, true},
		{"wrong session", func(r *FencedStopResponse) { r.SessionID = "wrong" }, false},
		{"wrong generation", func(r *FencedStopResponse) { r.Generation = "stale" }, false},
		{"empty generation", func(r *FencedStopResponse) { r.Generation = "" }, false},
		{"wrong version", func(r *FencedStopResponse) { r.Version = 2 }, false},
		{"not accepted with success", func(r *FencedStopResponse) { r.Accepted = false }, false},
		{"accepted without cleanup", func(r *FencedStopResponse) { r.Cleanup = string(ports.RuntimeCleanupNotAttempted) }, false},
		{"failed without error", func(r *FencedStopResponse) { r.Cleanup = string(ports.RuntimeCleanupFailed) }, false},
		{"success with error", func(r *FencedStopResponse) { r.Error = "contradiction" }, false},
		{"unknown cleanup", func(r *FencedStopResponse) { r.Cleanup = "probably_stopped" }, false},
		{"oversized error", func(r *FencedStopResponse) {
			r.Cleanup = string(ports.RuntimeCleanupFailed)
			r.Error = strings.Repeat("x", maxFencedStopErrorBytes+1)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := successfulFencedStopResponse()
			tt.edit(&want)
			addr := fencedStopReplyServer(t, func(conn net.Conn) {
				payload, _ := json.Marshal(want)
				frame, _ := EncodeMessage(MsgFencedStopRes, payload)
				// Both the frame header and JSON body can cross read boundaries.
				for _, b := range frame {
					if _, err := conn.Write([]byte{b}); err != nil {
						return
					}
				}
			})
			got, err := clientFencedStop(context.Background(), addr, fencedStopTestRequest())
			if tt.valid {
				if err != nil || got != want {
					t.Fatalf("response = %+v, %v; want %+v", got, err, want)
				}
			} else if err == nil || got != (FencedStopResponse{}) {
				t.Fatalf("invalid receipt accepted = %+v, %v", got, err)
			}
		})
	}
}

func TestClientFencedStopBoundsMalformedAndPrecedingFrames(t *testing.T) {
	validJSON, _ := json.Marshal(successfulFencedStopResponse())
	tests := []struct {
		name     string
		typ      byte
		payload  []byte
		declared uint32
	}{
		{"invalid json", MsgFencedStopRes, []byte("{"), 0},
		{"trailing json", MsgFencedStopRes, append(append([]byte{}, validJSON...), []byte(" {}")...), 0},
		{"unexpected type", MsgGetOutputRes, []byte("output"), 0},
		{"oversized receipt", MsgFencedStopRes, nil, maxFencedStopResponseBytes + 1},
		{"oversized output", MsgTerminalData, nil, maxFencedStopReadBytes + 1},
		{"short receipt", MsgFencedStopRes, []byte("{}"), 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := fencedStopReplyServer(t, func(conn net.Conn) {
				frame, _ := EncodeMessage(tt.typ, tt.payload)
				if tt.declared > 0 {
					binary.BigEndian.PutUint32(frame[1:5], tt.declared)
				}
				_, _ = conn.Write(frame)
			})
			got, err := clientFencedStop(context.Background(), addr, fencedStopTestRequest())
			if err == nil || got != (FencedStopResponse{}) {
				t.Fatalf("invalid response accepted = %+v, %v", got, err)
			}
		})
	}
	t.Run("output before receipt", func(t *testing.T) {
		addr := fencedStopReplyServer(t, func(conn net.Conn) {
			for _, item := range []struct {
				typ  byte
				data []byte
			}{
				{MsgTerminalData, []byte(strings.Repeat("history", 10000))},
				{MsgStatusRes, []byte(`{"alive":false}`)},
				{MsgFencedStopRes, validJSON},
			} {
				frame, _ := EncodeMessage(item.typ, item.data)
				if _, err := conn.Write(frame); err != nil {
					return
				}
			}
		})
		got, err := clientFencedStop(context.Background(), addr, fencedStopTestRequest())
		if err != nil || got != successfulFencedStopResponse() {
			t.Fatalf("receipt after output = %+v, %v", got, err)
		}
	})
}

func TestClientFencedStopPreservesCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			requestRead := make(chan struct{})
			addr := fencedStopReplyServer(t, func(conn net.Conn) {
				close(requestRead)
				_, _ = io.Copy(io.Discard, conn) // until caller cancellation closes it
			})
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
				want = context.DeadlineExceeded
			}
			defer cancel()
			finished := make(chan error, 1)
			go func() { _, err := clientFencedStop(ctx, addr, fencedStopTestRequest()); finished <- err }()
			<-requestRead
			if !deadline {
				cancel()
			}
			select {
			case err := <-finished:
				if !errors.Is(err, want) {
					t.Fatalf("error = %v, want %v", err, want)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation did not interrupt response read")
			}
		})
	}
}

func TestClientFencedStopRejectsBeforeDial(t *testing.T) {
	for _, req := range []FencedStopRequest{
		{}, {1, "session", ""}, {2, "session", "generation"},
		{1, "session", strings.Repeat("x", maxFencedStopGenerationBytes+1)},
	} {
		_, err := clientFencedStop(context.Background(), "not-an-address", req)
		var netErr net.Error
		if err == nil || errors.As(err, &netErr) {
			t.Fatalf("request reached dial: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := clientFencedStop(ctx, "not-an-address", fencedStopTestRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled request error = %v", err)
	}
}

func TestClientFencedStopEOFDoesNotBecomeSuccess(t *testing.T) {
	addr := fencedStopReplyServer(t, func(net.Conn) {})
	if res, err := clientFencedStop(context.Background(), addr, fencedStopTestRequest()); err == nil || res != (FencedStopResponse{}) {
		t.Fatalf("EOF response = %+v, %v", res, err)
	}
}

func TestClientFencedStopOldHostHasBoundedWait(t *testing.T) {
	// An older binary ignores the new request type and never acknowledges it.
	addr := fencedStopReplyServer(t, func(conn net.Conn) { _, _ = io.Copy(io.Discard, conn) })
	started := time.Now()
	res, err := clientFencedStop(context.Background(), addr, fencedStopTestRequest())
	if err == nil || !isTimeout(err) || res != (FencedStopResponse{}) {
		t.Fatalf("old host response = %+v, %v", res, err)
	}
	if elapsed := time.Since(started); elapsed > fencedStopTimeout+time.Second {
		t.Fatalf("unbounded old-host wait: %v", elapsed)
	}
}
