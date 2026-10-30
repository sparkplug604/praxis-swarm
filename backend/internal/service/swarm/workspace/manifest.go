package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	argv := append([]string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		// Preserve configured ignore semantics, including global excludes. Drop
		// Git routing/index/object environment overrides so reads stay bound to
		// the requested repository. Config cannot re-enable fsmonitor because
		// the explicit command-line override above has higher precedence.
		if !strings.HasPrefix(key, "GIT_") || key == "GIT_CONFIG_GLOBAL" || key == "GIT_CONFIG_SYSTEM" || key == "GIT_CONFIG_NOSYSTEM" {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read Git state: %w", err)
	}
	return out, nil
}

func requireRepositoryRoot(ctx context.Context, root RootIdentity) error {
	top, err := git(ctx, root.Path, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	actual, err := identifyRoot(strings.TrimSuffix(string(top), "\n"))
	if err != nil {
		return err
	}
	if actual.Path != root.Path || actual.FileID != root.FileID {
		return fmt.Errorf("%w: source must be repository root", ErrUnsafe)
	}
	return nil
}

func manifest(ctx context.Context, source string, ignored []string) (Manifest, error) {
	m := Manifest{}
	var err error
	m.Root, err = identifyRoot(source)
	if err != nil {
		return m, err
	}
	r, err := os.OpenRoot(m.Root.Path)
	if err != nil {
		return m, err
	}
	defer r.Close()
	if err := requireRepositoryRoot(ctx, m.Root); err != nil {
		return m, err
	}
	gitDir, err := git(ctx, m.Root.Path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return m, err
	}
	m.GitDirectory, err = identifyRoot(strings.TrimSuffix(string(gitDir), "\n"))
	if err != nil {
		return m, err
	}
	common, err := git(ctx, m.Root.Path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return m, err
	}
	m.GitCommonDirectory, err = identifyRoot(strings.TrimSuffix(string(common), "\n"))
	if err != nil {
		return m, err
	}
	shared, err := git(ctx, m.Root.Path, "rev-parse", "--shared-index-path")
	if err != nil {
		return m, err
	}
	if len(strings.TrimSpace(string(shared))) != 0 {
		return m, fmt.Errorf("%w: split index requires additional recovery inputs", ErrUnsupported)
	}
	head, headErr := git(ctx, m.Root.Path, "rev-parse", "--verify", "HEAD")
	if headErr == nil {
		m.Head = strings.TrimSpace(string(head))
	}
	ref, refErr := git(ctx, m.Root.Path, "symbolic-ref", "-q", "HEAD")
	if refErr == nil {
		m.HeadRef = strings.TrimSpace(string(ref))
	} else if headErr != nil {
		return m, headErr
	}
	indexPath, err := git(ctx, m.Root.Path, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return m, err
	}
	m.IndexPath = strings.TrimSuffix(string(indexPath), "\n")
	indexParent, err := os.OpenRoot(filepath.Dir(m.IndexPath))
	if err != nil {
		return m, err
	}
	m.Index, m.IndexBytes, err = readEntry(ctx, indexParent, filepath.Base(m.IndexPath))
	_ = indexParent.Close()
	if os.IsNotExist(err) {
		m.Index = Entry{Path: filepath.Base(m.IndexPath), Missing: true}
		err = nil
	}
	if err != nil {
		return m, err
	}
	stages, err := git(ctx, m.Root.Path, "ls-files", "--cached", "--stage", "-z")
	if err != nil {
		return m, err
	}
	selected := map[string]bool{}
	for _, record := range strings.Split(string(stages), "\x00") {
		if record == "" {
			continue
		}
		header, p, ok := strings.Cut(record, "\t")
		parts := strings.Split(header, " ")
		if !ok || len(parts) != 3 || !validPath(p) {
			return m, fmt.Errorf("%w: index entry", ErrUnsafe)
		}
		if parts[0] == "160000" {
			return m, fmt.Errorf("%w: submodule", ErrUnsupported)
		}
		m.IndexEntries = append(m.IndexEntries, IndexEntry{Path: p, Mode: parts[0], Object: parts[1], Stage: parts[2]})
		selected[p] = true
	}
	untracked, err := git(ctx, m.Root.Path, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return m, err
	}
	for _, p := range strings.Split(string(untracked), "\x00") {
		if p != "" {
			if !validPath(p) {
				return m, fmt.Errorf("%w: untracked path", ErrUnsafe)
			}
			selected[p] = true
		}
	}
	m.IncludeIgnored = append([]string(nil), ignored...)
	sort.Strings(m.IncludeIgnored)
	for i, p := range m.IncludeIgnored {
		if !validPath(p) || p == "." || (i > 0 && p == m.IncludeIgnored[i-1]) {
			return m, fmt.Errorf("%w: ignored input selection", ErrUnsafe)
		}
		actual, exists, err := resolvePath(r, p)
		if err != nil {
			return m, err
		}
		if !exists {
			return m, fmt.Errorf("%w: missing requested input", ErrUnsafe)
		}
		info, err := r.Lstat(actual)
		if err != nil {
			return m, err
		}
		if info.IsDir() {
			entries, err := walk(ctx, r, actual)
			if err != nil {
				return m, err
			}
			for _, e := range entries {
				selected[e.Path] = true
			}
		} else {
			selected[actual] = true
		}
	}
	paths := make([]string, 0, len(selected))
	for p := range selected {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	entries := map[string]Entry{}
	aliases := map[string]string{}
	for _, p := range paths {
		actual, exists, err := resolvePath(r, p)
		if err != nil {
			return m, err
		}
		if !exists {
			entries[p] = Entry{Path: p, Missing: true}
			continue
		}
		if prior, ok := aliases[actual]; ok && prior != p {
			return m, fmt.Errorf("%w: index paths alias one filesystem entry", ErrUnsafe)
		}
		aliases[actual] = p
		e, _, err := readEntry(ctx, r, actual)
		if err != nil {
			return m, err
		}
		entries[actual] = e
		for parent := path.Dir(actual); parent != "."; parent = path.Dir(parent) {
			if _, ok := entries[parent]; ok {
				continue
			}
			e, _, err := readEntry(ctx, r, parent)
			if err != nil {
				return m, err
			}
			entries[parent] = e
		}
	}
	for _, e := range entries {
		m.Entries = append(m.Entries, e)
	}
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	if err := validateRoot(m.Root); err != nil {
		return m, err
	}
	return m, nil
}
