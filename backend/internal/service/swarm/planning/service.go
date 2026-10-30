package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// CreateRun validates all planner output before the existing atomic store write.
// It neither invokes a planner nor admits an attempt.
func (s Service) CreateRun(ctx context.Context, id, requestID string, raw []byte) (domain.SwarmRun, error) {
	if ctx == nil || s.Store == nil {
		return domain.SwarmRun{}, ErrInvalid
	}
	plan, err := ParsePlan(raw)
	if err != nil {
		return domain.SwarmRun{}, err
	}
	return s.Store.CreateSwarmRun(ctx, id, requestID, plan, s.now())
}

// Compose derives exactly one assignment from durable state. The caller cannot
// substitute a contract, tool identity, model, context budget or entry directive.
// This read-only check is not a lease acquisition or authorization to dispatch.
func (s Service) Compose(ctx context.Context, runID, attemptID string) (Packet, error) {
	return s.compose(ctx, runID, attemptID, false)
}

func (s Service) compose(ctx context.Context, runID, attemptID string, dispatch bool) (Packet, error) {
	base, err := s.assignment(ctx, runID, attemptID, dispatch)
	if err != nil {
		return Packet{}, err
	}
	if s.Tools == nil {
		return Packet{}, fmt.Errorf("%w: tool verifier required", ErrStale)
	}
	if err := s.Tools.Verify(ctx, append([]domain.SwarmToolIdentity(nil), base.Contract.Work.Tools...)); err != nil {
		return Packet{}, err
	}
	subject := Subject{ProjectID: base.ProjectID, RunID: runID, TaskID: base.TaskID, AttemptID: attemptID,
		TaskType: base.Contract.Work.TaskType, Backend: string(base.Resolved.Harness)}
	for _, r := range base.Contract.Resources {
		if r.Kind == "path" {
			subject.Paths = append(subject.Paths, r.Key)
		}
	}
	measure := func(included []IncludedContext, omitted []OmittedContext) (int, error) {
		packet, err := renderPacket(base, included, omitted)
		return packet.ByteCount, err
	}
	selection, err := SelectContext(ctx, base.Contract.Work.Context, subject, s.Context, base.MaxBytes, measure)
	if err != nil {
		return Packet{}, err
	}
	packet, err := renderPacket(base, selection.Included, selection.Omitted)
	if err != nil {
		return Packet{}, err
	}
	if packet.ByteCount > base.MaxBytes {
		return Packet{}, ErrBudget
	}
	// Slow context resolution must not deliver a packet for an assignment that
	// was cancelled, revised or taken over while it was being composed.
	if err := s.Tools.Verify(ctx, append([]domain.SwarmToolIdentity(nil), base.Contract.Work.Tools...)); err != nil {
		return Packet{}, err
	}
	current, err := s.assignment(ctx, runID, attemptID, dispatch)
	if err != nil {
		return Packet{}, err
	}
	if domain.SwarmHash(base) != domain.SwarmHash(current) {
		return Packet{}, ErrStale
	}
	return packet, ctx.Err()
}

// ValidateResume regenerates the packet using current state and eligibility.
// A checksum alone proves neither approval nor correspondence to this attempt.
// Tool changes require an explicit new contract/attempt, never silent migration.
func (s Service) ValidateResume(ctx context.Context, packet Packet) error {
	return s.validatePacket(ctx, packet, false)
}

// ValidateDispatch revalidates an already composed packet after the durable
// BeginSwarmDispatch transition. It only reads current eligibility; it neither
// grants a launch nor allows a worker to resume a pending operation.
func (s Service) ValidateDispatch(ctx context.Context, packet Packet) error {
	return s.validatePacket(ctx, packet, true)
}

func (s Service) validatePacket(ctx context.Context, packet Packet, dispatch bool) error {
	if packet.Version != PacketVersion || packet.MaxBytes <= 0 || packet.MaxBytes > MaxPacketBytes ||
		packet.ByteCount <= 0 || packet.ByteCount > packet.MaxBytes || !domain.SwarmValidHash(packet.SHA256, false) {
		return ErrStale
	}
	raw, err := json.Marshal(packet)
	if err != nil || len(raw) != packet.ByteCount {
		return ErrStale
	}
	sum := packet.SHA256
	packet.SHA256 = ""
	if domain.SwarmHash(packet) != sum {
		return ErrStale
	}
	current, err := s.compose(ctx, packet.RunID, packet.AttemptID, dispatch)
	if err != nil {
		return err
	}
	if current.SHA256 != sum {
		return ErrStale
	}
	return nil
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) assignment(ctx context.Context, runID, attemptID string, dispatch bool) (Packet, error) {
	if ctx == nil || s.Store == nil || !domain.SwarmValidID(runID) || !domain.SwarmValidID(attemptID) {
		return Packet{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Packet{}, err
	}
	run, err := s.Store.GetSwarmRun(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	if run.ID != runID || run.Cancelled || run.Plan.ProtocolVersion != PacketVersion {
		return Packet{}, ErrStale
	}
	plan, err := domain.NormalizeSwarmPlan(run.Plan)
	if err != nil {
		return Packet{}, ErrStale
	}
	attempt, err := s.Store.GetSwarmAttempt(ctx, attemptID)
	if err != nil {
		return Packet{}, err
	}
	if attempt.ID != attemptID || attempt.RunID != runID || attempt.TaskID != attempt.Contract.ID ||
		!domain.SwarmValidID(attempt.OperationID) || !domain.SwarmValidID(attempt.Owner) || attempt.Generation <= 0 ||
		!attempt.LeaseUntil.After(s.now()) || !domain.SwarmValidHash(attempt.BaseCommit, true) ||
		!domain.SwarmValidHash(attempt.CapabilityFingerprint, false) ||
		!domain.SwarmValidID(string(attempt.Resolved.Harness)) || !domain.SwarmValidID(attempt.Resolved.Model) ||
		(attempt.ReportedModel != "" && attempt.ReportedModel != attempt.Resolved.Model) {
		return Packet{}, ErrStale
	}
	if (dispatch && attempt.State != "dispatching") || (!dispatch && attempt.State != "reserved" && attempt.State != "running") {
		return Packet{}, domain.ErrSwarmNotReady
	}
	var task domain.SwarmTaskContract
	for _, candidate := range plan.Tasks {
		if candidate.ID == attempt.TaskID {
			task = candidate
			break
		}
	}
	if task.ID == "" || task.Version != attempt.Contract.Version ||
		domain.SwarmHash(task) != attempt.ContractHash || domain.SwarmHash(attempt.Contract) != attempt.ContractHash {
		return Packet{}, ErrStale
	}
	stored, err := s.Store.GetSwarmContract(ctx, runID, task.ID, task.Version)
	if err != nil {
		return Packet{}, err
	}
	if domain.SwarmHash(stored) != attempt.ContractHash {
		return Packet{}, ErrStale
	}
	if err := ValidateWork(task); err != nil {
		return Packet{}, err
	}
	return Packet{Version: PacketVersion, ProjectID: plan.ProjectID, RunID: runID, PlanVersion: plan.Version,
		TaskID: task.ID, TaskVersion: task.Version, AttemptID: attemptID, OperationID: attempt.OperationID,
		Owner: attempt.Owner, Generation: attempt.Generation, BaseCommit: attempt.BaseCommit,
		ContractHash: attempt.ContractHash, CapabilityFingerprint: attempt.CapabilityFingerprint,
		Resolved: attempt.Resolved, Contract: task, MaxBytes: task.Work.ContextByteLimit}, nil
}
