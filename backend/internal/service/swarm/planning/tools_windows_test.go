//go:build windows

package planning

import (
	"errors"
	"testing"
)

func TestFileToolOpenerUnsupportedOnWindows(t *testing.T) {
	file, err := openToolFile("unused.exe")
	if file != nil || !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Windows verification must fail closed: file=%v err=%v", file, err)
	}
}
