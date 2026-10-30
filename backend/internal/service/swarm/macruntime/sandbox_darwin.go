//go:build darwin

// Package macruntime supplies an offline diagnostic sandbox configuration for
// AO's pinned direct-PTY runtime. It does not launch processes or grant live
// qualification. sandbox-exec is a private macOS interface: retest each host.
package macruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/conpty"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const sandboxExecutable = "/usr/bin/sandbox-exec"

// SandboxOptions names existing, canonical, disjoint directories. DataDir is
// private writable scratch for either role. Tools are explicit executable files
// outside those directories; their contents and identity are pinned. No home,
// credential, network, or live provider permissions are implicitly granted.
type SandboxOptions struct {
	Workspace, DataDir, Coordinator, Evaluator string
	ReadOnly                                   bool
	Tools                                      []string
}

type pathIdentity struct {
	Path, SHA256  string
	Device, Inode uint64
}

type Sandbox struct {
	options SandboxOptions
	paths   []pathIdentity
	profile string
	binding string
}

// Never allow all of /System: /System/Volumes/Data aliases writable user data.
var systemReads = []string{"/System/Library", "/System/Cryptexes", "/System/Volumes/Preboot/Cryptexes", "/usr/lib", "/bin", "/usr/bin"}

func NewSandbox(ctx context.Context, runtime *conpty.PinnedRuntime, options SandboxOptions) (*Sandbox, error) {
	if runtime == nil {
		return nil, errors.New("sandbox: explicit pinned runtime required")
	}
	return newSandbox(ctx, options, runtime.Binding())
}

func newSandbox(ctx context.Context, options SandboxOptions, runtime conpty.PinnedBinding) (*Sandbox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	options.Tools = slices.Clone(options.Tools)
	if runtime.Backend != "darwin-direct-pty" || runtime.EnvironmentSemanticsVersion != "explicit-full-env-v1" || !filepath.IsAbs(runtime.RegistryPath) {
		return nil, errors.New("sandbox: incompatible runtime binding")
	}
	s := &Sandbox{options: options}
	roots := []string{options.Workspace, options.DataDir, options.Coordinator, options.Evaluator, filepath.Dir(runtime.RegistryPath)}
	for i, root := range roots {
		for _, system := range systemReads {
			if overlaps(root, system) {
				return nil, errors.New("sandbox: diagnostic roots must be separate from system reads")
			}
		}
		for _, other := range roots[:i] {
			if overlaps(root, other) {
				return nil, errors.New("sandbox: roots must be disjoint")
			}
		}
		id, err := identify(root, true)
		if err != nil {
			return nil, err
		}
		s.paths = append(s.paths, id)
	}
	for _, tool := range append([]string{sandboxExecutable, runtime.HostExecutable}, options.Tools...) {
		for _, root := range roots {
			if overlaps(tool, root) {
				return nil, errors.New("sandbox: pinned tools must be outside diagnostic roots")
			}
		}
		id, err := identify(tool, false)
		if err != nil {
			return nil, err
		}
		if tool == runtime.HostExecutable && id.SHA256 != runtime.HostSHA256 {
			return nil, errors.New("sandbox: pinned host bytes changed")
		}
		s.paths = append(s.paths, id)
	}
	if err := s.validateExposedTrees(ctx); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n(allow process-exec process-fork)\n(allow signal (target self))\n(allow sysctl-read)\n(allow file-read-metadata)\n(allow file-read* (literal \"/\"))\n")
	for _, root := range systemReads {
		fmt.Fprintf(&b, "(allow file-read* file-map-executable (subpath %s))\n", strconv.Quote(root))
	}
	b.WriteString("(allow file-read* file-write* (literal \"/dev/null\"))\n")
	for _, tool := range options.Tools {
		fmt.Fprintf(&b, "(allow file-read* file-map-executable (literal %s))\n", strconv.Quote(tool))
	}
	fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", strconv.Quote(options.Workspace))
	if !options.ReadOnly {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", strconv.Quote(options.Workspace))
	}
	fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", strconv.Quote(options.DataDir))
	for _, root := range roots[2:] {
		fmt.Fprintf(&b, "(deny file-read* file-write* (subpath %s))\n", strconv.Quote(root))
	}
	s.profile = b.String()
	s.binding = domain.SwarmHash(struct {
		Version string
		Paths   []pathIdentity
		Profile string
		Runtime conpty.PinnedBinding
	}{"offline-macos-sbpl-v1", s.paths, s.profile, runtime})
	return s, nil
}

// Binding identifies this diagnostic policy, tool bytes and root identities.
// It is not a PermissionVerifier receipt or a process containment claim.
func (s *Sandbox) Binding() string { return s.binding }

// Wrap prepares argv for the existing Runtime.Create path. The profile is an
// immutable argument, not a worker-writable file. Use only with PinnedRuntime's
// explicit-full-env-v1 semantics; ordinary AO runtimes inherit ambient settings.
// Revalidation narrows drift but does not eliminate path/exec TOCTOU or inherited
// descriptors. The diagnostic must test the actual host before any live wiring.
func (s *Sandbox) Wrap(ctx context.Context, cfg ports.RuntimeConfig) (ports.RuntimeConfig, error) {
	if err := ctx.Err(); err != nil {
		return ports.RuntimeConfig{}, err
	}
	for i, previous := range s.paths {
		current, err := identify(previous.Path, i < 5)
		if err != nil || current != previous {
			return ports.RuntimeConfig{}, fmt.Errorf("sandbox: binding changed for %s: %w", previous.Path, errors.Join(err, ports.ErrRuntimeProbeInconclusive))
		}
	}
	if err := s.validateExposedTrees(ctx); err != nil {
		return ports.RuntimeConfig{}, err
	}
	if cfg.WorkspacePath != s.options.Workspace || len(cfg.Argv) == 0 || cfg.Env == nil || !filepath.IsAbs(cfg.Argv[0]) {
		return ports.RuntimeConfig{}, errors.New("sandbox: exact workspace, full environment and absolute command required")
	}
	allowed := slices.Contains(s.options.Tools, cfg.Argv[0])
	for _, root := range []string{"/bin", "/usr/bin"} {
		allowed = allowed || (filepath.Clean(cfg.Argv[0]) == cfg.Argv[0] && filepath.Dir(cfg.Argv[0]) == root)
	}
	if !allowed {
		return ports.RuntimeConfig{}, errors.New("sandbox: command outside explicit diagnostic tools")
	}
	cfg.Argv = append([]string{sandboxExecutable, "-p", s.profile}, cfg.Argv...)
	cfg.Env = maps.Clone(cfg.Env)
	return cfg, nil
}

// Reject pre-existing links into protected inodes. The source copier imposes
// similar rules, but scratch and standalone diagnostic callers need this guard
// too. This is a prelaunch check, not protection from a concurrent trusted host
// actor replacing files after validation.
func (s *Sandbox) validateExposedTrees(ctx context.Context) error {
	for _, root := range []string{s.options.Workspace, s.options.DataDir} {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if cancelErr := ctx.Err(); cancelErr != nil {
				return cancelErr
			}
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
				return fmt.Errorf("sandbox: exposed tree contains a link or special file: %s", path)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func identify(path string, directory bool) (pathIdentity, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsFunc(path, unicode.IsControl) {
		return pathIdentity{}, errors.New("sandbox: clean absolute path without control characters required")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return pathIdentity{}, errors.New("sandbox: existing canonical path required")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return pathIdentity{}, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return pathIdentity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.IsDir() != directory || (!directory && (!info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0)) {
		return pathIdentity{}, errors.New("sandbox: expected directory or regular executable")
	}
	id := pathIdentity{Path: path, Device: uint64(stat.Dev), Inode: stat.Ino}
	if !directory {
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return pathIdentity{}, err
		}
		id.SHA256 = hex.EncodeToString(h.Sum(nil))
	}
	return id, nil
}

func overlaps(a, b string) bool {
	within := func(child, parent string) bool {
		rel, err := filepath.Rel(parent, child)
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../"))) {
			return true
		}
		// Canonical symlink resolution does not normalize case/Unicode aliases
		// on all Mac volumes. Compare physical ancestry as well as spelling.
		parentInfo, err := os.Stat(parent)
		if err != nil {
			return false
		}
		for path := child; ; path = filepath.Dir(path) {
			if info, err := os.Stat(path); err == nil && os.SameFile(info, parentInfo) {
				return true
			}
			if path == filepath.Dir(path) {
				return false
			}
		}
	}
	return within(a, b) || within(b, a)
}
