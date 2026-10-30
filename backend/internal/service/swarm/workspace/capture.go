package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Capture creates an exclusive plain-file working copy. Git history/metadata is
// intentionally not placed in the worker directory. No source/index writes, Git
// commits, resets, hardlinks, or model calls occur here. Failed copies remain at
// their exclusive destination for coordinator-controlled inspection/cleanup.
func Capture(ctx context.Context, req CaptureRequest) (Snapshot, error) {
	s := Snapshot{}
	var err error
	s.Source, err = manifest(ctx, req.SourceRoot, req.IncludeIgnored)
	if err != nil {
		return s, err
	}
	s.ManifestHash = domain.SwarmHash(s.Source)
	abs, err := filepath.Abs(req.Destination)
	if err != nil {
		return s, err
	}
	parent, err := identifyRoot(filepath.Dir(abs))
	if err != nil {
		return s, err
	}
	destination := filepath.Join(parent.Path, filepath.Base(abs))
	for _, protected := range []string{s.Source.Root.Path, s.Source.GitDirectory.Path, s.Source.GitCommonDirectory.Path} {
		if overlaps(protected, destination) {
			return s, fmt.Errorf("%w: source or Git metadata overlaps destination", ErrUnsafe)
		}
	}
	p, err := os.OpenRoot(parent.Path)
	if err != nil {
		return s, err
	}
	// Root handles have no buffered file writes; copied files are closed below.
	defer func() { _ = p.Close() }()
	if err := p.Mkdir(filepath.Base(abs), 0o700); err != nil {
		return s, fmt.Errorf("exclusive destination: %w", err)
	}
	s.Destination, err = identifyRoot(abs)
	if err != nil {
		return s, err
	}
	d, err := os.OpenRoot(s.Destination.Path)
	if err != nil {
		return s, err
	}
	defer func() { _ = d.Close() }()
	source, err := os.OpenRoot(s.Source.Root.Path)
	if err != nil {
		return s, err
	}
	defer func() { _ = source.Close() }()
	for _, e := range s.Source.Entries {
		if err := ctx.Err(); err != nil {
			return s, err
		}
		if e.Missing {
			continue
		}
		actual, data, err := readEntry(ctx, source, e.Path)
		if err != nil {
			return s, err
		}
		if actual != e {
			return s, fmt.Errorf("%w: source %s", ErrChanged, e.Path)
		}
		if e.Directory {
			if err := d.Mkdir(e.Path, 0o700); err != nil {
				return s, err
			}
			continue
		}
		f, err := d.OpenFile(e.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(e.Mode))
		if err != nil {
			return s, err
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return s, writeErr
		}
		if closeErr != nil {
			return s, closeErr
		}
		if err := d.Chmod(e.Path, os.FileMode(e.Mode)); err != nil {
			return s, err
		}
	}
	// Set directory permissions only after populating them (a source directory
	// may be read-only). Reverse order preserves access through its ancestors.
	for i := len(s.Source.Entries) - 1; i >= 0; i-- {
		e := s.Source.Entries[i]
		if e.Directory && !e.Missing {
			if err := d.Chmod(e.Path, os.FileMode(e.Mode)); err != nil {
				return s, err
			}
		}
	}
	s.IdentityHash = identityHash(s)
	if err := ValidateForLaunch(ctx, s); err != nil {
		return s, err
	}
	return s, nil
}

func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}

func identityHash(s Snapshot) string { s.IdentityHash = ""; return domain.SwarmHash(s) }

func validateBinding(ctx context.Context, s Snapshot) error {
	if s.ManifestHash != domain.SwarmHash(s.Source) || s.IdentityHash != identityHash(s) {
		return fmt.Errorf("%w: snapshot binding", ErrChanged)
	}
	if err := validateRoot(s.Source.Root); err != nil {
		return err
	}
	if err := validateRoot(s.Source.GitDirectory); err != nil {
		return err
	}
	if err := validateRoot(s.Source.GitCommonDirectory); err != nil {
		return err
	}
	if err := validateRoot(s.Destination); err != nil {
		return err
	}
	now, err := manifest(ctx, s.Source.Root.RequestedPath, s.Source.IncludeIgnored)
	if err != nil {
		return err
	}
	if domain.SwarmHash(now) != s.ManifestHash {
		return fmt.Errorf("%w: source manifest", ErrChanged)
	}
	return nil
}

// ValidateForLaunch rejects changed source/index or destination contents since
// capture. A receipt is only a boundary check, not protection against a later
// actor changing the files; runtime permission enforcement remains separate.
func ValidateForLaunch(ctx context.Context, s Snapshot) error {
	if err := validateBinding(ctx, s); err != nil {
		return err
	}
	r, err := os.OpenRoot(s.Destination.Path)
	if err != nil {
		return err
	}
	// Closing this directory handle cannot flush captured file contents.
	defer func() { _ = r.Close() }()
	actual, err := walk(ctx, r, ".")
	if err != nil {
		return err
	}
	var expected []Entry
	for _, e := range s.Source.Entries {
		if !e.Missing {
			expected = append(expected, contentEntry(e))
		}
	}
	for i := range actual {
		actual[i] = contentEntry(actual[i])
	}
	if domain.SwarmHash(actual) != domain.SwarmHash(expected) {
		return fmt.Errorf("%w: destination contents", ErrChanged)
	}
	return validateBinding(ctx, s)
}

func contentEntry(e Entry) Entry { e.FileID = ""; e.ModifiedNS = 0; e.ChangedNS = 0; return e }

// ValidateSubmission permits candidate changes while rejecting new links,
// special files, moved/replaced roots and source/index drift. It repeats the
// candidate scan to detect changes during collection. The trusted coordinator
// must subsequently freeze the returned candidate before review/integration.
func ValidateSubmission(ctx context.Context, s Snapshot) (Submission, error) {
	result := Submission{}
	if err := validateBinding(ctx, s); err != nil {
		return result, err
	}
	r, err := os.OpenRoot(s.Destination.Path)
	if err != nil {
		return result, err
	}
	// Closing this directory handle cannot flush captured file contents.
	defer func() { _ = r.Close() }()
	entries, err := walk(ctx, r, ".")
	if err != nil {
		return result, err
	}
	again, err := walk(ctx, r, ".")
	if err != nil {
		return result, err
	}
	if domain.SwarmHash(entries) != domain.SwarmHash(again) {
		return result, ErrChanged
	}
	if err := validateBinding(ctx, s); err != nil {
		return result, err
	}
	result = Submission{SnapshotHash: s.IdentityHash, Root: s.Destination, Entries: entries}
	result.Hash = domain.SwarmHash(result)
	return result, nil
}
