//go:build !windows

package qualification

import (
	"fmt"
	"os"
	"syscall"
)

func directoryFileID(_ *os.File, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("filesystem does not expose directory identity")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
