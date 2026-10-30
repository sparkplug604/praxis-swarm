package conpty

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	maxFencedStopRequestBytes    = 4 * 1024
	maxFencedStopResponseBytes   = 16 * 1024
	maxFencedStopErrorBytes      = 1024
	maxFencedStopGenerationBytes = 256
	maxFencedStopSessionBytes    = 256
)

// closePTY serializes only PTY disposal, never the host's general mutex. The
// first result remains authoritative for all concurrent requests and shutdown;
// retrying Close could both repeat effects and erase an earlier cleanup error.
func (h *host) closePTY() error {
	h.ptyCloseOnce.Do(func() {
		h.ptyCloseErr = h.cfg.PTY.Close()
		if h.ptyCloseErr != nil {
			h.ptyCloseErrorText = strings.ToValidUTF8(h.ptyCloseErr.Error(), "?")
			if h.ptyCloseErrorText == "" {
				h.ptyCloseErrorText = "PTY close failed"
			}
			if len(h.ptyCloseErrorText) > maxFencedStopErrorBytes {
				h.ptyCloseErrorText = strings.ToValidUTF8(h.ptyCloseErrorText[:maxFencedStopErrorBytes-3], "?") + "..."
			}
		}
	})
	return h.ptyCloseErr
}

func (h *host) handleFencedStop(conn net.Conn, payload []byte) {
	var req FencedStopRequest
	err := decodeFencedStopJSON(payload, maxFencedStopRequestBytes, &req)
	if err != nil {
		req = FencedStopRequest{}
	}
	res := FencedStopResponse{
		Version: FencedStopProtocolVersion, SessionID: req.SessionID,
		Generation: req.Generation, Cleanup: string(ports.RuntimeCleanupNotAttempted),
	}
	switch {
	case err != nil:
		res.Error = "invalid fenced stop request"
	case !h.cfg.RequireFencedStop:
		res.Error = "host does not require fenced stop"
	case !validFencedStopRequest(req):
		res.Error = "unsupported version or invalid fenced stop identity"
	case h.cfg.SessionID == "" || h.cfg.LaunchGeneration == "" ||
		req.SessionID != h.cfg.SessionID || req.Generation != h.cfg.LaunchGeneration:
		res.Error = "fenced stop identity mismatch"
	default:
		// The immutable host identity is checked here, at the close boundary.
		// No registry probe or client-side precheck can authorize this effect.
		res.Accepted = true
		res.Cleanup = string(ports.RuntimeCleanupSucceeded)
		if closeErr := h.closePTY(); closeErr != nil {
			res.Cleanup = string(ports.RuntimeCleanupFailed)
			res.Error = h.ptyCloseErrorText
		}
	}
	data, _ := json.Marshal(res)
	frame, _ := EncodeMessage(MsgFencedStopRes, data)
	// Retain listener and receipt after stopping. If this client disappears or
	// its queue overflows, a later exact request can recover the same result.
	h.sendTo(conn, frame)
}

func validFencedStopRequest(req FencedStopRequest) bool {
	return req.Version == FencedStopProtocolVersion &&
		strings.TrimSpace(req.SessionID) != "" && strings.TrimSpace(req.Generation) != "" &&
		len(req.Generation) <= maxFencedStopGenerationBytes && len(req.SessionID) <= maxFencedStopSessionBytes &&
		utf8.ValidString(req.Generation) && utf8.ValidString(req.SessionID)
}

func decodeFencedStopJSON(payload []byte, limit int, dst any) error {
	if len(payload) > limit {
		return fmt.Errorf("fenced stop payload exceeds %d bytes", limit)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("fenced stop payload contains trailing data")
	}
	return nil
}
