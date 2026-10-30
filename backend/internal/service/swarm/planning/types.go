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
	ErrInvalid = errors.New("invalid swarm planning input")
	ErrStale   = errors.New("stale swarm packet or tool identity")
	ErrContext = errors.New("required swarm context unavailable or unapproved")
	ErrBudget  = errors.New("mandatory swarm packet exceeds context budget")
)

const PacketVersion int64 = 1
const MaxPlannerBytes = 2 * 1024 * 1024
const MaxPacketBytes = 1024 * 1024

type Subject struct {
	ProjectID                                   domain.ProjectID
	RunID, TaskID, AttemptID, TaskType, Backend string
	Paths                                       []string
}

// Resolve must check current source eligibility/revocation and task scope. A
// planner-supplied "approved" flag is never accepted. No live Praxis connector
// ships here; implementations must obtain approval from their existing authority.
type ContextResolver interface {
	Resolve(context.Context, Subject, domain.SwarmContextRef) (ApprovedContext, error)
}

type ApprovedContext struct {
	ID, SHA256, DecisionID string
	DecisionVersion        int64
	Body                   string
}

type IncludedContext struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	SHA256          string `json:"sha256"`
	DecisionID      string `json:"decisionId"`
	DecisionVersion int64  `json:"decisionVersion"`
	Body            string `json:"body"`
}

type OmittedContext struct {
	ID     string `json:"id"`
	Reason string `json:"reason"` // not_applicable, unavailable, unapproved, hash_mismatch, budget
}

// Measure must return bytes for the complete rendered packet, including the
// selected bodies and omission receipts. Required bodies are never truncated.
type Measure func([]IncludedContext, []OmittedContext) (int, error)

type ContextSelection struct {
	Included []IncludedContext
	Omitted  []OmittedContext
	Bytes    int
}

type Store interface {
	CreateSwarmRun(context.Context, string, string, domain.SwarmPlan, time.Time) (domain.SwarmRun, error)
	GetSwarmRun(context.Context, string) (domain.SwarmRun, error)
	GetSwarmAttempt(context.Context, string) (domain.SwarmAttempt, error)
	GetSwarmContract(context.Context, string, string, int64) (domain.SwarmTaskContract, error)
}

type ToolVerifier interface {
	Verify(context.Context, []domain.SwarmToolIdentity) error
}

type Service struct {
	Store   Store
	Context ContextResolver
	Tools   ToolVerifier
	Now     func() time.Time
}

// A packet is a frozen envelope. Entry and Markdown are regenerated and compared
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
