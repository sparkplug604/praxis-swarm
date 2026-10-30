package gitworktree

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestPinnedWorkspaceUsesCommitAndRejectsReuse(t *testing.T) {
	git := requireGit(t)
	repo := setupOriginClone(t, git, t.TempDir())
	pin := gitOutput(t, git, repo, "rev-parse", "HEAD")
	runGit(t, git, repo, "commit", "--allow-empty", "-m", "new default tip")
	newTip := gitOutput(t, git, repo, "rev-parse", "HEAD")
	runGit(t, git, repo, "update-ref", "refs/remotes/origin/main", newTip)
	ws, err := New(Options{Binary: git, ManagedRoot: filepath.Join(t.TempDir(), "managed"), RepoResolver: StaticRepoResolver{"proj": repo}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := ports.WorkspaceConfig{ProjectID: "proj", SessionID: "pinned", Branch: "swarm/pinned", PinnedSourceCommit: pin, BaseBranch: "nonexistent", BaseRef: "origin/main"}
	info, err := ws.Create(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, git, info.Path, "rev-parse", "HEAD"); got != pin || info.BaseSHA != pin || info.BaseRef != pin {
		t.Fatalf("HEAD=%s info=%+v want pinned=%s (default=%s)", got, info, pin, newTip)
	}
	if _, err = ws.Create(context.Background(), cfg); err == nil {
		t.Fatal("reused existing pinned branch/workspace")
	}
	occupied := cfg
	occupied.Branch = "swarm/another-branch"
	if _, err := ws.Create(context.Background(), occupied); err == nil {
		t.Fatal("reused existing workspace path under a new branch")
	}
	fresh := cfg
	fresh.SessionID = "fresh"
	fresh.Branch = "swarm/fresh"
	fresh.FreshBranch = true
	if _, err := ws.Create(context.Background(), fresh); err == nil {
		t.Fatal("speculative branch selection bypassed source pin")
	}
	if got := gitOutput(t, git, repo, "rev-parse", "HEAD"); got != newTip {
		t.Fatal("source checkout changed")
	}

	for _, tc := range []struct{ name, branch, commit string }{
		{"existing-local", "already-local", pin}, {"existing-remote", "already-remote", pin},
		{"missing-commit", "missing-commit", strings.Repeat("a", 40)}, {"ref-not-commit", "ref-not-commit", "HEAD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "existing-local" {
				runGit(t, git, repo, "branch", tc.branch, newTip)
			}
			if tc.name == "existing-remote" {
				runGit(t, git, repo, "update-ref", "refs/remotes/origin/"+tc.branch, newTip)
			}
			bad := cfg
			bad.SessionID = domain.SessionID("bad-" + tc.name)
			bad.Branch = tc.branch
			bad.PinnedSourceCommit = tc.commit
			if _, err := ws.Create(context.Background(), bad); err == nil {
				t.Fatal("unsafe pinned workspace accepted")
			}
		})
	}
}
