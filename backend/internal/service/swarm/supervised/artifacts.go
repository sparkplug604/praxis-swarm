package supervised

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
)

func gitRead(ctx context.Context, root string, args ...string) ([]byte, error) {
	argv := append([]string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-C", root}, args...)
	cmd := process.CommandContext(ctx, "git", argv...)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !strings.HasPrefix(key, "GIT_") || key == "GIT_CONFIG_GLOBAL" || key == "GIT_CONFIG_SYSTEM" || key == "GIT_CONFIG_NOSYSTEM" {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	return cmd.Output()
}

func cleanSource(ctx context.Context, root, commit string) error {
	head, err := gitRead(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != commit {
		return fmt.Errorf("%w: source HEAD differs from plan", domain.ErrSwarmStale)
	}
	status, err := gitRead(ctx, root, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return fmt.Errorf("%w: supervised source must be clean and committed", domain.ErrSwarmConflict)
	}
	return nil
}

func loadSnapshot(path string) (workspace.Snapshot, error) {
	var snapshot workspace.Snapshot
	raw, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	err = json.Unmarshal(raw, &snapshot)
	return snapshot, err
}

// AO worktree creation may refresh Git metadata. Compare preserved source file
// contents/modes instead of treating benign index timestamps as source edits.
func sourceMatches(path, source string) error {
	snapshot, err := loadSnapshot(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	// Closing a read-only directory handle cannot affect the verified contents.
	defer func() { _ = root.Close() }()
	for _, entry := range snapshot.Source.Entries {
		if entry.Missing || entry.Directory {
			continue
		}
		info, err := root.Lstat(entry.Path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || uint32(info.Mode().Perm()) != entry.Mode || info.Size() != entry.Size {
			return workspace.ErrChanged
		}
		file, err := root.Open(entry.Path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
			return workspace.ErrChanged
		}
	}
	return nil
}

// Git preserves executable bits, not owner/group read/write permissions.
// Retain exact modes in snapshots, but compare candidate scope using Git modes.
func content(entries []workspace.Entry) map[string]string {
	result := make(map[string]string)
	for _, entry := range entries {
		if entry.Missing || entry.Directory {
			continue
		}
		result[entry.Path] = fmt.Sprintf("%t:%d:%s", entry.Mode&0o111 != 0, entry.Size, entry.SHA256)
	}
	return result
}

func validateScope(before, after []workspace.Entry, resources []domain.SwarmResource) error {
	a, b := content(before), content(after)
	paths := make(map[string]bool)
	for path := range a {
		paths[path] = true
	}
	for path := range b {
		paths[path] = true
	}
	for path := range paths {
		if a[path] == b[path] {
			continue
		}
		allowed := false
		for _, resource := range resources {
			if resource.Namespace != "repository" || resource.Kind != "path" {
				continue
			}
			key := strings.TrimSuffix(resource.Key, "/**")
			if key == "." || path == key || strings.HasPrefix(path, key+"/") {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("candidate changed a file outside task scope: %s", path)
		}
	}
	return nil
}

// Register the canonical path before either worker captures. A check against a
// stale Run copy would allow concurrent claims of the same workspace.
func (s *Service) claimWorkspace(ctx context.Context, entry *execution, index int, task *Task) error {
	path, err := s.runtime.Workspace(ctx, task.SessionID)
	if err != nil {
		return err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	run := &entry.run
	artifactRoot, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return err
	}
	if path == artifactRoot || within(path, artifactRoot) || within(artifactRoot, path) {
		return fmt.Errorf("worker workspace overlaps evidence storage")
	}
	if !filepath.IsAbs(path) || path == run.SourcePath || within(run.SourcePath, path) || within(path, run.SourcePath) {
		return fmt.Errorf("worker workspace must be separate from source")
	}
	for _, other := range run.Tasks {
		if other.ID != task.ID && other.WorkspacePath != "" && (other.WorkspacePath == path || within(other.WorkspacePath, path) || within(path, other.WorkspacePath)) {
			return fmt.Errorf("worker workspaces overlap")
		}
	}
	run.Tasks[index].WorkspacePath = path
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if err := s.store.UpdateSupervisedSwarmRun(ctx, run.ID, raw, time.Now().UTC()); err != nil {
		return err
	}
	task.WorkspacePath = path
	return nil
}

func (s *Service) freeze(ctx context.Context, run Run, index int, task *Task) error {
	path := task.WorkspacePath
	if path == "" {
		return fmt.Errorf("worker workspace has no persisted ownership claim")
	}

	dir, err := os.MkdirTemp(filepath.Join(s.root, run.ID), "candidate-"+task.ID+"-")
	if err != nil {
		return err
	}
	snapshot, err := workspace.Capture(ctx, workspace.CaptureRequest{SourceRoot: path, Destination: filepath.Join(dir, "files")})
	if err != nil {
		return err
	}
	task.SnapshotPath = filepath.Join(dir, "snapshot.json")
	if err := saveJSON(task.SnapshotPath, snapshot); err != nil {
		return err
	}
	task.SnapshotHash = snapshot.ManifestHash
	if err := workspace.ValidateForLaunch(ctx, snapshot); err != nil {
		return err
	}
	task.CandidateHash = domain.SwarmHash(content(snapshot.Source.Entries))
	diff, err := gitRead(ctx, path, "diff", "--binary", "--no-ext-diff", "--no-textconv", run.Plan.SourceCommit, "--")
	if err != nil {
		return err
	}
	task.DiffPath = filepath.Join(dir, "changes.patch")
	if err := os.WriteFile(task.DiffPath, diff, 0o600); err != nil {
		return err
	}
	// Recheck after diff generation; retained snapshots contain untracked files
	// too, whereas Git's tracked-file patch does not.
	if err := workspace.ValidateForLaunch(ctx, snapshot); err != nil {
		return err
	}
	baseline, err := loadSnapshot(run.SourceSnapshot)
	if err != nil {
		return err
	}
	return validateScope(baseline.Source.Entries, snapshot.Source.Entries, run.Plan.Tasks[index].Resources)
}
