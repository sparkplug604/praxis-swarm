package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/supervised"
)

type supervisedAOStore interface {
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	GetSessionByClientRequestID(context.Context, string) (domain.SessionRecord, bool, error)
}
type supervisedAOSessions interface {
	Spawn(context.Context, ports.SpawnConfig) (domain.Session, int, int, error)
	ExitAgent(context.Context, domain.SessionID) (sessionsvc.ExitAgentOutcome, error)
}
type supervisedAOChat interface {
	Snapshot(context.Context, domain.SessionID) (chatsvc.Snapshot, error)
}

// supervisedAORuntime uses existing AO session semantics. Controller observations
// are not exact scheduler leases, OS process absence or containment evidence.
type supervisedAORuntime struct {
	store       supervisedAOStore
	sessions    supervisedAOSessions
	chat        supervisedAOChat
	mu          sync.Mutex
	promptBytes map[domain.SessionID][2]int
}

var _ supervised.Runtime = (*supervisedAORuntime)(nil)

func (a *supervisedAORuntime) ProjectPath(ctx context.Context, id domain.ProjectID) (string, error) {
	project, found, err := a.store.GetProject(ctx, string(id))
	if err != nil {
		return "", err
	}
	if !found {
		return "", domain.ErrSwarmNotFound
	}
	if project.ID != string(id) || project.Kind.WithDefault() != domain.ProjectKindSingleRepo || !project.ArchivedAt.IsZero() || !filepath.IsAbs(project.Path) {
		return "", fmt.Errorf("%w: supervised runs require an active local single-repository project", domain.ErrSwarmInvalid)
	}
	path, err := filepath.EvalSymlinks(project.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", domain.ErrSwarmInvalid
	}
	return path, nil
}

func (a *supervisedAORuntime) Launch(ctx context.Context, projectID domain.ProjectID, requestID, branch string, packet planning.SupervisedPacket) (string, error) {
	if packet.Profile != supervised.Profile || packet.Requested.Harness != "codex" || !domain.SwarmValidHash(packet.SourceCommit, true) || packet.Markdown == "" || packet.Requested.Model == "" || !strings.HasPrefix(requestID, "sao-") || branch == "" {
		return "", domain.ErrSwarmInvalid
	}
	if _, err := a.ProjectPath(ctx, projectID); err != nil {
		return "", err
	}
	cfg := ports.SpawnConfig{ProjectID: projectID, Kind: domain.KindWorker, Harness: "codex", RequestedMode: domain.SessionModeChat, ClientRequestID: requestID, Branch: branch, PinnedSourceCommit: packet.SourceCommit, SupervisedAssignment: true, Prompt: packet.Markdown,
		AgentConfig: ports.AgentConfig{Model: packet.Requested.Model, Effort: packet.Requested.Effort, Permissions: ports.PermissionModeAcceptEdits}, AgentConfigResolved: true, EffortOverride: true,
		DisplayName: "Swarm " + packet.TaskID, KeepDisplayName: true, DisableCosmeticModelCalls: true}
	cfg.ClientRequestHash = domain.SwarmHash(struct {
		ProjectID            domain.ProjectID
		Branch               string
		Packet               planning.SupervisedPacket
		Permissions          ports.PermissionMode
		SupervisedAssignment bool
	}{projectID, branch, packet, ports.PermissionModeAcceptEdits, true})
	session, promptBytes, systemBytes, err := a.sessions.Spawn(ctx, cfg)
	if session.ID != "" && promptBytes > 0 && systemBytes >= 0 {
		a.mu.Lock()
		if a.promptBytes == nil {
			a.promptBytes = make(map[domain.SessionID][2]int)
		}
		a.promptBytes[session.ID] = [2]int{promptBytes, systemBytes}
		a.mu.Unlock()
	}
	return string(session.ID), err
}

func supervisedOwnedSession(record domain.SessionRecord) bool {
	return record.ID != "" && record.Kind == domain.KindWorker && record.Harness == "codex" && record.Mode == domain.SessionModeChat && strings.HasPrefix(record.ClientRequestID, "sao-")
}

func (a *supervisedAORuntime) Find(ctx context.Context, requestID string) (string, error) {
	record, found, err := a.store.GetSessionByClientRequestID(ctx, requestID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", domain.ErrSwarmNotFound
	}
	if record.ClientRequestID != requestID || !supervisedOwnedSession(record) {
		return "", domain.ErrSwarmConflict
	}
	// Even an uncommitted launch row is retained for stop reconciliation. This
	// lookup does not authorize another Spawn or claim the controller is live.
	return string(record.ID), nil
}

func (a *supervisedAORuntime) session(ctx context.Context, id string) (domain.SessionRecord, error) {
	record, found, err := a.store.GetSession(ctx, domain.SessionID(id))
	if err != nil {
		return record, err
	}
	if !found {
		return record, domain.ErrSwarmNotFound
	}
	if string(record.ID) != id || !supervisedOwnedSession(record) {
		return record, domain.ErrSwarmConflict
	}
	return record, nil
}

func (a *supervisedAORuntime) Observe(ctx context.Context, id string) (supervised.Observation, error) {
	if _, err := a.session(ctx, id); err != nil {
		return supervised.Observation{}, err
	}
	snapshot, err := a.chat.Snapshot(ctx, domain.SessionID(id))
	if err != nil {
		return supervised.Observation{}, err
	}
	if string(snapshot.SessionID) != id || snapshot.Harness != "codex" || snapshot.Mode != domain.SessionModeChat {
		return supervised.Observation{}, domain.ErrSwarmConflict
	}
	out := supervised.Observation{Controller: string(snapshot.Controller), Model: snapshot.Conversation.Settings.Model, Effort: snapshot.Conversation.Settings.ReasoningEffort,
		ModelEvidence: "conversation_settings_not_independent_model_telemetry", PromptEvidence: "unknown_after_restart_or_replay", ObservedAt: time.Now().UTC()}
	var latest *domain.ConversationTurn
	for i := range snapshot.Turns {
		turn := &snapshot.Turns[i]
		if turn.RolledBackAt != nil || (turn.HandledBySessionID != "" && string(turn.HandledBySessionID) != id) {
			continue
		}
		if latest == nil || turn.RequestedAt.After(latest.RequestedAt) {
			latest = turn
		}
	}
	if latest != nil {
		out.TurnState = string(latest.State)
		out.StartedAt = latest.StartedAt
		out.CompletedAt = latest.CompletedAt
		out.ProviderTurnID = latest.ProviderTurnID
	}
	if snapshot.Usage != nil {
		out.Usage, err = json.Marshal(snapshot.Usage)
		if err != nil {
			return supervised.Observation{}, err
		}
	}
	a.mu.Lock()
	sizes, known := a.promptBytes[domain.SessionID(id)]
	a.mu.Unlock()
	if known {
		out.PromptBytes = sizes[0]
		out.SystemPromptBytes = sizes[1]
		out.PromptEvidence = "spawn_response_byte_counts"
	}
	return out, nil
}

func (a *supervisedAORuntime) Stop(ctx context.Context, id string) error {
	if _, err := a.session(ctx, id); err != nil {
		return err
	}
	_, err := a.sessions.ExitAgent(ctx, domain.SessionID(id))
	return err // The service independently observes the resulting controller state.
}

func (a *supervisedAORuntime) Workspace(ctx context.Context, id string) (string, error) {
	record, err := a.session(ctx, id)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(record.Metadata.WorkspacePath) {
		return "", fmt.Errorf("%w: actual session workspace unavailable", domain.ErrSwarmNotReady)
	}
	return record.Metadata.WorkspacePath, nil
}
