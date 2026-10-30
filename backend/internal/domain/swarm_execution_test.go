package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func executionReceiptForTest() SwarmExecutionReceipt {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	return SwarmExecutionReceipt{Version: 1, AttemptID: "attempt", OperationID: "operation", Owner: "owner", SessionID: "session",
		CapabilityFingerprint: hash, QualificationBinding: hash, PacketHash: hash, WorkspaceIdentityHash: hash,
		SourceManifestHash: hash, WorkspaceRoot: "/isolated/workspace", ConfigHash: hash, PermissionReceiptID: "permission",
		EvidenceMode: "test", Generation: 1, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
}

func TestSwarmExecutionReceiptValidation(t *testing.T) {
	valid := executionReceiptForTest()
	for _, mode := range []string{"test", "live"} {
		r := valid
		r.EvidenceMode = mode
		if err := ValidateSwarmExecutionReceipt(r); err != nil {
			t.Fatalf("valid %s receipt: %v", mode, err)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*SwarmExecutionReceipt)
	}{
		{"version", func(r *SwarmExecutionReceipt) { r.Version = 2 }},
		{"generation", func(r *SwarmExecutionReceipt) { r.Generation = 0 }},
		{"attempt", func(r *SwarmExecutionReceipt) { r.AttemptID = "" }},
		{"operation", func(r *SwarmExecutionReceipt) { r.OperationID = "" }},
		{"owner", func(r *SwarmExecutionReceipt) { r.Owner = "" }},
		{"session", func(r *SwarmExecutionReceipt) { r.SessionID = "\ninvalid" }},
		{"permission", func(r *SwarmExecutionReceipt) { r.PermissionReceiptID = "" }},
		{"capability", func(r *SwarmExecutionReceipt) { r.CapabilityFingerprint = strings.Repeat("A", 64) }},
		{"qualification", func(r *SwarmExecutionReceipt) { r.QualificationBinding = "" }},
		{"packet", func(r *SwarmExecutionReceipt) { r.PacketHash = strings.Repeat("a", 40) }},
		{"workspace identity", func(r *SwarmExecutionReceipt) { r.WorkspaceIdentityHash = "" }},
		{"manifest", func(r *SwarmExecutionReceipt) { r.SourceManifestHash = "" }},
		{"config", func(r *SwarmExecutionReceipt) { r.ConfigHash = "" }},
		{"relative workspace", func(r *SwarmExecutionReceipt) { r.WorkspaceRoot = "relative" }},
		{"unclean workspace", func(r *SwarmExecutionReceipt) { r.WorkspaceRoot = "/tmp/../work" }},
		{"nul workspace", func(r *SwarmExecutionReceipt) { r.WorkspaceRoot = "/tmp/\x00" }},
		{"mode", func(r *SwarmExecutionReceipt) { r.EvidenceMode = "qualified" }},
		{"zero created time", func(r *SwarmExecutionReceipt) { r.CreatedAt = time.Time{} }},
		{"invalid expiry", func(r *SwarmExecutionReceipt) { r.ExpiresAt = r.CreatedAt }},
		{"unencodable time", func(r *SwarmExecutionReceipt) { r.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.edit(&r)
			if err := ValidateSwarmExecutionReceipt(r); !errors.Is(err, ErrSwarmInvalid) {
				t.Fatalf("accepted invalid receipt: %v", err)
			}
		})
	}
}

func TestSwarmExecutionResultValidation(t *testing.T) {
	receipt := executionReceiptForTest()
	valid := SwarmExecutionResult{AttemptID: receipt.AttemptID, ReceiptHash: SwarmHash(receipt), SessionID: receipt.SessionID, CreatedAt: receipt.CreatedAt}
	for _, tc := range []struct {
		outcome, handle string
		valid           bool
	}{
		{"created", "handle", true}, {"created", "", false}, {"possible", "handle", true}, {"possible", "", true},
		{"unknown", "handle", true}, {"unknown", "", true}, {"no_effect", "", true}, {"no_effect", "handle", false},
		{"bogus", "", false}, {"created", "\ninvalid", false},
	} {
		t.Run(tc.outcome+"/"+tc.handle, func(t *testing.T) {
			r := valid
			r.Outcome, r.HandleID = tc.outcome, tc.handle
			if err := ValidateSwarmExecutionResult(r); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
	valid.Outcome = "unknown"
	withHandles := valid
	withHandles.HandleID, withHandles.AdditionalHandles = "first", []string{"second", "third"}
	if err := ValidateSwarmExecutionResult(withHandles); err != nil {
		t.Fatalf("valid additional runtime ownership: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*SwarmExecutionResult)
	}{
		{"attempt", func(r *SwarmExecutionResult) { r.AttemptID = "" }},
		{"receipt hash", func(r *SwarmExecutionResult) { r.ReceiptHash = "" }},
		{"session", func(r *SwarmExecutionResult) { r.SessionID = "" }},
		{"time", func(r *SwarmExecutionResult) { r.CreatedAt = time.Time{} }},
		{"runtime effect", func(r *SwarmExecutionResult) { r.RuntimeEffect = "bad" }},
		{"runtime cleanup", func(r *SwarmExecutionResult) { r.RuntimeCleanup = "bad" }},
		{"empty additional handle", func(r *SwarmExecutionResult) { r.AdditionalHandles = []string{""} }},
		{"invalid additional handle", func(r *SwarmExecutionResult) { r.AdditionalHandles = []string{"bad\nhandle"} }},
		{"duplicate additional handle", func(r *SwarmExecutionResult) { r.AdditionalHandles = []string{"same", "same"} }},
		{"primary repeated as additional", func(r *SwarmExecutionResult) { r.HandleID = "same"; r.AdditionalHandles = []string{"same"} }},
		{"no effect with additional handle", func(r *SwarmExecutionResult) { r.Outcome = "no_effect"; r.AdditionalHandles = []string{"possible"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.edit(&r)
			if err := ValidateSwarmExecutionResult(r); !errors.Is(err, ErrSwarmInvalid) {
				t.Fatalf("accepted invalid result: %v", err)
			}
		})
	}
}
