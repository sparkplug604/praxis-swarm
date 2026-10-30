//go:build !windows

package planning

import (
	"os"
	"syscall"
)

func openToolFile(path string) (*os.File, error) {
	// NONBLOCK handles replacement with a FIFO between Lstat and open;
	// NOFOLLOW prevents a final-component symlink substitution.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
