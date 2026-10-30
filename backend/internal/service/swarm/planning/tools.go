package planning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// FileToolVerifier reads bytes only. The version is coordinator-supplied metadata
// bound to the binary hash; no arbitrary --version command is launched here.
// Dispatch must enforce the same identity at use time; this is not a sandbox.
type FileToolVerifier struct{}

// Verify checks canonical executable files against their recorded content hashes.
func (FileToolVerifier) Verify(ctx context.Context, identities []domain.SwarmToolIdentity) error {
	if ctx == nil || len(identities) == 0 || len(identities) > 32 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, tool := range identities {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !domain.SwarmValidID(tool.Name) || !domain.SwarmValidID(tool.Version) || seen[tool.Name] ||
			!filepath.IsAbs(tool.Path) || filepath.Clean(tool.Path) != tool.Path || !domain.SwarmValidHash(tool.SHA256, false) {
			return ErrStale
		}
		seen[tool.Name] = true
		if err := verifyToolFile(ctx, tool); err != nil {
			return err
		}
	}
	return nil
}

func verifyToolFile(ctx context.Context, tool domain.SwarmToolIdentity) error {
	canonical, err := filepath.EvalSymlinks(tool.Path)
	if err != nil || canonical != tool.Path {
		return ErrStale
	}
	info, err := os.Lstat(tool.Path)
	if err != nil || !info.Mode().IsRegular() {
		return ErrStale
	}
	file, err := openToolFile(tool.Path)
	if err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			return err
		}
		return ErrStale
	}
	// The executable is read-only; no buffered writes remain to flush.
	defer func() { _ = file.Close() }()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || !os.SameFile(info, before) || before.Mode().Perm()&0o111 == 0 || before.Size() > 1024*1024*1024 {
		return ErrStale
	}
	hash := sha256.New()
	buf := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := file.Read(buf)
		total += int64(n)
		if total > 1024*1024*1024 {
			return ErrStale
		}
		_, _ = hash.Write(buf[:n])
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ErrStale
		}
	}
	after, err := os.Stat(tool.Path)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || before.Mode() != after.Mode() {
		return ErrStale
	}
	canonical, err = filepath.EvalSymlinks(tool.Path)
	if err != nil || canonical != tool.Path || hex.EncodeToString(hash.Sum(nil)) != tool.SHA256 {
		return ErrStale
	}
	return ctx.Err()
}
