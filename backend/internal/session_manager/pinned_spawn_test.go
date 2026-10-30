package sessionmanager

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/workspace/gitworktree"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type pinnedSpawnWorkspace struct {
	*gitworktree.Workspace
	resolves, fetches int
}

func (w *pinnedSpawnWorkspace) ResolveDefaultBranch(context.Context, string, string) (ports.WorkspaceDefaultBranch, error) {
	w.resolves++
	return ports.WorkspaceDefaultBranch{}, nil
}
func (w *pinnedSpawnWorkspace) FetchDefaultBranch(context.Context, string, ports.WorkspaceDefaultBranch) error {
	w.fetches++
	return nil
}

func TestPinnedSpawnChecksSourceBeforeProvider(t *testing.T) {
	for _, changeDuringProvision := range []bool{false, true} {
		name := "source-pinned"
		if changeDuringProvision {
			name = "provision-changed-head"
		}
		t.Run(name, func(t *testing.T) {
			repo := newManagerGitRepo(t)
			pin := strings.TrimSpace(runManagerGit(t, repo, "rev-parse", "HEAD"))
			runManagerGit(t, repo, "commit", "--allow-empty", "-m", "advance default")
			workspace, err := gitworktree.New(gitworktree.Options{ManagedRoot: t.TempDir(), RepoResolver: gitworktree.StaticRepoResolver{"mer": repo}})
			if err != nil {
				t.Fatal(err)
			}
			ws := &pinnedSpawnWorkspace{Workspace: workspace}
			chat := &recordingLauncher{}
			m, store, runtime := newChatManager(chat)
			m.dataDir = t.TempDir()
			m.workspace = ws
			project := store.projects["mer"]
			project.Path = repo
			if changeDuringProvision {
				project.Config.PostCreate = []string{"git -c user.name=AO -c user.email=ao@example.com commit --allow-empty -m changed-head"}
			}
			store.projects["mer"] = project
			cfg := ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, RequestedMode: domain.SessionModeChat, Branch: "swarm/pin", PinnedSourceCommit: pin, Prompt: "fake provider only", DisableCosmeticModelCalls: true}
			record, _, _, err := m.Spawn(context.Background(), cfg)
			if ws.fetches != 0 || ws.resolves != 0 {
				t.Fatalf("pinned start refreshed defaults: resolves=%d fetches=%d", ws.resolves, ws.fetches)
			}
			if changeDuringProvision {
				if err == nil || len(chat.started) != 0 || runtime.created != 0 {
					t.Fatalf("changed HEAD reached provider: err=%v starts=%d runtime=%d", err, len(chat.started), runtime.created)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(chat.started) != 1 || runtime.created != 0 {
				t.Fatalf("starts=%d runtime=%d", len(chat.started), runtime.created)
			}
			if got := strings.TrimSpace(runManagerGit(t, record.Metadata.WorkspacePath, "rev-parse", "HEAD")); got != pin {
				t.Fatalf("HEAD=%s want %s", got, pin)
			}
		})
	}
}

func TestPinnedSpawnRejectsUnsupportedPathsBeforeWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ports.SpawnConfig)
	}{
		{"async", func(c *ports.SpawnConfig) { c.Async = true }},
		{"speculative", func(c *ports.SpawnConfig) { c.TaskPreparation = "token" }},
		{"missing-branch", func(c *ports.SpawnConfig) { c.Branch = "" }},
		{"symbolic-ref", func(c *ports.SpawnConfig) { c.PinnedSourceCommit = "HEAD" }},
		{"orchestrator", func(c *ports.SpawnConfig) { c.Kind = domain.KindOrchestrator }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat := &recordingLauncher{}
			m, _, rt := newChatManager(chat)
			cfg := ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, RequestedMode: domain.SessionModeChat, Branch: "swarm/pin", PinnedSourceCommit: strings.Repeat("a", 40)}
			tc.mutate(&cfg)
			if _, _, _, err := m.Spawn(context.Background(), cfg); err == nil {
				t.Fatal("unsupported pin accepted")
			}
			if m.workspace.(*fakeWorkspace).createCount != 0 || len(chat.started) != 0 || rt.created != 0 {
				t.Fatal("unsupported pin had launch effects")
			}
		})
	}
}
