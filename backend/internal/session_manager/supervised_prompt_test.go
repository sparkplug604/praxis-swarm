package sessionmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/workspace/gitworktree"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func supervisedPromptConfig() ports.SpawnConfig {
	return ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessCodex,
		RequestedMode: domain.SessionModeChat, Branch: "swarm/supervised", PinnedSourceCommit: strings.Repeat("a", 40),
		SupervisedAssignment: true, Prompt: "Supplied immutable task packet\n", DisableCosmeticModelCalls: true,
		AgentConfig: ports.AgentConfig{Permissions: ports.PermissionModeAcceptEdits}, AgentConfigResolved: true}
}

func assertSupervisedPrompt(t *testing.T, prompt, pin string) {
	t.Helper()
	for _, want := range []string{pin, "supplied task packet", "file ownership", "allowed paths", "source checkout", "verification commands", "accept-edits", "sandbox boundaries", "approval requirements", "Do not broaden permissions", "final response", "tests and their outcomes", "coordinator collects your native conversation result"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{"ao report", "ao send", "ao spawn", "--done", "Worker Reports", "Orchestrator Coordination", "using-ao", "Create or update a PR"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("ordinary workflow %q leaked into supervised prompt", unwanted)
		}
	}
	if len(prompt) > 2000 {
		t.Fatalf("supervised standing prompt grew to %d bytes", len(prompt))
	}
}

func TestSupervisedAssignmentPromptExcludesOrdinaryWorkflow(t *testing.T) {
	cfg := supervisedPromptConfig()
	m, store, _, _ := newManager()
	project := store.projects["mer"]
	project.Config.AgentRules = "Preserve the explicit project restriction on owned paths."
	project.Path = t.TempDir()
	project.Config.AgentRulesFile = "WORKER_RULES.md"
	if err := os.WriteFile(filepath.Join(project.Path, project.Config.AgentRulesFile), []byte("Preserve the explicit repository coding convention."), 0600); err != nil {
		t.Fatal(err)
	}
	store.projects["mer"] = project
	store.sessions["mer-orchestrator"] = domain.SessionRecord{ID: "mer-orchestrator", ProjectID: "mer", Kind: domain.KindOrchestrator}
	prompt, system, err := m.buildSpawnTexts(context.Background(), cfg, "mer-1")
	if err != nil {
		t.Fatal(err)
	}
	if prompt != cfg.Prompt {
		t.Fatalf("supplied packet changed: %q", prompt)
	}
	assertSupervisedPrompt(t, system, cfg.PinnedSourceCommit)
	for _, rule := range []string{project.Config.AgentRules, "Preserve the explicit repository coding convention."} {
		if !strings.Contains(system, rule) {
			t.Fatalf("supervised prompt dropped project rule %q", rule)
		}
	}
	// The ordinary path still carries its existing reports and project rules.
	cfg.SupervisedAssignment = false
	_, ordinary, err := m.buildSpawnTexts(context.Background(), cfg, "mer-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ordinary, "ao report --done") || !strings.Contains(ordinary, project.Config.AgentRules) {
		t.Fatal("ordinary AO prompt changed")
	}
}

func TestSupervisedAssignmentReachesControllerWithAcceptEdits(t *testing.T) {
	repo := newManagerGitRepo(t)
	cfg := supervisedPromptConfig()
	cfg.PinnedSourceCommit = strings.TrimSpace(runManagerGit(t, repo, "rev-parse", "HEAD"))
	workspace, err := gitworktree.New(gitworktree.Options{ManagedRoot: t.TempDir(), RepoResolver: gitworktree.StaticRepoResolver{"mer": repo}})
	if err != nil {
		t.Fatal(err)
	}
	chat := &recordingLauncher{}
	m, store, runtime := newChatManager(chat)
	m.dataDir = t.TempDir()
	m.workspace = workspace
	project := store.projects["mer"]
	project.Path = repo
	project.Config.AgentConfig.Permissions = ports.PermissionModeBypassPermissions
	store.projects["mer"] = project
	rec, promptBytes, systemBytes, err := m.Spawn(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.started) != 1 || runtime.created != 0 {
		t.Fatalf("controller starts=%d runtime=%d", len(chat.started), runtime.created)
	}
	started := chat.started[0]
	if started.Permissions != ports.PermissionModeAcceptEdits || rec.Metadata.Permissions != ports.PermissionModeAcceptEdits || len(chat.preflightPermissions) != 1 || chat.preflightPermissions[0] != ports.PermissionModeAcceptEdits {
		t.Fatal("supervised permissions were weakened")
	}
	assertSupervisedPrompt(t, started.SystemPrompt, cfg.PinnedSourceCommit)
	if promptBytes != len(cfg.Prompt) || systemBytes != len(started.SystemPrompt) {
		t.Fatalf("byte counts do not match actual prompt: %d/%d", promptBytes, systemBytes)
	}
}

func TestSupervisedAssignmentRejectsMismatchedLaunchProfile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ports.SpawnConfig)
	}{
		{"unpinned", func(c *ports.SpawnConfig) { c.PinnedSourceCommit = "" }},
		{"harness", func(c *ports.SpawnConfig) { c.Harness = domain.HarnessClaudeCode }},
		{"terminal", func(c *ports.SpawnConfig) { c.RequestedMode = domain.SessionModeTUI }},
		{"orchestrator", func(c *ports.SpawnConfig) { c.Kind = domain.KindOrchestrator }},
		{"bypass", func(c *ports.SpawnConfig) { c.AgentConfig.Permissions = ports.PermissionModeBypassPermissions }},
		{"default-permissions", func(c *ports.SpawnConfig) { c.AgentConfig.Permissions = "" }},
		{"async", func(c *ports.SpawnConfig) { c.Async = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := supervisedPromptConfig()
			tc.mutate(&cfg)
			chat := &recordingLauncher{}
			m, _, rt := newChatManager(chat)
			if _, _, _, err := m.Spawn(context.Background(), cfg); err == nil {
				t.Fatal("invalid supervised profile accepted")
			}
			if _, _, err := m.buildSpawnTexts(context.Background(), cfg, "mer-1"); err == nil {
				t.Fatal("invalid supervised prompt accepted")
			}
			if len(chat.preflighted) != 0 || len(chat.started) != 0 || rt.created != 0 || m.workspace.(*fakeWorkspace).createCount != 0 {
				t.Fatal("invalid profile reached launch")
			}
		})
	}
}

func TestSupervisedAssignmentFailsForMissingProjectRules(t *testing.T) {
	m, store, _, _ := newManager()
	project := store.projects["mer"]
	project.Path = t.TempDir()
	project.Config.AgentRulesFile = "missing-rules.md"
	store.projects["mer"] = project
	if _, _, err := m.buildSpawnTexts(context.Background(), supervisedPromptConfig(), "mer-1"); err == nil {
		t.Fatal("missing explicit project rules file was silently dropped")
	}
}
