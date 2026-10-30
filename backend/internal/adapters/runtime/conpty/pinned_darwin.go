//go:build darwin

package conpty

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/conpty/ptyregistry"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// PinnedOptions binds a disposable Mac diagnostic process to one direct PTY
// host and one existing registry directory. It is not a confinement policy.
type PinnedOptions struct {
	RunFilePath    string
	HostExecutable string
	HostSHA256     string
}

// PinnedBinding describes the launch implementation, not a qualification claim.
// Every field is a value; changing the returned binding cannot change a runtime.
type PinnedBinding struct {
	Backend                     string
	HostExecutable              string
	HostSHA256                  string
	RegistryPath                string
	EnvironmentSemanticsVersion string
	StopProtocolVersion         int
}

// PinnedRuntime is a bounded direct-PTY diagnostic binding. It never reports
// fenced death: the host's child status alone cannot prove descendant absence.
// One instance is permitted per process because the existing registry is global.
type PinnedRuntime struct {
	direct      *Runtime
	binding     PinnedBinding
	hostInfo    os.FileInfo
	registryDir os.FileInfo
	ownedMu     sync.Mutex
	owned       map[string]pinnedOwnedHost
}

type pinnedOwnedHost struct {
	process   *os.Process
	addr      string
	launchID  string
	destroyed bool
}

var pinnedRuntimeClaim struct {
	sync.Mutex
	claimed bool
}

var _ ports.Runtime = (*PinnedRuntime)(nil)
var _ ports.FencedRuntimeProber = (*PinnedRuntime)(nil)
var _ ports.FencedRuntimeStopper = (*PinnedRuntime)(nil)

// NewPinnedRuntime validates all binding inputs before selecting the registry.
// It must run before concurrent runtime activity, in a disposable process with
// an explicitly scoped registry. Ordinary Runtime construction is not permitted
// alongside it; subsequent scope drift is rejected before launches or cleanup.
func NewPinnedRuntime(ctx context.Context, opts PinnedOptions) (*PinnedRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.RunFilePath == "" || !filepath.IsAbs(opts.RunFilePath) || filepath.Clean(opts.RunFilePath) != opts.RunFilePath {
		return nil, errors.New("pinned conpty: explicit clean absolute run file path required")
	}
	dir := filepath.Dir(opts.RunFilePath)
	dirInfo, err := canonicalPinnedPath(dir, true)
	if err != nil {
		return nil, fmt.Errorf("pinned conpty: registry parent: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("pinned conpty: resolve home: %w", err)
	}
	// No diagnostic constructor may select the user's ordinary AO state.
	if realHome, resolveErr := filepath.EvalSymlinks(home); resolveErr == nil {
		home = realHome
	}
	if err := excludePinnedGlobalDir(dir, home); err != nil {
		return nil, err
	}
	hostInfo, err := canonicalPinnedPath(opts.HostExecutable, false)
	if err != nil {
		return nil, fmt.Errorf("pinned conpty: host: %w", err)
	}
	if hostInfo.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("pinned conpty: host must be executable")
	}
	digest, decodeErr := hex.DecodeString(opts.HostSHA256)
	if decodeErr != nil || len(digest) != sha256.Size || opts.HostSHA256 != strings.ToLower(opts.HostSHA256) {
		return nil, errors.New("pinned conpty: lowercase SHA-256 host digest required")
	}
	r := &PinnedRuntime{
		binding: PinnedBinding{
			Backend: "darwin-direct-pty", HostExecutable: opts.HostExecutable,
			HostSHA256: opts.HostSHA256, RegistryPath: filepath.Join(dir, "windows-pty-hosts.json"),
			EnvironmentSemanticsVersion: "explicit-full-env-v1",
			StopProtocolVersion:         FencedStopProtocolVersion,
		},
		hostInfo: hostInfo, registryDir: dirInfo, owned: make(map[string]pinnedOwnedHost),
	}
	if err := r.validateHost(ctx); err != nil {
		return nil, err
	}
	if err := r.validateRegistryPath(); err != nil {
		return nil, err
	}
	pinnedRuntimeClaim.Lock()
	defer pinnedRuntimeClaim.Unlock()
	if pinnedRuntimeClaim.claimed {
		return nil, errors.New("pinned conpty: only one runtime binding is allowed per process")
	}
	r.direct = New(Options{RunFilePath: opts.RunFilePath, Spawner: func(ctx context.Context, sessionID, cwd string, argv []string, env map[string]string, options HostOptions) (string, int, error) {
		// Validate again immediately before starting the bound host. The same
		// shared Unix launcher owns readiness, detachment and error cleanup.
		if err := r.validate(ctx); err != nil {
			return "", 0, err
		}
		options.RequireFencedStop = true
		addr, pid, err := spawnUnixHost(ctx, r.binding.HostExecutable, sessionID, cwd, argv, explicitUnixEnvironment(env), options, true, func(process *os.Process) {
			r.ownedMu.Lock()
			r.owned[sessionID] = pinnedOwnedHost{process: process, launchID: env[runtimeLaunchIDEnv]}
			r.ownedMu.Unlock()
		})
		r.ownedMu.Lock()
		if owned, ok := r.owned[sessionID]; ok {
			owned.addr = addr
			r.owned[sessionID] = owned
		}
		r.ownedMu.Unlock()
		return addr, pid, err
	}})
	// Native Close may spend two half-second graces reaping its process group.
	r.direct.destroyWait = 2 * time.Second
	r.direct.registerHost = func(ctx context.Context, entry ptyregistry.Entry) error {
		entry.RetainUntilUnregistered = true
		return ptyregistry.Register(ctx, entry)
	}
	// Retain the original os.Process objects. Their signal/Wait synchronization
	// refuses already-reaped processes rather than signalling a reused PID.
	r.direct.pidIsAlive = func(pid int) bool {
		r.ownedMu.Lock()
		defer r.ownedMu.Unlock()
		for _, owned := range r.owned {
			if owned.process.Pid == pid && pinnedProcessAlive(owned.process) {
				return true
			}
		}
		return false
	}
	r.direct.processFinder = func(pid int) (processKiller, error) {
		process := r.ownedProcess(pid)
		if process == nil || !pinnedProcessAlive(process) {
			return nil, fmt.Errorf("pinned conpty: process ownership unavailable: %w", ports.ErrRuntimeProbeInconclusive)
		}
		return process, nil
	}
	r.direct.killHost = r.killOwnedHost
	pinnedRuntimeClaim.claimed = true
	return r, nil
}

// Binding returns the immutable launch identity by value.
func (r *PinnedRuntime) Binding() PinnedBinding { return r.binding }

// Create accepts a complete explicit environment and direct absolute argv only.
// It adds no terminal defaults and never inherits ambient host variables.
func (r *PinnedRuntime) Create(ctx context.Context, cfg ports.RuntimeConfig) (ports.RuntimeHandle, error) {
	if err := r.validate(ctx); err != nil {
		return ports.RuntimeHandle{}, conptyCreateFailure(err)
	}
	if cfg.Env == nil || !validFencedStopRequest(FencedStopRequest{Version: FencedStopProtocolVersion, SessionID: string(cfg.SessionID), Generation: cfg.Env[runtimeLaunchIDEnv]}) {
		return ports.RuntimeHandle{}, conptyCreateFailure(errors.New("pinned conpty: full explicit environment and AO_RUNTIME_LAUNCH_ID required"))
	}
	for key, value := range cfg.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			return ports.RuntimeHandle{}, conptyCreateFailure(errors.New("pinned conpty: invalid explicit environment entry"))
		}
		if strings.HasPrefix(key, "DYLD_") || strings.HasPrefix(key, "LD_") {
			return ports.RuntimeHandle{}, conptyCreateFailure(errors.New("pinned conpty: loader override environment variables are forbidden before the host starts"))
		}
	}
	if len(cfg.Argv) == 0 || !filepath.IsAbs(cfg.Argv[0]) || filepath.Base(cfg.Argv[0]) == "env" {
		return ports.RuntimeHandle{}, conptyCreateFailure(errors.New("pinned conpty: absolute command without env prefix required"))
	}
	if cfg.StartOnAttach || !cfg.ExitOnCommandCompletion {
		return ports.RuntimeHandle{}, conptyCreateFailure(errors.New("pinned conpty: immediate start and exit-on-command-completion required"))
	}
	if !filepath.IsAbs(cfg.WorkspacePath) {
		return ports.RuntimeHandle{}, conptyCreateFailure(errors.New("pinned conpty: absolute workspace path required"))
	}
	r.ownedMu.Lock()
	_, startedBefore := r.owned[string(cfg.SessionID)]
	r.ownedMu.Unlock()
	if startedBefore {
		return ports.RuntimeHandle{}, conptyCreateFailure(errors.New("pinned conpty: a started session ID cannot be reused within this diagnostic process"))
	}
	cfg.Env = maps.Clone(cfg.Env)
	cfg.Argv = slices.Clone(cfg.Argv)
	return r.direct.Create(ctx, cfg)
}

// Destroy retains the ordinary cleanup mechanism only for a currently owned
// unreaped host. Recovered, reused and failed-start stale handles remain unknown.
// Successful host cleanup is not descendant evidence.
func (r *PinnedRuntime) Destroy(ctx context.Context, handle ports.RuntimeHandle) error {
	if err := r.validateRegistry(); err != nil {
		return errors.Join(ports.ErrRuntimeProbeInconclusive, err)
	}
	r.ownedMu.Lock()
	owned, ok := r.owned[handle.ID]
	r.ownedMu.Unlock()
	r.direct.mu.Lock()
	session := r.direct.sessions[handle.ID]
	current := session != nil && session.currentOwner && owned.process != nil && session.pid == owned.process.Pid && session.launchID == owned.launchID
	r.direct.mu.Unlock()
	if !ok || owned.destroyed || !current || !pinnedProcessAlive(owned.process) {
		return fmt.Errorf("pinned conpty: recovered or stale handle cannot be safely destroyed: %w", ports.ErrRuntimeProbeInconclusive)
	}
	if err := r.direct.Destroy(ctx, handle); err != nil {
		return err
	}
	r.ownedMu.Lock()
	owned.destroyed = true
	r.owned[handle.ID] = owned
	r.ownedMu.Unlock()
	return nil
}

func (r *PinnedRuntime) ownedProcess(pid int) *os.Process {
	r.ownedMu.Lock()
	defer r.ownedMu.Unlock()
	var process *os.Process
	for _, owned := range r.owned {
		if owned.process.Pid == pid {
			if process != nil {
				return nil // a numeric PID shared across launch lifetimes is ambiguous
			}
			process = owned.process
		}
	}
	return process
}

func pinnedProcessAlive(process *os.Process) bool {
	if process == nil {
		return false
	}
	err := process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (r *PinnedRuntime) killOwnedHost(addr string) error {
	r.ownedMu.Lock()
	var process *os.Process
	for _, owned := range r.owned {
		if owned.addr == addr {
			if process != nil {
				r.ownedMu.Unlock()
				return ports.ErrRuntimeProbeInconclusive
			}
			process = owned.process
		}
	}
	r.ownedMu.Unlock()
	if !pinnedProcessAlive(process) {
		return ports.ErrRuntimeProbeInconclusive
	}
	// This is disposal of an original owned diagnostic host, never the product
	// fenced-stop path. Opted-in hosts reject legacy kill messages; signalling
	// the retained process instead enters RunHost's normal graceful shutdown.
	// os.Process refuses a reaped object instead of looking up its numeric PID.
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("pinned conpty: signal owned diagnostic host: %w", errors.Join(ports.ErrRuntimeProbeInconclusive, err))
	}
	return nil
}

// GetOutput reads diagnostic output only while the pinned registry remains valid.
func (r *PinnedRuntime) GetOutput(ctx context.Context, handle ports.RuntimeHandle, lines int) (string, error) {
	if err := r.validateRegistry(); err != nil {
		return "", err
	}
	return r.direct.GetOutput(ctx, handle, lines)
}

// IsAlive reports a live host without treating host absence as confirmed runtime absence.
func (r *PinnedRuntime) IsAlive(ctx context.Context, handle ports.RuntimeHandle) (bool, error) {
	if err := r.validateRegistry(); err != nil {
		return false, errors.Join(ports.ErrRuntimeProbeInconclusive, err)
	}
	alive, err := r.direct.IsAlive(ctx, handle)
	if !alive {
		return false, errors.Join(ports.ErrRuntimeProbeInconclusive, err)
	}
	return alive, err
}

// ProbeFencedRuntime deliberately withholds dead until a separate descendant
// containment implementation is qualified. No option can bypass this boundary.
func (r *PinnedRuntime) ProbeFencedRuntime(ctx context.Context, ref ports.FencedRuntimeRef) ports.FencedProbeResult {
	unknown := ports.FencedProbeResult{Liveness: ports.FencedUnknown, Reason: ports.FencedReasonProbeFailed}
	if err := r.validateRegistry(); err != nil {
		return unknown
	}
	result := r.direct.ProbeFencedRuntime(ctx, ref)
	if result.Liveness == ports.FencedDead {
		return unknown
	}
	return result
}

func (r *PinnedRuntime) validate(ctx context.Context) error {
	if err := r.validateRegistry(); err != nil {
		return err
	}
	return r.validateHost(ctx)
}

func (r *PinnedRuntime) validateRegistry() error {
	path, err := ptyregistry.Path()
	if err != nil || path != r.binding.RegistryPath {
		return fmt.Errorf("pinned conpty: registry binding changed: %w", errors.Join(err, ports.ErrRuntimeProbeInconclusive))
	}
	return r.validateRegistryPath()
}

func (r *PinnedRuntime) validateRegistryPath() error {
	dirInfo, err := canonicalPinnedPath(filepath.Dir(r.binding.RegistryPath), true)
	if err != nil || !os.SameFile(r.registryDir, dirInfo) {
		return fmt.Errorf("pinned conpty: registry directory replaced: %w", errors.Join(err, ports.ErrRuntimeProbeInconclusive))
	}
	info, err := os.Lstat(r.binding.RegistryPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pinned conpty: inspect registry: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("pinned conpty: registry must be a regular file, never a symlink")
	}
	return nil
}

func (r *PinnedRuntime) validateHost(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := canonicalPinnedPath(r.binding.HostExecutable, false)
	if err != nil || !os.SameFile(r.hostInfo, info) || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("pinned conpty: host path replaced or unavailable: %w", errors.Join(err, ports.ErrRuntimeProbeInconclusive))
	}
	f, err := os.Open(r.binding.HostExecutable)
	if err != nil {
		return fmt.Errorf("pinned conpty: read host: %w", err)
	}
	// This handle only reads executable bytes; it has no buffered writes.
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return fmt.Errorf("pinned conpty: hash host: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != r.binding.HostSHA256 {
		return errors.New("pinned conpty: host SHA-256 changed")
	}
	return nil
}

func canonicalPinnedPath(path string, directory bool) (os.FileInfo, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("clean absolute canonical path required")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if canonical != path {
		return nil, errors.New("symlink or noncanonical path refused")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return nil, errors.New("unexpected filesystem object type")
	}
	return info, nil
}

// Compare physical ancestors: macOS case aliases and Data-volume firmlinks
// can name the same directory while EvalSymlinks preserves their spellings.
func excludePinnedGlobalDir(dir, home string) error {
	globalDir := filepath.Join(home, ".ao")
	globalInfo, err := os.Stat(globalDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil // the required existing registry parent cannot be inside it
	}
	if err != nil {
		return fmt.Errorf("pinned conpty: inspect global AO directory: %w", err)
	}
	for current := dir; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return fmt.Errorf("pinned conpty: inspect registry ancestry: %w", err)
		}
		if os.SameFile(globalInfo, info) {
			return errors.New("pinned conpty: registry must be outside the user's .ao directory")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

// explicitUnixEnvironment serializes exactly the supplied environment. An empty
// non-nil slice is intentional: exec.Cmd treats nil as ambient inheritance.
func explicitUnixEnvironment(env map[string]string) []string {
	entries := make([]string, 0, len(env))
	for key, value := range env {
		entries = append(entries, key+"="+value)
	}
	slices.Sort(entries)
	return entries
}
