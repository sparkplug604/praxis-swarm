// Package supervised connects supplied plans to ordinary AO sessions. It does
// not authorize the exact-copy scheduler or claim independent acceptance.
package supervised

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
)

// Profile identifies the supervised AO execution contract in retained evidence.
const Profile = "supervised-ao-v1"

// StartInput supplies a plan, bounded context and a maximum run duration.
type StartInput struct {
	RequestID      string                     `json:"requestId"`
	Plan           json.RawMessage            `json:"plan"`
	Context        []planning.SuppliedContext `json:"context"`
	TimeoutSeconds int                        `json:"timeoutSeconds"`
}

// Run retains the source baseline, worker outcomes and explicit assurance limits.
type Run struct {
	ID              string           `json:"id"`
	Profile         string           `json:"profile"`
	RequestID       string           `json:"requestId"`
	RequestHash     string           `json:"requestHash"`
	Plan            domain.SwarmPlan `json:"plan"`
	Status          string           `json:"status"`
	CreatedAt       time.Time        `json:"createdAt"`
	DeadlineAt      time.Time        `json:"deadlineAt"`
	SourcePath      string           `json:"sourcePath"`
	SourceSnapshot  string           `json:"sourceSnapshot"`
	SourceUnchanged bool             `json:"sourceUnchanged"`
	Tasks           []Task           `json:"tasks"`
	Errors          []string         `json:"errors"`
	Limits          []string         `json:"limits"`
}

// Task binds a launch intent to its observed session and frozen candidate files.
type Task struct {
	ID            string                    `json:"id"`
	RequestID     string                    `json:"requestId"`
	Branch        string                    `json:"branch"`
	Packet        planning.SupervisedPacket `json:"packet"`
	SessionID     string                    `json:"sessionId,omitempty"`
	Status        string                    `json:"status"`
	Observation   Observation               `json:"observation"`
	WorkspacePath string                    `json:"workspacePath,omitempty"`
	SnapshotPath  string                    `json:"snapshotPath,omitempty"`
	SnapshotHash  string                    `json:"snapshotHash,omitempty"`
	CandidateHash string                    `json:"candidateHash,omitempty"`
	DiffPath      string                    `json:"diffPath,omitempty"`
	Error         string                    `json:"error,omitempty"`
}

// Observation records provider-reported settings, usage and completion evidence.
type Observation struct {
	Controller        string          `json:"controller"`
	TurnState         string          `json:"turnState"`
	StartedAt         *time.Time      `json:"startedAt,omitempty"`
	CompletedAt       *time.Time      `json:"completedAt,omitempty"`
	ProviderTurnID    string          `json:"providerTurnId,omitempty"`
	Model             string          `json:"model,omitempty"`
	Effort            string          `json:"effort,omitempty"`
	ModelEvidence     string          `json:"modelEvidence,omitempty"`
	PromptEvidence    string          `json:"promptEvidence,omitempty"`
	Usage             json.RawMessage `json:"usage,omitempty"`
	PromptBytes       int             `json:"promptBytes,omitempty"`
	SystemPromptBytes int             `json:"systemPromptBytes,omitempty"`
	ObservedAt        time.Time       `json:"observedAt"`
}

// Manager exposes the supervised run lifecycle to callers.
type Manager interface {
	Start(context.Context, StartInput) (Run, error)
	Get(context.Context, string) (Run, error)
	Stop(context.Context, string) (Run, error)
}

// Store persists idempotent run creation and subsequent evidence updates.
type Store interface {
	CreateSupervisedSwarmRun(context.Context, string, string, string, []byte, time.Time) ([]byte, bool, error)
	GetSupervisedSwarmRun(context.Context, string) ([]byte, error)
	UpdateSupervisedSwarmRun(context.Context, string, []byte, time.Time) error
}

// Runtime adapts project workspaces and ordinary AO sessions to the coordinator.
type Runtime interface {
	ProjectPath(context.Context, domain.ProjectID) (string, error)
	Launch(context.Context, domain.ProjectID, string, string, planning.SupervisedPacket) (string, error)
	Find(context.Context, string) (string, error)
	Observe(context.Context, string) (Observation, error)
	Stop(context.Context, string) error
	Workspace(context.Context, string) (string, error)
}
