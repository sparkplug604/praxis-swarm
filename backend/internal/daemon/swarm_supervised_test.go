package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/supervised"
)

type supervisedAdapterStore struct {
	project domain.ProjectRecord
	session domain.SessionRecord
	found   bool
}

func (s *supervisedAdapterStore) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return s.project, s.found, nil
}
func (s *supervisedAdapterStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return s.session, s.found, nil
}
func (s *supervisedAdapterStore) GetSessionByClientRequestID(context.Context, string) (domain.SessionRecord, bool, error) {
	return s.session, s.found, nil
}

type supervisedAdapterSessions struct {
	cfg           ports.SpawnConfig
	record        domain.SessionRecord
	bytes, system int
	stopErr       error
	stops         int
}

func (s *supervisedAdapterSessions) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error) {
	s.cfg = cfg
	return domain.Session{SessionRecord: s.record}, s.bytes, s.system, nil
}
func (s *supervisedAdapterSessions) ExitAgent(context.Context, domain.SessionID) (sessionsvc.ExitAgentOutcome, error) {
	s.stops++
	return sessionsvc.ExitAgentOutcome{}, s.stopErr
}

type supervisedAdapterChat struct {
	snapshot chatsvc.Snapshot
	err      error
}

func (c *supervisedAdapterChat) Snapshot(context.Context, domain.SessionID) (chatsvc.Snapshot, error) {
	return c.snapshot, c.err
}

func supervisedAdapterFixture(t *testing.T) (*supervisedAORuntime, *supervisedAdapterStore, *supervisedAdapterSessions, *supervisedAdapterChat) {
	t.Helper()
	record := domain.SessionRecord{ID: "session-1", ProjectID: "project", Kind: domain.KindWorker, Harness: "codex", Mode: domain.SessionModeChat, ClientRequestID: "sao-run-worker", Metadata: domain.SessionMetadata{WorkspacePath: t.TempDir()}}
	store := &supervisedAdapterStore{project: domain.ProjectRecord{ID: "project", Path: t.TempDir()}, session: record, found: true}
	sessions := &supervisedAdapterSessions{record: record, bytes: 1234, system: 456}
	chat := &supervisedAdapterChat{snapshot: chatsvc.Snapshot{SessionID: record.ID, Harness: "codex", Mode: domain.SessionModeChat, Controller: ports.ChatControllerStopped}}
	return &supervisedAORuntime{store: store, sessions: sessions, chat: chat}, store, sessions, chat
}

func TestSupervisedAOAdapterLaunchAndObservedEvidence(t *testing.T) {
	a, store, sessions, chat := supervisedAdapterFixture(t)
	ctx := context.Background()
	packet := planning.SupervisedPacket{Profile: supervised.Profile, SourceCommit: strings.Repeat("a", 40), TaskID: "worker", Markdown: "supplied packet", Requested: domain.SwarmSelection{Harness: "codex", Model: "requested-model", Effort: "low"}}
	id, err := a.Launch(ctx, "project", "sao-run-worker", "swarm/branch", packet)
	if err != nil || id != "session-1" {
		t.Fatalf("id=%s err=%v", id, err)
	}
	cfg := sessions.cfg
	if cfg.ClientRequestID != "sao-run-worker" || cfg.ClientRequestHash == "" || cfg.Branch != "swarm/branch" || cfg.PinnedSourceCommit != packet.SourceCommit || !cfg.SupervisedAssignment || cfg.Harness != "codex" || cfg.RequestedMode != domain.SessionModeChat || cfg.AgentConfig.Permissions != ports.PermissionModeAcceptEdits || cfg.AgentConfig.Model != "requested-model" || cfg.AgentConfig.Effort != "low" || !cfg.AgentConfigResolved || !cfg.EffortOverride || !cfg.DisableCosmeticModelCalls || cfg.Prompt != packet.Markdown {
		t.Fatalf("launch config=%+v", cfg)
	}
	firstHash := cfg.ClientRequestHash
	if _, err = a.Launch(ctx, "project", "sao-run-worker", "swarm/other", packet); err != nil {
		t.Fatal(err)
	}
	if sessions.cfg.ClientRequestHash == firstHash {
		t.Fatal("changed branch retained launch hash")
	}
	now := time.Now().UTC()
	older := now.Add(-time.Minute)
	chat.snapshot.Conversation.Settings = domain.ConversationSettings{Model: "observed-model", ReasoningEffort: "medium"}
	chat.snapshot.Usage = &domain.ConversationUsage{TotalTokens: 37}
	chat.snapshot.Turns = []domain.ConversationTurn{{ID: "current", HandledBySessionID: store.session.ID, RequestedAt: now, StartedAt: &older, CompletedAt: &now, State: domain.TurnStateCompleted, ProviderTurnID: "provider-turn"}, {ID: "older", RequestedAt: older, State: domain.TurnStateRunning}}
	observed, err := a.Observe(ctx, id)
	if err != nil || observed.Model != "observed-model" || observed.Effort != "medium" || observed.TurnState != "completed" || observed.ProviderTurnID != "provider-turn" || observed.PromptBytes != 1234 || observed.SystemPromptBytes != 456 || observed.PromptEvidence != "spawn_response_byte_counts" || !strings.Contains(string(observed.Usage), `"totalTokens":37`) || !strings.Contains(observed.ModelEvidence, "not_independent") {
		t.Fatalf("observation=%+v err=%v", observed, err)
	}
	workspace, err := a.Workspace(ctx, id)
	if err != nil || workspace != store.session.Metadata.WorkspacePath {
		t.Fatalf("workspace=%s err=%v", workspace, err)
	}
	if err = a.Stop(ctx, id); err != nil || sessions.stops != 1 {
		t.Fatalf("stop=%v calls=%d", err, sessions.stops)
	}
}

func TestSupervisedAOAdapterPreservesUnknownAndOwnership(t *testing.T) {
	a, store, sessions, chat := supervisedAdapterFixture(t)
	ctx := context.Background()
	observed, err := a.Observe(ctx, "session-1")
	if err != nil || observed.PromptEvidence != "unknown_after_restart_or_replay" || observed.Usage != nil || observed.CompletedAt != nil {
		t.Fatalf("unknown=%+v %v", observed, err)
	}
	// A durable but uncommitted launch is retained for reconciliation.
	id, err := a.Find(ctx, "sao-run-worker")
	if err != nil || id != "session-1" {
		t.Fatalf("find=%s %v", id, err)
	}
	if _, err = a.Find(ctx, "other-request"); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatal(err)
	}
	sessions.stopErr = errors.New("unconfirmed stop")
	if err = a.Stop(ctx, "session-1"); !errors.Is(err, sessions.stopErr) {
		t.Fatal(err)
	}
	chat.err = errors.New("snapshot unavailable")
	if _, err = a.Observe(ctx, "session-1"); !errors.Is(err, chat.err) {
		t.Fatal(err)
	}
	chat.err = nil
	chat.snapshot.SessionID = "other"
	if _, err = a.Observe(ctx, "session-1"); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatal(err)
	}
	store.session.Mode = domain.SessionModeTUI
	if err = a.Stop(ctx, "session-1"); !errors.Is(err, domain.ErrSwarmConflict) || sessions.stops != 1 {
		t.Fatalf("foreign controller stopped: %v", err)
	}
	store.session.Mode = domain.SessionModeChat
	store.session.Metadata.WorkspacePath = ""
	if _, err = a.Workspace(ctx, "session-1"); !errors.Is(err, domain.ErrSwarmNotReady) {
		t.Fatal(err)
	}
}

func TestSupervisedAOAdapterRequiresLocalSingleRepository(t *testing.T) {
	for _, kind := range []domain.ProjectKind{domain.ProjectKindWorkspace, domain.ProjectKindScratch} {
		a, store, _, _ := supervisedAdapterFixture(t)
		store.project.Kind = kind
		if _, err := a.ProjectPath(context.Background(), "project"); !errors.Is(err, domain.ErrSwarmInvalid) {
			t.Fatal(err)
		}
	}
	a, store, _, _ := supervisedAdapterFixture(t)
	store.project.Path = "ssh://host/repo"
	if _, err := a.ProjectPath(context.Background(), "project"); !errors.Is(err, domain.ErrSwarmInvalid) {
		t.Fatal(err)
	}
	store.found = false
	if _, err := a.ProjectPath(context.Background(), "project"); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatal(err)
	}
}
