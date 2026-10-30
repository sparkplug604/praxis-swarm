package scheduler

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
)

// These are sealed synthetic packets for the pure mapping boundary. They do
// not supply planning eligibility or permission evidence and never launch AO.
func aoConfigFixture(t *testing.T) (domain.SwarmAttempt, planning.Packet) {
	t.Helper()
	selection := domain.SwarmSelection{Harness: domain.HarnessCodex, Model: "fixture-model", Effort: "high"}
	contract := domain.SwarmTaskContract{ID: "task", Version: 1, Objective: "Implement the bounded task", Agent: selection}
	attempt := domain.SwarmAttempt{
		ID: "attempt", RunID: "run", TaskID: "task", OperationID: "operation", Owner: "coordinator", Generation: 1,
		BaseCommit: strings.Repeat("a", 40), Contract: contract, ContractHash: domain.SwarmHash(contract),
		CapabilityFingerprint: strings.Repeat("b", 64), Resolved: selection, State: "reserved",
		Schedule: &domain.SwarmScheduleBinding{
			PoolID: "pool", PolicyHash: strings.Repeat("c", 64), ProviderKey: "account",
			SourceManifestHash: strings.Repeat("d", 64), WorkspaceIdentityHash: strings.Repeat("e", 64),
			WorkspaceRoot: t.TempDir(), Resources: []domain.SwarmResource{{Namespace: "repo", Kind: "path", Key: "src"}},
		},
	}
	packet := planning.Packet{
		Version: planning.PacketVersion, ProjectID: "project", PlanVersion: 1,
		RunID: attempt.RunID, TaskID: attempt.TaskID, TaskVersion: contract.Version,
		AttemptID: attempt.ID, OperationID: attempt.OperationID, Owner: attempt.Owner, Generation: attempt.Generation,
		BaseCommit: attempt.BaseCommit, ContractHash: attempt.ContractHash, CapabilityFingerprint: attempt.CapabilityFingerprint,
		Resolved: selection, Contract: contract, Entry: "Complete this assignment and stop.", Markdown: "Frozen synthetic assignment.", MaxBytes: planning.MaxPacketBytes,
	}
	return attempt, aoSealFixture(t, packet)
}

func aoSealFixture(t *testing.T, packet planning.Packet) planning.Packet {
	t.Helper()
	packet.SHA256 = strings.Repeat("0", 64)
	packet.ByteCount = 0
	for i := 0; i < 16; i++ {
		raw, err := json.Marshal(packet)
		if err != nil {
			t.Fatal(err)
		}
		if packet.ByteCount == len(raw) {
			packet.SHA256 = ""
			packet.SHA256 = domain.SwarmHash(packet)
			return packet
		}
		packet.ByteCount = len(raw)
	}
	t.Fatal("synthetic packet size did not converge")
	return planning.Packet{}
}

func TestSpawnConfigForPreservesFrozenInputsAndRequestIdentity(t *testing.T) {
	a, p := aoConfigFixture(t)
	cfg, err := SpawnConfigFor(a, p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectID != p.ProjectID || cfg.Kind != domain.KindWorker || cfg.RequestedMode != domain.SessionModeTUI ||
		cfg.Harness != a.Resolved.Harness || cfg.AgentConfig.Model != a.Resolved.Model || cfg.AgentConfig.Effort != a.Resolved.Effort ||
		!cfg.AgentConfigResolved || !cfg.EffortOverride || cfg.AgentAccountID != a.Schedule.ProviderKey ||
		cfg.Prompt != p.Markdown || cfg.ClientRequestID != a.OperationID || !domain.SwarmValidHash(cfg.ClientRequestHash, false) ||
		cfg.DisplayName != "Swarm run / task" || !cfg.KeepDisplayName || !cfg.DisableCosmeticModelCalls || cfg.NameFromPrompt ||
		cfg.AgentConfig.Permissions != ports.PermissionModeDefault || cfg.Async || cfg.TaskPreparation != "" {
		t.Fatalf("unexpected AO inputs: %+v", cfg)
	}
	replay, err := SpawnConfigFor(a, p)
	if err != nil || !reflect.DeepEqual(replay, cfg) {
		t.Fatalf("request preparation changed on replay: %+v, %v", replay, err)
	}
	// Explicit provider-default effort must be represented without inheritance.
	a.Resolved.Effort = ""
	p.Resolved = a.Resolved
	p = aoSealFixture(t, p)
	providerDefault, err := SpawnConfigFor(a, p)
	if err != nil || providerDefault.AgentConfig.Effort != "" || !providerDefault.EffortOverride {
		t.Fatalf("provider-default effort lost: %+v, %v", providerDefault, err)
	}
	if providerDefault.ClientRequestID != cfg.ClientRequestID || providerDefault.ClientRequestHash == cfg.ClientRequestHash {
		t.Fatal("changed selection did not conflict under the original request identity")
	}
}

func TestSpawnConfigForRejectsMismatchedAndCorruptPackets(t *testing.T) {
	cases := map[string]func(*domain.SwarmAttempt, *planning.Packet){
		"run":               func(_ *domain.SwarmAttempt, p *planning.Packet) { p.RunID = "other" },
		"task":              func(_ *domain.SwarmAttempt, p *planning.Packet) { p.TaskID = "other" },
		"attempt":           func(_ *domain.SwarmAttempt, p *planning.Packet) { p.AttemptID = "other" },
		"operation":         func(_ *domain.SwarmAttempt, p *planning.Packet) { p.OperationID = "other" },
		"owner":             func(_ *domain.SwarmAttempt, p *planning.Packet) { p.Owner = "other" },
		"generation":        func(_ *domain.SwarmAttempt, p *planning.Packet) { p.Generation++ },
		"contract version":  func(_ *domain.SwarmAttempt, p *planning.Packet) { p.TaskVersion++ },
		"contract body":     func(_ *domain.SwarmAttempt, p *planning.Packet) { p.Contract.Objective = "other" },
		"base commit":       func(_ *domain.SwarmAttempt, p *planning.Packet) { p.BaseCommit = strings.Repeat("f", 40) },
		"capability":        func(_ *domain.SwarmAttempt, p *planning.Packet) { p.CapabilityFingerprint = strings.Repeat("f", 64) },
		"model":             func(_ *domain.SwarmAttempt, p *planning.Packet) { p.Resolved.Model = "other" },
		"effort":            func(_ *domain.SwarmAttempt, p *planning.Packet) { p.Resolved.Effort = "low" },
		"missing project":   func(_ *domain.SwarmAttempt, p *planning.Packet) { p.ProjectID = "" },
		"missing schedule":  func(a *domain.SwarmAttempt, _ *planning.Packet) { a.Schedule = nil },
		"missing account":   func(a *domain.SwarmAttempt, _ *planning.Packet) { a.Schedule.ProviderKey = "" },
		"reported mismatch": func(a *domain.SwarmAttempt, _ *planning.Packet) { a.ReportedModel = "other" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			a, p := aoConfigFixture(t)
			mutate(&a, &p)
			p = aoSealFixture(t, p) // A valid checksum does not repair wrong identities.
			if _, err := SpawnConfigFor(a, p); !errors.Is(err, planning.ErrStale) {
				t.Fatalf("expected stale identity, got %v", err)
			}
		})
	}
	for _, corrupt := range []func(*planning.Packet){
		func(p *planning.Packet) { p.Markdown += "changed" },
		func(p *planning.Packet) { p.ByteCount++ },
		func(p *planning.Packet) { p.SHA256 = strings.Repeat("f", 64) },
		func(p *planning.Packet) { p.MaxBytes = 1 },
	} {
		a, p := aoConfigFixture(t)
		corrupt(&p)
		if _, err := SpawnConfigFor(a, p); !errors.Is(err, planning.ErrStale) {
			t.Fatalf("corrupt packet accepted: %v", err)
		}
	}
}

func TestSpawnConfigForRequestHashBindsWorkspaceAccountAndPrompt(t *testing.T) {
	for name, mutate := range map[string]func(*domain.SwarmAttempt, *planning.Packet){
		"account": func(a *domain.SwarmAttempt, _ *planning.Packet) { a.Schedule.ProviderKey = "other" },
		"workspace": func(a *domain.SwarmAttempt, _ *planning.Packet) {
			a.Schedule.WorkspaceIdentityHash = strings.Repeat("f", 64)
		},
		"source": func(a *domain.SwarmAttempt, _ *planning.Packet) {
			a.Schedule.SourceManifestHash = strings.Repeat("f", 64)
		},
		"prompt": func(_ *domain.SwarmAttempt, p *planning.Packet) { p.Markdown += "changed" },
	} {
		t.Run(name, func(t *testing.T) {
			a, p := aoConfigFixture(t)
			before, err := SpawnConfigFor(a, p)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&a, &p)
			p = aoSealFixture(t, p)
			after, err := SpawnConfigFor(a, p)
			if err != nil {
				t.Fatal(err)
			}
			if before.ClientRequestID != after.ClientRequestID || before.ClientRequestHash == after.ClientRequestHash {
				t.Fatal("changed request was not bound under the same operation")
			}
		})
	}
}

func TestValidateAOSpawnBoundaryAlwaysRejectsUnenforceableLaunch(t *testing.T) {
	if err := ValidateAOSpawnBoundary(); !errors.Is(err, ErrAOExactWorkspaceUnsupported) {
		t.Fatalf("ordinary AO Spawn was allowed: %v", err)
	}
}
