package conpty

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	fencedStopTimeout = 3 * time.Second
	// Scrollback and output may precede the receipt. Discard them without
	// allocating their declared size, within both a byte and a time budget.
	maxFencedStopReadBytes = 16 * 1024 * 1024
)

// clientFencedStop sends one request and requires one matching, valid receipt.
// It never retries, sends a legacy kill or treats a refused connection as proof
// of cleanup. A transport failure after writing leaves the effect uncertain.
// Valid rejection and failed-cleanup receipts are returned for the caller to
// classify; the error return describes transport, context or protocol failures.
func clientFencedStop(ctx context.Context, addr string, req FencedStopRequest) (FencedStopResponse, error) {
	var zero FencedStopResponse
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !validFencedStopRequest(req) {
		return zero, errors.New("invalid fenced stop version or identity")
	}
	payload, err := json.Marshal(req)
	if err != nil || len(payload) > maxFencedStopRequestBytes {
		return zero, errors.New("fenced stop request is too large")
	}
	conn, err := dialHostContext(ctx, addr, dialTimeout)
	if err != nil {
		return zero, fencedStopIOError(ctx, "dial", err)
	}
	defer func() { _ = conn.Close() }()
	stopCancellation := armClientDeadline(ctx, conn, fencedStopTimeout)
	defer stopCancellation()
	frame, _ := EncodeMessage(MsgFencedStopReq, payload)
	if n, err := conn.Write(frame); err != nil {
		return zero, fencedStopIOError(ctx, "write request", err)
	} else if n != len(frame) {
		return zero, fencedStopIOError(ctx, "write request", io.ErrShortWrite)
	}
	remaining := int64(maxFencedStopReadBytes)
	for remaining >= 5 {
		var header [5]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return zero, fencedStopIOError(ctx, "read response header", err)
		}
		remaining -= 5
		size := int64(binary.BigEndian.Uint32(header[1:]))
		if size > remaining {
			return zero, errors.New("fenced stop response exceeds read budget")
		}
		remaining -= size
		if header[0] != MsgFencedStopRes {
			if header[0] != MsgTerminalData && header[0] != MsgStatusRes {
				return zero, fmt.Errorf("unexpected fenced stop response type %#x", header[0])
			}
			if _, err := io.CopyN(io.Discard, conn, size); err != nil {
				return zero, fencedStopIOError(ctx, "read preceding output", err)
			}
			continue
		}
		if size > maxFencedStopResponseBytes {
			return zero, errors.New("fenced stop receipt is too large")
		}
		data := make([]byte, int(size))
		if _, err := io.ReadFull(conn, data); err != nil {
			return zero, fencedStopIOError(ctx, "read receipt", err)
		}
		var res FencedStopResponse
		if err := decodeFencedStopJSON(data, maxFencedStopResponseBytes, &res); err != nil {
			return zero, fmt.Errorf("decode fenced stop receipt: %w", err)
		}
		if res.Version != FencedStopProtocolVersion || res.SessionID != req.SessionID || res.Generation != req.Generation {
			return zero, errors.New("fenced stop receipt identity or version mismatch")
		}
		if len(res.Error) > maxFencedStopErrorBytes {
			return zero, errors.New("fenced stop receipt error is too large")
		}
		validReceipt := (!res.Accepted && res.Cleanup == string(ports.RuntimeCleanupNotAttempted) && res.Error != "") ||
			(res.Accepted && ((res.Cleanup == string(ports.RuntimeCleanupSucceeded) && res.Error == "") ||
				(res.Cleanup == string(ports.RuntimeCleanupFailed) && res.Error != "")))
		if !validReceipt {
			return zero, errors.New("inconsistent fenced stop receipt")
		}
		return res, nil
	}
	return zero, errors.New("fenced stop response exhausted read budget")
}

func fencedStopIOError(ctx context.Context, operation string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	// The socket deadline may fire just before the context timer publishes Err.
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return fmt.Errorf("fenced stop %s: %w", operation, err)
}
