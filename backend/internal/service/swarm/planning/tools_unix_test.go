//go:build !windows

package planning

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestFileToolVerifierRejectsFIFOWithoutBlocking(t *testing.T) {
	tool := packetTool(t)
	if err := os.Remove(tool.Path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(tool.Path, 0700); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- (FileToolVerifier{}).Verify(context.Background(), []domain.SwarmToolIdentity{tool}) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrStale) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked verification")
	}
}
