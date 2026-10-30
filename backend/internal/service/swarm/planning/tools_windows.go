//go:build windows

package planning

import (
	"errors"
	"fmt"
	"os"
)

func openToolFile(_ string) (*os.File, error) {
	// Windows needs handle-based identity checks before executable verification
	// can provide the same protection as the Unix no-follow opener.
	return nil, fmt.Errorf("swarm tool verification on Windows requires handle-based identity checks: %w", errors.ErrUnsupported)
}
