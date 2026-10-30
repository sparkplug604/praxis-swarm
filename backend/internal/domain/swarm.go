package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

var (
	ErrSwarmInvalid  = errors.New("invalid swarm request")
	ErrSwarmConflict = errors.New("swarm request conflicts with durable state")
	ErrSwarmStale    = errors.New("stale swarm contract or lease")
	ErrSwarmNotReady = errors.New("swarm task is not ready")
	ErrSwarmCapacity = errors.New("swarm capacity or usage reservation unavailable")
	ErrSwarmNotFound = errors.New("swarm record not found")
)

// Resource namespaces must identify the same repository/invariant across runs.
// Paths are normalized relative scopes, not proof of filesystem containment.
// The scheduler must resolve filesystem aliases before constructing these keys.
type SwarmResource struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"` // path or semantic
	Key       string `json:"key"`
}

type SwarmSelection struct {
	Harness AgentHarness `json:"harness"`
	Model   string       `json:"model"`
	Effort  string       `json:"effort"`
}

type SwarmDependency struct {
	TaskID  string `json:"taskId"`
	Version int64  `json:"version"`
}

type SwarmTaskContract struct {
	Role          string            `json:"role,omitempty"` // legacy omission means worker
	ID            string            `json:"id"`
	Version       int64             `json:"version"`
	Objective     string            `json:"objective"`
	OracleHash    string            `json:"oracleHash"`
	Agent         SwarmSelection    `json:"agent"`
	Dependencies  []SwarmDependency `json:"dependencies"`
	Resources     []SwarmResource   `json:"resources"`
	ReserveTokens int64             `json:"reserveTokens"`
	// Omitted for legacy contracts so their hashes remain unchanged. SWM-03
	// preparation requires Work and persists it inside the existing JSON snapshot.
	Work *SwarmWorkSpec `json:"work,omitempty"`
}

type SwarmPlan struct {
	ProtocolVersion int64               `json:"protocolVersion"`
	Version         int64               `json:"version"`
	ProjectID       ProjectID           `json:"projectId"`
	SourceCommit    string              `json:"sourceCommit"`
	Objective       string              `json:"objective"`
	BudgetBucket    string              `json:"budgetBucket"`
	TokenLimit      int64               `json:"tokenLimit"`
	MaxActive       int64               `json:"maxActive"`
	Tasks           []SwarmTaskContract `json:"tasks"`
}

type SwarmRun struct {
	ID              string    `json:"id"`
	RequestID       string    `json:"requestId"`
	Plan            SwarmPlan `json:"plan"`
	Cancelled       bool      `json:"cancelled"`
	IntegrationHead string    `json:"integrationHead"`
	CreatedAt       time.Time `json:"createdAt"`
}

type SwarmClaim struct {
	Schedule              *SwarmScheduleBinding `json:"schedule,omitempty"`
	Routing               *SwarmRouteBinding    `json:"routing,omitempty"`
	RunID                 string                `json:"runId"`
	TaskID                string                `json:"taskId"`
	Version               int64                 `json:"version"`
	RequestID             string                `json:"requestId"`
	Owner                 string                `json:"owner"`
	Resolved              SwarmSelection        `json:"resolved"`
	CapabilityFingerprint string                `json:"capabilityFingerprint"`
}

// Identity and the contract snapshot never change. Mutable execution facts are
// generation-fenced; expiry alone never releases reservations or retries launch.
type SwarmAttempt struct {
	Schedule              *SwarmScheduleBinding `json:"schedule,omitempty"`
	Routing               *SwarmRouteBinding    `json:"routing,omitempty"`
	ID                    string                `json:"id"`
	RunID                 string                `json:"runId"`
	TaskID                string                `json:"taskId"`
	RequestID             string                `json:"requestId"`
	OperationID           string                `json:"operationId"`
	BaseCommit            string                `json:"baseCommit"`
	Contract              SwarmTaskContract     `json:"contract"`
	ContractHash          string                `json:"contractHash"`
	Resolved              SwarmSelection        `json:"resolved"`
	CapabilityFingerprint string                `json:"capabilityFingerprint"`
	State                 string                `json:"state"`
	Owner                 string                `json:"owner"`
	Generation            int64                 `json:"generation"`
	LeaseUntil            time.Time             `json:"leaseUntil"`
	SessionID             string                `json:"sessionId,omitempty"`
	ReportedModel         string                `json:"reportedModel,omitempty"` // empty is unknown
	SubmissionCommit      string                `json:"submissionCommit,omitempty"`
	CreatedAt             time.Time             `json:"createdAt"`
}

type SwarmFence struct {
	AttemptID  string
	Owner      string
	Generation int64
}

type SwarmUsage struct {
	AttemptID    string `json:"attemptId"`
	ReceiptID    string `json:"receiptId"`
	ActualTokens int64  `json:"actualTokens"`
}

type SwarmBudget struct {
	Bucket   string `json:"bucket"`
	Limit    int64  `json:"limit"`
	Reserved int64  `json:"reserved"`
	Spent    int64  `json:"spent"`
}

// Evidence is accepted only from coordinator-owned verifier paths, never from
// worker claims. Storage validates bindings; it does not execute the oracle.
type SwarmEvidence struct {
	ID           string `json:"id"`
	AttemptID    string `json:"attemptId"`
	Kind         string `json:"kind"` // review or evaluation
	ActorID      string `json:"actorId"`
	Commit       string `json:"commit"`
	ContractHash string `json:"contractHash"`
	OracleHash   string `json:"oracleHash"`
	Verdict      string `json:"verdict"` // pass, fail, inconclusive
	ArtifactHash string `json:"artifactHash"`
}

// Integration records bind combined-result checks to the precise integration
// commit. The later integration controller owns real review, evaluation and Git
// mutation. This record alone never performs a Git merge.
type SwarmIntegration struct {
	ID                     string   `json:"id"`
	RunID                  string   `json:"runId"`
	BaseCommit             string   `json:"baseCommit"`
	Commit                 string   `json:"commit"`
	AttemptIDs             []string `json:"attemptIds"`
	ReviewID               string   `json:"reviewId"`
	EvaluatorID            string   `json:"evaluatorId"`
	ReviewArtifactHash     string   `json:"reviewArtifactHash"`
	EvaluationArtifactHash string   `json:"evaluationArtifactHash"`
}

func SwarmHash(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	} // all callers use JSON-safe concrete contracts
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func SwarmValidHash(s string, commit bool) bool {
	if len(s) != 64 && !(commit && len(s) == 40) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

func SwarmValidID(s string) bool {
	return s != "" && len(s) <= 256 && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\n\r\t")
}

func SwarmResourcesConflict(a, b SwarmResource) bool {
	if a.Namespace != b.Namespace || a.Kind != b.Kind {
		return false
	}
	if a.Kind == "semantic" {
		return a.Key == b.Key
	}
	return a.Key == "." || b.Key == "." || a.Key == b.Key ||
		strings.HasPrefix(a.Key, b.Key+"/") || strings.HasPrefix(b.Key, a.Key+"/")
}

// NormalizeSwarmPlan returns an independent, deterministically ordered graph.
// Rejects ambiguous scopes, missing/stale dependencies and cycles before writes.
func NormalizeSwarmPlan(input SwarmPlan) (SwarmPlan, error) {
	data, _ := json.Marshal(input)
	var p SwarmPlan
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("%w: plan", ErrSwarmInvalid)
	}
	bad := func(reason string) (SwarmPlan, error) {
		return SwarmPlan{}, fmt.Errorf("%w: %s", ErrSwarmInvalid, reason)
	}
	if p.ProtocolVersion != 1 || p.Version < 1 || !SwarmValidID(string(p.ProjectID)) ||
		!SwarmValidHash(p.SourceCommit, true) || strings.TrimSpace(p.Objective) == "" ||
		!SwarmValidID(p.BudgetBucket) || p.TokenLimit <= 0 || p.MaxActive < 1 || p.MaxActive > 1000 ||
		len(p.Tasks) == 0 || len(p.Tasks) > 1000 {
		return bad("plan metadata or limits")
	}
	tasks := make(map[string]*SwarmTaskContract, len(p.Tasks))
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if t.Role != "" && !SwarmValidRole(t.Role) {
			return bad("task role")
		}
		if !SwarmValidID(t.ID) || tasks[t.ID] != nil || t.Version < 1 ||
			strings.TrimSpace(t.Objective) == "" || !SwarmValidHash(t.OracleHash, false) ||
			!SwarmValidID(string(t.Agent.Harness)) || !SwarmValidID(t.Agent.Model) ||
			t.ReserveTokens <= 0 || t.ReserveTokens > p.TokenLimit || len(t.Resources) == 0 || len(t.Resources) > 256 || len(t.Dependencies) > 1000 {
			return bad("invalid or duplicate task " + t.ID)
		}
		tasks[t.ID] = t
		for _, r := range t.Resources {
			if !SwarmValidID(r.Namespace) || !SwarmValidID(r.Key) {
				return bad("resource identity")
			}
			switch r.Kind {
			case "path":
				if path.IsAbs(r.Key) || path.Clean(r.Key) != r.Key || r.Key == ".." || strings.HasPrefix(r.Key, "../") || strings.ContainsAny(r.Key, "\\*?[]:") {
					return bad("resource path")
				}
			case "semantic":
			default:
				return bad("resource kind")
			}
		}
		sort.Slice(t.Resources, func(i, j int) bool {
			a, b := t.Resources[i], t.Resources[j]
			return a.Namespace+"\x00"+a.Kind+"\x00"+a.Key < b.Namespace+"\x00"+b.Kind+"\x00"+b.Key
		})
		for j := 1; j < len(t.Resources); j++ {
			if t.Resources[j] == t.Resources[j-1] {
				return bad("duplicate resource")
			}
		}
		sort.Slice(t.Dependencies, func(i, j int) bool { return t.Dependencies[i].TaskID < t.Dependencies[j].TaskID })
	}
	for _, t := range p.Tasks {
		seen := map[string]bool{}
		for _, d := range t.Dependencies {
			if tasks[d.TaskID] == nil || tasks[d.TaskID].Version != d.Version || seen[d.TaskID] {
				return bad("missing, duplicate or stale dependency")
			}
			seen[d.TaskID] = true
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return false
		}
		if done[id] {
			return true
		}
		visiting[id] = true
		for _, d := range tasks[id].Dependencies {
			if !visit(d.TaskID) {
				return false
			}
		}
		visiting[id] = false
		done[id] = true
		return true
	}
	for id := range tasks {
		if !visit(id) {
			return bad("dependency cycle")
		}
	}
	sort.Slice(p.Tasks, func(i, j int) bool { return p.Tasks[i].ID < p.Tasks[j].ID })
	return p, nil
}
