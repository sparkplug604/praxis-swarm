// Package qualification prepares swarm launches through AO's agent/catalog ports.
// It does not launch processes, provision sandboxes, or turn native approval flags
// into confinement guarantees. Live admission requires coordinator-owned evidence.
package qualification

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type Role string

const (
	Planner     Role = "planner"
	Worker      Role = "worker"
	Reviewer    Role = "reviewer"
	Integration Role = "integration"
)

var (
	ErrInvalid           = errors.New("invalid swarm qualification request")
	ErrUnavailable       = errors.New("selected swarm model unavailable")
	ErrUnverified        = errors.New("swarm capabilities unverified")
	ErrUnsafePermissions = errors.New("unsafe swarm permissions")
	ErrIdentityMismatch  = errors.New("swarm execution identity mismatch")
)

type Selection struct {
	AgentID string `json:"agentId"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
}

// Policy authorizes ordered alternatives separately for each role. Only a
// definitive missing adapter/model can use one; auth, invalid effort, cancellation,
// and unknown capability errors never trigger provider substitution.
type Policy struct{ Fallbacks map[Role][]Selection }

type PermissionRequirements struct {
	CoordinatorRoot string `json:"coordinatorRoot"`
	EvaluatorRoot   string `json:"evaluatorRoot"`
	// Reviewers must bind the exact frozen submission they can only read.
	SubmissionSHA256 string `json:"submissionSha256,omitempty"`
}

type Request struct {
	Role        Role
	Requested   Selection
	Policy      Policy
	Scope       string
	Launch      ports.LaunchConfig
	Permissions PermissionRequirements
}

// Catalog is implemented by AO's existing agent service. Use synchronous
// revalidation, not a stale picker response with a background refresh pending.
type Catalog interface {
	RevalidateModels(context.Context, string, string) (ports.AgentModelCatalog, error)
}

type RuntimeIdentity struct {
	BinaryPath   string `json:"binaryPath"`
	BinarySHA256 string `json:"binarySha256"`
	Version      string `json:"version"`
	// Hash of effective runtime/sandbox/config inputs, including credential scope
	// identity where relevant. Never include raw account credentials.
	ConfigFingerprint string `json:"configFingerprint"`
}

type RuntimeInspector interface {
	Inspect(context.Context, string, ports.LaunchConfig) (RuntimeIdentity, error)
}

type EvidenceMode string

const (
	TestEvidence EvidenceMode = "test"
	LiveEvidence EvidenceMode = "live"
)

type CheckStatus string

const (
	Passed      CheckStatus = "passed"
	Failed      CheckStatus = "failed"
	Unknown     CheckStatus = "unknown"
	Unsupported CheckStatus = "unsupported"
)

type CapabilityChecks struct {
	SettingsPreserved      CheckStatus `json:"settingsPreserved"`
	CancellationObserved   CheckStatus `json:"cancellationObserved"`
	ProcessContainment     CheckStatus `json:"processContainment"`
	ProtectedStorageDenied CheckStatus `json:"protectedStorageDenied"`
	SubmissionReadOnly     CheckStatus `json:"submissionReadOnly"`
}

// CapabilityEvidence must come from trusted coordinator-owned receipts, never
// worker-supplied JSON. Passing unit tests is TestEvidence, even if filesystem
// fixtures exist. A real sandbox probe must exercise shell/descendant writes as
// well as native tools before reporting containment. No live verifier ships here.
type CapabilityEvidence struct {
	Binding   string           `json:"binding"`
	Mode      EvidenceMode     `json:"mode"`
	ReceiptID string           `json:"receiptId"`
	ExpiresAt time.Time        `json:"expiresAt"`
	Checks    CapabilityChecks `json:"checks"`
}

type Probe struct {
	Binding     string
	Role        Role
	Runtime     RuntimeIdentity
	Selection   Selection
	Launch      ports.LaunchConfig
	Command     []string
	Permissions PermissionRequirements
}

type PermissionVerifier interface {
	Verify(context.Context, Probe) (CapabilityEvidence, error)
}

type Report struct {
	// CapabilityFingerprint pins the launch profile before composing a task.
	// Prompt and session identity are excluded to avoid a self-referential packet;
	// Binding and the final execution receipt bind those fields after composition.
	// This fingerprint alone never authorizes execution.
	CapabilityFingerprint string    `json:"capabilityFingerprint"`
	Requested             Selection `json:"requested"`
	Resolved              Selection `json:"resolved"`
	FallbackUsed          bool      `json:"fallbackUsed"`
	// Empty reported fields mean unknown; argv is not executed-model telemetry.
	ReportedModel  string                `json:"reportedModel,omitempty"`
	ReportedEffort string                `json:"reportedEffort,omitempty"`
	Telemetry      string                `json:"telemetry"`
	Runtime        RuntimeIdentity       `json:"runtime"`
	Binding        string                `json:"binding"`
	Evidence       CapabilityEvidence    `json:"evidence"`
	Auth           ports.AgentAuthStatus `json:"auth"`
	TestVerified   bool                  `json:"testVerified"`
	LiveQualified  bool                  `json:"liveQualified"`
}

// Preparation is diagnostic until err == nil and Report.LiveQualified is true.
// Consumers must re-prepare immediately before dispatch and retain the binding;
// never launch from a failed preparation or replay it after configuration changes.
// Directory identities are checked before and after verification, but this does
// not close a later filesystem race: the dispatcher must pin/enforce the actual
// sandbox boundaries and revalidate immediately before its owned launch.
type Preparation struct {
	Report         Report
	Launch         ports.LaunchConfig
	Command        []string
	PromptDelivery ports.PromptDeliveryStrategy
}

type Service struct {
	Catalog     Catalog
	Agent       func(string) (ports.Agent, error)
	Runtime     RuntimeInspector
	Permissions PermissionVerifier
	Now         func() time.Time
}
