// Package planning validates planner data and composes immutable worker packets.
// It does not call a model, execute commands, approve lessons or dispatch work.
package planning

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var (
	// ErrInvalid reports malformed planning data.
	ErrInvalid = errors.New("invalid swarm planning input")
	// ErrStale reports changed packet or executable identity.
	ErrStale = errors.New("stale swarm packet or tool identity")
	// ErrContext reports unavailable or unapproved required context.
	ErrContext = errors.New("required swarm context unavailable or unapproved")
	// ErrBudget reports mandatory context exceeding the packet bound.
	ErrBudget = errors.New("mandatory swarm packet exceeds context budget")
)

// PacketVersion identifies the serialized worker-packet protocol.
const PacketVersion int64 = 1

// MaxPlannerBytes bounds the supplied planner JSON document.
const MaxPlannerBytes = 2 * 1024 * 1024

// MaxPacketBytes bounds the complete serialized worker packet.
const MaxPacketBytes = 1024 * 1024

// Subject supplies task identity and path scope to the context approval authority.
type Subject struct {
	ProjectID                                   domain.ProjectID
	RunID, TaskID, AttemptID, TaskType, Backend string
	Paths                                       []string
}

// ContextResolver must check current source eligibility/revocation and task scope. A
// planner-supplied "approved" flag is never accepted. No live Praxis connector
// ships here; implementations must obtain approval from their existing authority.
type ContextResolver interface {
	Resolve(context.Context, Subject, domain.SwarmContextRef) (ApprovedContext, error)
}

// ApprovedContext binds eligible content to the authority decision that approved it.
type ApprovedContext struct {
	ID, SHA256, DecisionID string
	DecisionVersion        int64
	Body                   string
}

// IncludedContext retains selected content and its approval provenance in a packet.
type IncludedContext struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	SHA256          string `json:"sha256"`
	DecisionID      string `json:"decisionId"`
	DecisionVersion int64  `json:"decisionVersion"`
	Body            string `json:"body"`
}

// OmittedContext records why a context reference was excluded without truncating it.
type OmittedContext struct {
	ID     string `json:"id"`
	Reason string `json:"reason"` // not_applicable, unavailable, unapproved, hash_mismatch, budget
}

// Measure must return bytes for the complete rendered packet, including the
// selected bodies and omission receipts. Required bodies are never truncated.
type Measure func([]IncludedContext, []OmittedContext) (int, error)

// ContextSelection contains the selected bodies, omission receipts and measured size.
type ContextSelection struct {
	Included []IncludedContext
	Omitted  []OmittedContext
	Bytes    int
}

// Store supplies durable plans and immutable task/attempt contracts for composition.
type Store interface {
	CreateSwarmRun(context.Context, string, string, domain.SwarmPlan, time.Time) (domain.SwarmRun, error)
	GetSwarmRun(context.Context, string) (domain.SwarmRun, error)
	GetSwarmAttempt(context.Context, string) (domain.SwarmAttempt, error)
	GetSwarmContract(context.Context, string, string, int64) (domain.SwarmTaskContract, error)
}

// ToolVerifier revalidates recorded executable identities without selecting substitutes.
type ToolVerifier interface {
	Verify(context.Context, []domain.SwarmToolIdentity) error
}

// Service composes worker packets from durable contracts and trusted context/tool checks.
type Service struct {
	Store   Store
	Context ContextResolver
	Tools   ToolVerifier
	Now     func() time.Time
}

// Packet is a frozen envelope. Entry and Markdown are regenerated and compared
// on resume; their prose is not a second source of scheduling instructions.
type Packet struct {
	Version               int64                    `json:"version"`
	ProjectID             domain.ProjectID         `json:"projectId"`
	RunID                 string                   `json:"runId"`
	PlanVersion           int64                    `json:"planVersion"`
	TaskID                string                   `json:"taskId"`
	TaskVersion           int64                    `json:"taskVersion"`
	AttemptID             string                   `json:"attemptId"`
	OperationID           string                   `json:"operationId"`
	Owner                 string                   `json:"owner"`
	Generation            int64                    `json:"generation"`
	BaseCommit            string                   `json:"baseCommit"`
	ContractHash          string                   `json:"contractHash"`
	CapabilityFingerprint string                   `json:"capabilityFingerprint"`
	Resolved              domain.SwarmSelection    `json:"resolved"`
	Contract              domain.SwarmTaskContract `json:"contract"`
	Context               []IncludedContext        `json:"context"`
	Omitted               []OmittedContext         `json:"omitted"`
	Entry                 string                   `json:"entry"`
	Markdown              string                   `json:"markdown"`
	MaxBytes              int                      `json:"maxBytes"`
	ByteCount             int                      `json:"byteCount"`
	SHA256                string                   `json:"sha256"`
}
