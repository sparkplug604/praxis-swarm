//go:build windows

package workspace

import "os"

func openRead(*os.Root, string) (*os.File, error) { return nil, ErrUnsupported }

// Windows qualification needs a handle-based implementation and real-volume
// alias tests. Failing closed is preferable to silently dropping inode checks.
func fileIdentity(os.FileInfo) (string, uint64, int64, error) {
	return "", 0, 0, ErrUnsupported
}
