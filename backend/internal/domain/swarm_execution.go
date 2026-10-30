package domain

import (
	"path/filepath"
	"strings"
	"time"
)

// SwarmExecutionReceipt binds the final prepared launch to one durable attempt.
// It contains coordinator assertions, not proof that a runtime enforces access.
// Raw arguments, environment values and credentials must never enter this record.
type SwarmExecutionReceipt struct {
	Version               int64     `json:"version"`
	AttemptID             string    `json:"attemptId"`
	OperationID           string    `json:"operationId"`
	Owner                 string    `json:"owner"`
	SessionID             string    `json:"sessionId"`
	CapabilityFingerprint string    `json:"capabilityFingerprint"`
	QualificationBinding  string    `json:"qualificationBinding"`
	PacketHash            string    `json:"packetHash"`
	WorkspaceIdentityHash string    `json:"workspaceIdentityHash"`
	SourceManifestHash    string    `json:"sourceManifestHash"`
	WorkspaceRoot         string    `json:"workspaceRoot"`
	ConfigHash            string    `json:"configHash"`
	PermissionReceiptID   string    `json:"permissionReceiptId"`
	EvidenceMode          string    `json:"evidenceMode"`
	Generation            int64     `json:"generation"`
	ExpiresAt             time.Time `json:"expiresAt"`
	CreatedAt             time.Time `json:"createdAt"`
}

// SwarmExecutionResult is an immutable observation of the first launch call.
// A possible or unknown result retains uncertainty; it never permits relaunch.
type SwarmExecutionResult struct {
	AttemptID         string    `json:"attemptId"`
	ReceiptHash       string    `json:"receiptHash"`
	SessionID         string    `json:"sessionId"`
	HandleID          string    `json:"handleId"`
	AdditionalHandles []string  `json:"additionalHandles,omitempty"`
	Outcome           string    `json:"outcome"`
	RuntimeEffect     string    `json:"runtimeEffect,omitempty"`
	RuntimeCleanup    string    `json:"runtimeCleanup,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}

// ValidateSwarmExecutionReceipt checks launch bindings, evidence mode and timestamp bounds.
func ValidateSwarmExecutionReceipt(r SwarmExecutionReceipt) error {
	if r.Version != 1 || r.Generation < 1 || !swarmExecutionTime(r.CreatedAt) ||
		!swarmExecutionTime(r.ExpiresAt) || !r.ExpiresAt.After(r.CreatedAt) ||
		(r.EvidenceMode != "test" && r.EvidenceMode != "live") ||
		!filepath.IsAbs(r.WorkspaceRoot) || filepath.Clean(r.WorkspaceRoot) != r.WorkspaceRoot ||
		strings.ContainsRune(r.WorkspaceRoot, '\x00') {
		return ErrSwarmInvalid
	}
	for _, id := range []string{r.AttemptID, r.OperationID, r.Owner, r.SessionID, r.PermissionReceiptID} {
		if !SwarmValidID(id) {
			return ErrSwarmInvalid
		}
	}
	for _, hash := range []string{r.CapabilityFingerprint, r.QualificationBinding, r.PacketHash,
		r.WorkspaceIdentityHash, r.SourceManifestHash, r.ConfigHash} {
		if !SwarmValidHash(hash, false) {
			return ErrSwarmInvalid
		}
	}
	return nil
}

// ValidateSwarmExecutionResult checks immutable launch outcomes and recorded handle identities.
func ValidateSwarmExecutionResult(r SwarmExecutionResult) error {
	if !SwarmValidID(r.AttemptID) || !SwarmValidID(r.SessionID) ||
		!SwarmValidHash(r.ReceiptHash, false) || !swarmExecutionTime(r.CreatedAt) ||
		(r.HandleID != "" && !SwarmValidID(r.HandleID)) {
		return ErrSwarmInvalid
	}
	seen := map[string]bool{r.HandleID: true}
	for _, handle := range r.AdditionalHandles {
		if !SwarmValidID(handle) || seen[handle] {
			return ErrSwarmInvalid
		}
		seen[handle] = true
	}
	switch r.RuntimeEffect {
	case "", "none", "possible", "applied":
	default:
		return ErrSwarmInvalid
	}
	switch r.RuntimeCleanup {
	case "", "not_attempted", "succeeded", "failed":
	default:
		return ErrSwarmInvalid
	}
	switch r.Outcome {
	case "created":
		if r.HandleID == "" {
			return ErrSwarmInvalid
		}
	case "possible", "unknown":
	case "no_effect":
		if r.HandleID != "" || len(r.AdditionalHandles) != 0 {
			return ErrSwarmInvalid
		}
	default:
		return ErrSwarmInvalid
	}
	return nil
}

func swarmExecutionTime(t time.Time) bool {
	// JSON is the durable representation, so reject timestamps it cannot encode.
	_, err := t.MarshalJSON()
	return !t.IsZero() && err == nil
}
