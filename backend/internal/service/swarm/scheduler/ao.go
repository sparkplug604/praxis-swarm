package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
)

// ErrAOExactWorkspaceUnsupported identifies the remaining session-manager
// integration boundary. Ordinary AO Spawn provisions its own workspace and
// cannot enforce a preprovisioned workspace or qualify the final launch in it.
var ErrAOExactWorkspaceUnsupported = errors.New("AO Spawn does not support an exact preprovisioned workspace and post-provision live qualification")

// ValidateAOSpawnBoundary fails closed until AO can bind its actual workspace,
// resolved launch configuration and enforced process permissions to the live
// qualification receipt. TaskPreparation is an optional hint, not that binding.
func ValidateAOSpawnBoundary() error {
	return ErrAOExactWorkspaceUnsupported
}

// SpawnConfigFor prepares diagnostic AO inputs only; it never authorizes or
// starts a session. A future dispatcher must first revalidate the packet through
// planning.Service.ValidateResume and close ValidateAOSpawnBoundary. The pure
// check here cannot establish current project/plan state, eligibility or leases.
//
// AO atomically assigns ClientRequestID to one row and replays committed rows
// with the same hash. An incomplete request is an error, potentially after a
// launch; prelaunch rollback can remove a seed row. Scheduler operation state
// must therefore remain authoritative for reconciliation and duplicate prevention.
func SpawnConfigFor(attempt domain.SwarmAttempt, packet planning.Packet) (ports.SpawnConfig, error) {
	if attempt.Schedule == nil || domain.ValidateSwarmScheduleBinding(*attempt.Schedule) != nil ||
		!domain.SwarmValidID(attempt.ID) || !domain.SwarmValidID(attempt.RunID) ||
		!domain.SwarmValidID(attempt.TaskID) || !domain.SwarmValidID(attempt.OperationID) ||
		!domain.SwarmValidID(attempt.Owner) || attempt.Generation <= 0 ||
		!domain.SwarmValidHash(attempt.BaseCommit, true) ||
		!domain.SwarmValidHash(attempt.CapabilityFingerprint, false) ||
		!domain.SwarmValidID(string(attempt.Resolved.Harness)) || !domain.SwarmValidID(attempt.Resolved.Model) ||
		(attempt.ReportedModel != "" && attempt.ReportedModel != attempt.Resolved.Model) {
		return ports.SpawnConfig{}, fmt.Errorf("%w: missing or invalid AO attempt identity", planning.ErrStale)
	}
	if packet.Version != planning.PacketVersion || !domain.SwarmValidID(string(packet.ProjectID)) || packet.PlanVersion <= 0 ||
		packet.RunID != attempt.RunID || packet.TaskID != attempt.TaskID || packet.AttemptID != attempt.ID ||
		packet.OperationID != attempt.OperationID || packet.Owner != attempt.Owner || packet.Generation != attempt.Generation ||
		packet.BaseCommit != attempt.BaseCommit || packet.ContractHash != attempt.ContractHash ||
		packet.CapabilityFingerprint != attempt.CapabilityFingerprint || packet.Resolved != attempt.Resolved ||
		packet.TaskID != attempt.Contract.ID || packet.TaskVersion != attempt.Contract.Version || packet.TaskVersion <= 0 ||
		domain.SwarmHash(attempt.Contract) != attempt.ContractHash || domain.SwarmHash(packet.Contract) != attempt.ContractHash {
		return ports.SpawnConfig{}, fmt.Errorf("%w: packet does not bind this AO attempt", planning.ErrStale)
	}
	if packet.MaxBytes <= 0 || packet.MaxBytes > planning.MaxPacketBytes || packet.ByteCount <= 0 ||
		packet.ByteCount > packet.MaxBytes || !domain.SwarmValidHash(packet.SHA256, false) ||
		strings.TrimSpace(packet.Entry) == "" || strings.TrimSpace(packet.Markdown) == "" {
		return ports.SpawnConfig{}, fmt.Errorf("%w: invalid AO packet envelope", planning.ErrStale)
	}
	raw, err := json.Marshal(packet)
	if err != nil || len(raw) != packet.ByteCount {
		return ports.SpawnConfig{}, fmt.Errorf("%w: AO packet byte count changed", planning.ErrStale)
	}
	packetHash := packet.SHA256
	packet.SHA256 = ""
	if domain.SwarmHash(packet) != packetHash {
		return ports.SpawnConfig{}, fmt.Errorf("%w: AO packet checksum changed", planning.ErrStale)
	}
	cfg := ports.SpawnConfig{
		ProjectID: packet.ProjectID, Kind: domain.KindWorker,
		Harness: attempt.Resolved.Harness, Prompt: packet.Markdown,
		AgentConfig:         ports.AgentConfig{Model: attempt.Resolved.Model, Effort: attempt.Resolved.Effort, Permissions: ports.PermissionModeDefault},
		AgentConfigResolved: true, EffortOverride: true,
		AgentAccountID: attempt.Schedule.ProviderKey, RequestedMode: domain.SessionModeTUI,
		ClientRequestID: attempt.OperationID,
		DisplayName:     fmt.Sprintf("Swarm %s / %s", attempt.RunID, attempt.TaskID),
		KeepDisplayName: true, DisableCosmeticModelCalls: true,
	}
	// Binding the schedule includes the exact workspace/source identities even
	// though Spawn cannot yet consume or enforce them. Never treat this digest
	// as permission evidence. Launch options are explicit because AgentConfig
	// omits them from JSON.
	cfg.ClientRequestHash = domain.SwarmHash(struct {
		Version       int64
		PacketHash    string
		Schedule      domain.SwarmScheduleBinding
		Config        ports.SpawnConfig
		LaunchOptions domain.AgentLaunchOptions
	}{1, packetHash, *attempt.Schedule, cfg, cfg.AgentConfig.Launch})
	return cfg, nil
}
