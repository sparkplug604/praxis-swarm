package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func validPath(p string) bool {
	if p == "" || !utf8.ValidString(p) || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsAny(p, "\\\x00\n\r\t") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

func identifyRoot(input string) (RootIdentity, error) {
	abs, err := filepath.Abs(input)
	if err != nil {
		return RootIdentity{}, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return RootIdentity{}, err
	}
	// EvalSymlinks alone does not canonicalize case/Unicode on a Mac volume.
	volume := filepath.VolumeName(real)
	current := volume + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(real, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		info, err := os.Lstat(filepath.Join(current, part))
		if err != nil {
			return RootIdentity{}, err
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return RootIdentity{}, err
		}
		name := ""
		for _, e := range entries {
			ei, eerr := e.Info()
			if os.IsNotExist(eerr) {
				continue // unrelated sibling may disappear during enumeration
			}
			if eerr != nil {
				return RootIdentity{}, eerr
			}
			if os.SameFile(info, ei) {
				if name != "" {
					return RootIdentity{}, fmt.Errorf("%w: ambiguous root alias", ErrUnsafe)
				}
				name = e.Name()
			}
		}
		if name == "" {
			return RootIdentity{}, ErrChanged
		}
		current = filepath.Join(current, name)
	}
	info, err := os.Lstat(current)
	if err != nil {
		return RootIdentity{}, err
	}
	if !info.IsDir() {
		return RootIdentity{}, fmt.Errorf("%w: root is not a directory", ErrUnsafe)
	}
	id, _, _, err := fileIdentity(info)
	return RootIdentity{RequestedPath: abs, Path: current, FileID: id}, err
}

func validateRoot(expected RootIdentity) error {
	actual, err := identifyRoot(expected.RequestedPath)
	if err != nil {
		return fmt.Errorf("%w: root unavailable: %v", ErrChanged, err)
	}
	if actual != expected {
		return fmt.Errorf("%w: root moved or replaced", ErrChanged)
	}
	return nil
}

// resolvePath asks the filesystem which directory entry names the requested
// object. No guessed lowercase or Unicode normalization rules are used.
// Missing suffixes return the closest existing parent for conservative locking.
func resolvePath(root *os.Root, requested string) (string, bool, error) {
	if !validPath(requested) {
		return "", false, fmt.Errorf("%w: path %q", ErrUnsafe, requested)
	}
	if requested == "." {
		return ".", true, nil
	}
	current := "."
	parts := strings.Split(requested, "/")
	for i, part := range parts {
		candidate := path.Join(current, part)
		info, err := root.Lstat(candidate)
		if os.IsNotExist(err) {
			return current, false, nil
		}
		if err != nil {
			return "", false, err
		}
		if err := safeInfo(info); err != nil {
			return "", false, fmt.Errorf("%s: %w", candidate, err)
		}
		parent, err := root.Open(current)
		if err != nil {
			return "", false, err
		}
		entries, err := parent.ReadDir(-1)
		closeErr := parent.Close()
		if err != nil {
			return "", false, err
		}
		if closeErr != nil {
			return "", false, closeErr
		}
		name := ""
		for _, e := range entries {
			ei, err := root.Lstat(path.Join(current, e.Name()))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return "", false, err
			}
			if os.SameFile(info, ei) {
				if name != "" {
					return "", false, fmt.Errorf("%w: hardlink alias", ErrUnsafe)
				}
				name = e.Name()
			}
		}
		if name == "" {
			return "", false, ErrChanged
		}
		current = path.Join(current, name)
		if !validPath(current) {
			return "", false, fmt.Errorf("%w: Git metadata", ErrUnsafe)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return "", false, fmt.Errorf("%w: non-directory ancestor", ErrUnsafe)
		}
	}
	return current, true, nil
}

func safeInfo(info os.FileInfo) error {
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("%w: links and special files are unsupported", ErrUnsafe)
	}
	_, links, _, err := fileIdentity(info)
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() && links != 1 {
		return fmt.Errorf("%w: hardlinked file", ErrUnsafe)
	}
	return nil
}

func entryInfo(p string, info os.FileInfo) (Entry, error) {
	if err := safeInfo(info); err != nil {
		return Entry{}, err
	}
	id, _, changed, err := fileIdentity(info)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Path: p, FileID: id, Mode: uint32(info.Mode().Perm()), Directory: info.IsDir()}
	if !info.IsDir() {
		e.Size = info.Size()
		e.ModifiedNS = info.ModTime().UnixNano()
		e.ChangedNS = changed
	}
	return e, nil
}

func readEntry(ctx context.Context, root *os.Root, p string) (Entry, []byte, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, nil, err
	}
	before, err := root.Lstat(p)
	if err != nil {
		return Entry{}, nil, err
	}
	e, err := entryInfo(p, before)
	if err != nil {
		return Entry{}, nil, err
	}
	if e.Directory {
		return e, nil, nil
	}
	if err := ctx.Err(); err != nil {
		return Entry{}, nil, err
	}
	f, err := openRead(root, p)
	if err != nil {
		return Entry{}, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return Entry{}, nil, err
	}
	if !os.SameFile(before, opened) {
		return Entry{}, nil, ErrChanged
	}
	if err := safeInfo(opened); err != nil {
		return Entry{}, nil, err
	}
	data, err := io.ReadAll(&contextReader{ctx: ctx, reader: f})
	if err != nil {
		return Entry{}, nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return Entry{}, nil, err
	}
	final, err := root.Lstat(p)
	if err != nil {
		return Entry{}, nil, err
	}
	post, err := entryInfo(p, after)
	if err != nil {
		return Entry{}, nil, err
	}
	last, err := entryInfo(p, final)
	if err != nil {
		return Entry{}, nil, err
	}
	if e != post || e != last || int64(len(data)) != e.Size {
		return Entry{}, nil, ErrChanged
	}
	e.SHA256 = digest(data)
	return e, data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func walk(ctx context.Context, root *os.Root, start string) ([]Entry, error) {
	var entries []Entry
	err := fs.WalkDir(root.FS(), start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if !validPath(p) {
			return fmt.Errorf("%w: invalid tree path %q", ErrUnsafe, p)
		}
		e, _, err := readEntry(ctx, root, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		entries = append(entries, e)
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, err
}
