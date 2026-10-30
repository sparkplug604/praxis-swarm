//go:build !windows

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type openSwapContext struct {
	context.Context
	calls int
	swap  func() error
	err   error
}

func (c *openSwapContext) Err() error {
	c.calls++
	if c.calls == 2 {
		c.err = c.swap()
	}
	if c.err != nil {
		return c.err
	}
	return c.Context.Err()
}

func TestExchangedFIFOOpenDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	put(t, root, "input", "regular file")
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := &openSwapContext{Context: context.Background(), swap: func() error {
		if err := os.Remove(filepath.Join(root, "input")); err != nil {
			return err
		}
		return syscall.Mkfifo(filepath.Join(root, "input"), 0600)
	}}
	done := make(chan error, 1)
	go func() { _, _, err := readEntry(ctx, r, "input"); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO exchange accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("file exchanged with FIFO blocked open")
	}
}
