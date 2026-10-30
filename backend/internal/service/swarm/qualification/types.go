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

// Role identifies a swarm responsibility with distinct qualification requirements.
type Role string

// Planner, Worker, Reviewer and Integration are supported qualification roles.
const (
	Planner     Role = "planner"
	Worker      Role = "worker"
	Reviewer    Role = "reviewer"
	Integration Role = "integration"
)

var (
	// ErrInvalid reports an invalid qualification request.
	ErrInvalid = errors.New("invalid swarm qualification request")
	// ErrUnavailable reports a definitively unavailable selected adapter or model.
	ErrUnavailable = errors.New("selected swarm model unavailable")
	// ErrUnverified reports missing or insufficient capability evidence.
	ErrUnverified = errors.New("swarm capabilities unverified")
	// ErrUnsafePermissions reports unsafe launch permissions or protected roots.
	ErrUnsafePermissions = errors.New("unsafe swarm permissions")
	// ErrIdentityMismatch reports execution identity differing from qualification evidence.
	ErrIdentityMismatch = errors.New("swarm execution identity mismatch")
)

// Selection requests an explicit agent, model and reasoning effort.
type Selection struct {
	AgentID string `json:"agentId"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
}

// Policy authorizes ordered alternatives separately for each role. Only a
// definitive missing adapter/model can use one; auth, invalid effort, cancellation,
// and unknown capability errors never trigger provider substitution.
type Policy struct{ Fallbacks map[Role][]Selection }

// PermissionRequirements identifies protected storage and any frozen reviewer submission.
type PermissionRequirements struct {
	CoordinatorRoot string `json:"coordinatorRoot"`
	EvaluatorRoot   string `json:"evaluatorRoot"`
	// Reviewers must bind the exact frozen submission they can only read.
	SubmissionSHA256 string `json:"submissionSha256,omitempty"`
}

// Request combines role, selection, launch configuration and permission requirements.
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

// RuntimeIdentity binds the executable and effective runtime configuration without raw credentials.
type RuntimeIdentity struct {
	BinaryPath   string `json:"binaryPath"`
	BinarySHA256 string `json:"binarySha256"`
	Version      string `json:"version"`
	// Hash of effective runtime/sandbox/config inputs, including credential scope
	// identity where relevant. Never include raw account credentials.
	ConfigFingerprint string `json:"configFingerprint"`
}

// RuntimeInspector resolves the executable and configuration identity before qualification.
type RuntimeInspector interface {
	Inspect(context.Context, string, ports.LaunchConfig) (RuntimeIdentity, error)
}

// EvidenceMode separates fixture-based verification from live capability evidence.
type EvidenceMode string

// TestEvidence and LiveEvidence distinguish fixture results from live verification.
const (
	TestEvidence EvidenceMode = "test"
	LiveEvidence EvidenceMode = "live"
)

// CheckStatus records a capability result without treating unknown as passed.
type CheckStatus string

// Passed, Failed, Unknown and Unsupported preserve distinct capability outcomes.
const (
	Passed      CheckStatus = "passed"
	Failed      CheckStatus = "failed"
	Unknown     CheckStatus = "unknown"
	Unsupported CheckStatus = "unsupported"
)

// CapabilityChecks records independent settings, cancellation and access-boundary observations.
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

// Probe supplies the exact prepared launch and protected roots to a trusted verifier.
type Probe struct {
	Binding     string
	Role        Role
	Runtime     RuntimeIdentity
	Selection   Selection
	Launch      ports.LaunchConfig
	Command     []string
	Permissions PermissionRequirements
}

// PermissionVerifier obtains coordinator-owned capability evidence for a bound launch.
type PermissionVerifier interface {
	Verify(context.Context, Probe) (CapabilityEvidence, error)
}

// Report retains selection, runtime identity and the limits of qualification evidence.
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

// Service prepares exact launches through catalog, runtime and permission checks.
type Service struct {
	Catalog     Catalog
	Agent       func(string) (ports.Agent, error)
	Runtime     RuntimeInspector
	Permissions PermissionVerifier
	Now         func() time.Time
}
