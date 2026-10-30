package gitworktree

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// createPinnedWorktree deliberately does not use the ordinary branch restore,
// default-ref resolution or stale-registration retry paths. A failed add is
// retained for inspection; it never resumes a branch created by another call.
func (w *Workspace) createPinnedWorktree(ctx context.Context, repo string, cfg ports.WorkspaceConfig) (ports.WorkspaceInfo, error) {
	if !domain.SwarmValidHash(cfg.PinnedSourceCommit, true) || cfg.FreshBranch {
		return ports.WorkspaceInfo{}, errors.New("gitworktree: pinned source must be a full commit ID without speculative branch selection")
	}
	if err := w.validateBranch(ctx, repo, cfg.Branch); err != nil {
		return ports.WorkspaceInfo{}, err
	}
	sha, err := w.revParse(ctx, repo, cfg.PinnedSourceCommit+"^{commit}")
	if err != nil {
		return ports.WorkspaceInfo{}, fmt.Errorf("gitworktree: pinned source commit is unavailable: %w", err)
	}
	if sha != cfg.PinnedSourceCommit {
		return ports.WorkspaceInfo{}, errors.New("gitworktree: pinned source commit is mismatched")
	}
	if exists, err := w.branchNameTaken(ctx, repo, cfg.Branch, true); err != nil {
		return ports.WorkspaceInfo{}, err
	} else if exists {
		return ports.WorkspaceInfo{}, fmt.Errorf("gitworktree: pinned target branch %q already exists", cfg.Branch)
	}
	path, err := w.managedPath(cfg)
	if err != nil {
		return ports.WorkspaceInfo{}, err
	}
	if _, err := os.Lstat(path); err == nil {
		return ports.WorkspaceInfo{}, errors.New("gitworktree: pinned workspace path is present")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ports.WorkspaceInfo{}, fmt.Errorf("gitworktree: pinned workspace path is inaccessible: %w", err)
	}
	records, err := w.listRecords(ctx, repo)
	if err != nil {
		return ports.WorkspaceInfo{}, err
	}
	if _, exists := findWorktree(records, path); exists {
		return ports.WorkspaceInfo{}, errors.New("gitworktree: pinned workspace path is already registered")
	}
	if err := w.enableWorktreeConfig(ctx, repo); err != nil {
		return ports.WorkspaceInfo{}, err
	}
	// Git's -b also rejects a competing branch creation after the check above.
	if _, err := w.run(ctx, w.binary, worktreeAddNewBranchArgs(repo, cfg.Branch, path, sha, false)...); err != nil {
		return ports.WorkspaceInfo{}, fmt.Errorf("gitworktree: add pinned worktree: %w", err)
	}
	info := ports.WorkspaceInfo{Path: path, Branch: cfg.Branch, BaseSHA: sha, BaseRef: sha, SessionID: cfg.SessionID, ProjectID: cfg.ProjectID, RepoPath: repo}
	head, err := w.revParse(ctx, path, "HEAD")
	if err != nil {
		return info, fmt.Errorf("gitworktree: pinned worktree HEAD was not verified: %w", err)
	}
	if head != sha {
		return info, errors.New("gitworktree: pinned worktree HEAD is mismatched")
	}
	return info, nil
}
