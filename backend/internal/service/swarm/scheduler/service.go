// Package scheduler admits fair batches and fences launch/reconciliation. The
// concrete AO exact-workspace launch boundary remains unavailable; test launchers
// exercise this controller without qualifying any provider or sandbox.
package scheduler

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/qualification"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/routing"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
)

// Store supplies generation-fenced dispatch and completion transitions.
type Store interface {
	GetSwarmRun(context.Context, string) (domain.SwarmRun, error)
	GetSwarmContract(context.Context, string, string, int64) (domain.SwarmTaskContract, error)
	GetSwarmAttempt(context.Context, string) (domain.SwarmAttempt, error)
	GetSwarmAttemptByRequest(context.Context, string) (domain.SwarmAttempt, error)
	BeginSwarmDispatch(context.Context, domain.SwarmFence, time.Time) (domain.SwarmAttempt, bool, error)
	ConfirmSwarmRunning(context.Context, domain.SwarmFence, string, string, time.Time) error
	FinishSwarmAttempt(context.Context, domain.SwarmFence, string, string, bool, time.Time) error
}

// Admission reserves an eligible task under the routing and concurrency policy.
type Admission interface {
	Admit(context.Context, routing.AdmissionRequest) (domain.SwarmAttempt, routing.RouteAdvice, error)
}

// Composer freezes task packets and revalidates their identities before dispatch.
type Composer interface {
	Compose(context.Context, string, string) (planning.Packet, error)
	ValidateResume(context.Context, planning.Packet) error
}

// Preparer binds the complete task prompt after composing a packet that embeds
// the stable capability profile. The final binding is separate from that profile.
type Preparer interface {
	Prepare(context.Context, domain.SwarmAttempt, planning.Packet, workspace.Snapshot) (qualification.Preparation, error)
}

// Runtime must enforce the exact prepared workspace/command/permissions. A
// successful Start returns promptly after launch, not after task completion.
// Errors are ambiguous: a process might already exist. Observe must look up the
// durable operation and its exact runtime generation, never guess from a PID or
// a display status. Terminal absence must fence out every pending or future
// Start for that operation; a momentarily absent lookup is insufficient. The
// local exclusion below cannot replace that guarantee across coordinators.
// There is deliberately no ordinary AO Spawn adapter here.
type Runtime interface {
	Mode() qualification.EvidenceMode
	Start(context.Context, Launch) (Observation, error)
	Observe(context.Context, domain.SwarmAttempt) (Observation, error)
}

// Launch binds an admitted attempt to its packet, workspace and final preparation.
type Launch struct {
	Attempt     domain.SwarmAttempt
	Packet      planning.Packet
	Workspace   workspace.Snapshot
	Preparation qualification.Preparation
}

// Observation records operation ownership and whether runtime absence is terminal.
type Observation struct {
	OperationID   string
	SessionID     string
	ReportedModel string
	State         string // alive, absent, unknown
	Exact         bool   // coordinator-observed operation/runtime-generation match
	Terminal      bool   // absence is final: no owned process or pending/future launch
}

// Candidate pairs an admission request with its independently captured workspace.
type Candidate struct {
	Admission routing.AdmissionRequest
	Workspace workspace.Snapshot
}

// Result retains admission advice and the outcome of one scheduling candidate.
type Result struct {
	RequestID string
	Attempt   domain.SwarmAttempt
	Advice    routing.RouteAdvice
	Err       error
}

// Service schedules bounded fair batches and reconciles owned runtime observations.
type Service struct {
	// Owner is the coordinator lease identity, not caller-supplied authority.
	Owner     string
	Store     Store
	Admission Admission
	Composer  Composer
	Preparer  Preparer
	Runtime   Runtime
	Now       func() time.Time
	// Bounds preparation/admission calls independently of the worker lifetime.
	OperationTimeout time.Duration
	MaxPerPass       int
	mu               sync.Mutex
	lastProject      string
	operationMu      sync.Mutex
	operations       map[string]bool
}

// Tick rotates project queues, attempts each eligible candidate at most once,
// and starts admitted attempts concurrently. A rejected candidate never ends the
// pass. Store admission remains authoritative for dependencies/resources/limits.
// Fairness is local to this coordinator and resets on restart; durable identity
// and capacity do not. Callers must supply candidates from all ready projects.
func (s *Service) Tick(ctx context.Context, candidates []Candidate) ([]Result, error) {
	if ctx == nil || !domain.SwarmValidID(s.Owner) || s.Store == nil || s.Admission == nil || s.Composer == nil || s.Preparer == nil || s.Runtime == nil || s.OperationTimeout <= 0 ||
		s.MaxPerPass < 1 || s.MaxPerPass > 1000 || len(candidates) > 10000 {
		return nil, domain.ErrSwarmInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The only functional execution path today is an explicit test runtime.
	// Known unavailable live dispatch must not consume durable reservations.
	if s.Runtime.Mode() == qualification.LiveEvidence {
		return nil, errors.Join(qualification.ErrUnverified, ValidateAOSpawnBoundary())
	}
	if s.Runtime.Mode() != qualification.TestEvidence {
		return nil, qualification.ErrUnverified
	}
	s.mu.Lock()
	groups := map[string][]Candidate{}
	var results []Result
	for _, c := range candidates {
		if c.Admission.Claim.Owner != s.Owner {
			results = append(results, Result{RequestID: c.Admission.Claim.RequestID, Err: domain.ErrSwarmStale})
			continue
		}
		probe, cancel := context.WithTimeout(ctx, s.OperationTimeout)
		run, err := s.Store.GetSwarmRun(probe, c.Admission.Claim.RunID)
		cancel()
		if err != nil {
			results = append(results, Result{RequestID: c.Admission.Claim.RequestID, Err: err})
			continue
		}
		groups[string(run.Plan.ProjectID)] = append(groups[string(run.Plan.ProjectID)], c)
	}
	ordered := fairOrder(groups, s.lastProject)
	type pendingResult struct {
		index int
		done  chan error
	}
	var pending []pendingResult
	seenAttempts := map[string]bool{}
	admitted := 0
	for _, entry := range ordered {
		if admitted >= s.MaxPerPass || ctx.Err() != nil {
			break
		}
		c := entry.candidate
		probe, cancel := context.WithTimeout(ctx, s.OperationTimeout)
		err := s.bindCandidate(probe, &c)
		var a domain.SwarmAttempt
		var advice routing.RouteAdvice
		if err == nil {
			a, advice, err = s.Admission.Admit(probe, c.Admission)
		}
		if err == nil && a.Owner != c.Admission.Claim.Owner {
			err = domain.ErrSwarmStale
		}
		cancel()
		results = append(results, Result{RequestID: c.Admission.Claim.RequestID, Attempt: a, Advice: advice, Err: err})
		if err != nil {
			continue
		}
		// Replays recover the durable operation without charging another pass
		// admission. Only a reserved attempt still needs a launch decision.
		if a.State != "reserved" || seenAttempts[a.ID] {
			continue
		}
		seenAttempts[a.ID] = true
		admitted++
		s.lastProject = entry.project
		index := len(results) - 1
		// A separate result channel avoids concurrent access while appending to
		// the slice. Each launched goroutine writes only its own result object.
		done := make(chan error, 1)
		pending = append(pending, pendingResult{index, done})
		go func(id string, snapshot workspace.Snapshot) {
			launchCtx, cancel := context.WithTimeout(ctx, s.OperationTimeout)
			defer cancel()
			done <- s.Dispatch(launchCtx, id, snapshot)
		}(a.ID, c.Workspace)
	}
	s.mu.Unlock()
	for _, p := range pending {
		results[p.index].Err = <-p.done
	}
	return results, ctx.Err()
}

type orderedCandidate struct {
	project   string
	candidate Candidate
}

func fairOrder(groups map[string][]Candidate, after string) []orderedCandidate {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	start := sort.SearchStrings(keys, after)
	if start < len(keys) && keys[start] == after {
		start++
	}
	if len(keys) > 0 {
		start %= len(keys)
		keys = append(append([]string{}, keys[start:]...), keys[:start]...)
	}
	var out []orderedCandidate
	for round := 0; ; round++ {
		added := false
		for _, key := range keys {
			if round < len(groups[key]) {
				out = append(out, orderedCandidate{key, groups[key][round]})
				added = true
			}
		}
		if !added {
			return out
		}
	}
}

func (s *Service) bindWorkspace(ctx context.Context, c *Candidate) error {
	if c.Admission.Claim.Schedule == nil {
		return domain.ErrSwarmInvalid
	}
	if err := workspace.ValidateForLaunch(ctx, c.Workspace); err != nil {
		return err
	}
	run, err := s.Store.GetSwarmRun(ctx, c.Admission.Claim.RunID)
	if err != nil {
		return err
	}
	if c.Workspace.Source.Head != run.IntegrationHead {
		return domain.ErrSwarmStale
	}
	task, err := s.Store.GetSwarmContract(ctx, run.ID, c.Admission.Claim.TaskID, c.Admission.Claim.Version)
	if err != nil {
		return err
	}
	resources, err := workspace.ResolveResources(ctx, c.Workspace.Source.Root.Path, task.Resources)
	if err != nil {
		return err
	}
	b := *c.Admission.Claim.Schedule
	b.Resources = resources
	b.SourceManifestHash = c.Workspace.ManifestHash
	b.WorkspaceIdentityHash = c.Workspace.IdentityHash
	b.WorkspaceRoot = c.Workspace.Destination.Path
	c.Admission.Claim.Schedule = &b
	return domain.ValidateSwarmScheduleBinding(b)
}

// Recover the original binding before touching mutable source or worker files.
// Running/finished replays need no pristine workspace. Reserved replays still
// pass Dispatch's fresh filesystem and packet checks before any launch.
func (s *Service) bindCandidate(ctx context.Context, c *Candidate) error {
	a, err := s.Store.GetSwarmAttemptByRequest(ctx, c.Admission.Claim.RequestID)
	if errors.Is(err, domain.ErrSwarmNotFound) {
		return s.bindWorkspace(ctx, c)
	}
	if err != nil {
		return err
	}
	if a.Schedule == nil || c.Admission.Claim.Schedule == nil {
		return domain.ErrSwarmConflict
	}
	prior, requested := a.Schedule, *c.Admission.Claim.Schedule
	if prior.PoolID != requested.PoolID || prior.PolicyHash != requested.PolicyHash || prior.ProviderKey != requested.ProviderKey ||
		prior.SourceManifestHash != c.Workspace.ManifestHash || prior.WorkspaceIdentityHash != c.Workspace.IdentityHash || prior.WorkspaceRoot != c.Workspace.Destination.Path ||
		(requested.SourceManifestHash != "" && requested.SourceManifestHash != prior.SourceManifestHash) ||
		(requested.WorkspaceIdentityHash != "" && requested.WorkspaceIdentityHash != prior.WorkspaceIdentityHash) ||
		(requested.WorkspaceRoot != "" && requested.WorkspaceRoot != prior.WorkspaceRoot) {
		return domain.ErrSwarmConflict
	}
	requested.SourceManifestHash = c.Workspace.ManifestHash
	requested.WorkspaceIdentityHash = c.Workspace.IdentityHash
	requested.WorkspaceRoot = c.Workspace.Destination.Path
	requested.Resources = append([]domain.SwarmResource(nil), prior.Resources...)
	c.Admission.Claim.Schedule = &requested
	return domain.ValidateSwarmScheduleBinding(requested)
}

// Dispatch never retries an ambiguous Start. A crash/timeout after Begin leaves
// the attempt dispatching and its capacity held until explicit reconciliation.
func (s *Service) Dispatch(ctx context.Context, id string, snapshot workspace.Snapshot) error {
	if ctx == nil || !domain.SwarmValidID(s.Owner) || s.Store == nil || s.Composer == nil || s.Preparer == nil || s.Runtime == nil {
		return qualification.ErrUnverified
	}
	if s.Runtime.Mode() == qualification.LiveEvidence {
		return errors.Join(qualification.ErrUnverified, ValidateAOSpawnBoundary())
	}
	if !s.beginOperation(id) {
		return domain.ErrSwarmNotReady
	}
	defer s.endOperation(id)
	a, err := s.Store.GetSwarmAttempt(ctx, id)
	if err != nil {
		return err
	}
	if a.Owner != s.Owner {
		return domain.ErrSwarmStale
	}
	if a.State != "reserved" {
		return domain.ErrSwarmNotReady
	}
	if a.Schedule == nil || a.Schedule.SourceManifestHash != snapshot.ManifestHash || a.Schedule.WorkspaceIdentityHash != snapshot.IdentityHash || a.Schedule.WorkspaceRoot != snapshot.Destination.Path || a.BaseCommit != snapshot.Source.Head {
		return domain.ErrSwarmStale
	}
	if err := workspace.ValidateForLaunch(ctx, snapshot); err != nil {
		return err
	}
	p, err := s.Composer.Compose(ctx, a.RunID, a.ID)
	if err != nil {
		return err
	}
	prepared, prepareErr := s.Preparer.Prepare(ctx, a, p, snapshot)
	if err := s.checkPreparation(a, snapshot, prepared, prepareErr); err != nil {
		return err
	}
	if err := s.Composer.ValidateResume(ctx, p); err != nil {
		return err
	}
	if err := workspace.ValidateForLaunch(ctx, snapshot); err != nil {
		return err
	}
	// No authority comes from a cached packet or an earlier qualification.
	if err := s.checkPreparation(a, snapshot, prepared, prepareErr); err != nil {
		return err
	}
	current, err := s.Store.GetSwarmAttempt(ctx, id)
	if err != nil {
		return err
	}
	if domain.SwarmHash(a) != domain.SwarmHash(current) {
		return domain.ErrSwarmStale
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a, launch, err := s.Store.BeginSwarmDispatch(ctx, fence(a), s.now())
	if err != nil {
		return err
	}
	if !launch {
		return domain.ErrSwarmNotReady
	}
	observed, err := s.Runtime.Start(ctx, Launch{a, p, snapshot, prepared})
	if err != nil {
		return err
	}
	return s.recordObservation(ctx, a, observed)
}

func (s *Service) checkPreparation(a domain.SwarmAttempt, snapshot workspace.Snapshot, p qualification.Preparation, err error) error {
	r := p.Report
	e := r.Evidence
	if r.CapabilityFingerprint != a.CapabilityFingerprint || !domain.SwarmValidHash(r.Binding, false) || e.Binding != r.Binding || e.ReceiptID == "" || !e.ExpiresAt.After(s.now()) ||
		r.Resolved.AgentID != string(a.Resolved.Harness) || r.Resolved.Model != a.Resolved.Model || r.Resolved.Effort != a.Resolved.Effort ||
		p.Launch.WorkspacePath != snapshot.Destination.Path || p.Launch.Config.Model != a.Resolved.Model || p.Launch.Config.Effort != a.Resolved.Effort ||
		e.Checks.SettingsPreserved != qualification.Passed || e.Checks.CancellationObserved != qualification.Passed || e.Checks.ProcessContainment != qualification.Passed || e.Checks.ProtectedStorageDenied != qualification.Passed ||
		(domain.SwarmTaskRole(a.Contract) == "reviewer" && e.Checks.SubmissionReadOnly != qualification.Passed) {
		return qualification.ErrUnverified
	}
	switch s.Runtime.Mode() {
	case qualification.LiveEvidence:
		return errors.Join(qualification.ErrUnverified, ValidateAOSpawnBoundary())
	case qualification.TestEvidence:
		if (err != nil && !errors.Is(err, qualification.ErrUnverified)) || !r.TestVerified || r.LiveQualified || e.Mode != qualification.TestEvidence {
			return qualification.ErrUnverified
		}
	default:
		return qualification.ErrUnverified
	}
	return nil
}

// Reconcile updates an attempt only after observing its exact durable runtime identity.
func (s *Service) Reconcile(ctx context.Context, id string) error {
	if ctx == nil || !domain.SwarmValidID(s.Owner) || s.Store == nil || s.Runtime == nil {
		return domain.ErrSwarmInvalid
	}
	if s.Runtime.Mode() == qualification.LiveEvidence {
		return errors.Join(qualification.ErrUnverified, ValidateAOSpawnBoundary())
	}
	if s.Runtime.Mode() != qualification.TestEvidence {
		return qualification.ErrUnverified
	}
	if !s.beginOperation(id) {
		return domain.ErrSwarmNotReady
	}
	defer s.endOperation(id)
	a, err := s.Store.GetSwarmAttempt(ctx, id)
	if err != nil {
		return err
	}
	if a.Owner != s.Owner {
		return domain.ErrSwarmStale
	}
	if a.State != "dispatching" && a.State != "running" {
		return domain.ErrSwarmNotReady
	}
	observation, err := s.Runtime.Observe(ctx, a)
	if err != nil {
		return err
	}
	return s.recordObservation(ctx, a, observation)
}
func (s *Service) recordObservation(ctx context.Context, a domain.SwarmAttempt, o Observation) error {
	if !o.Exact || o.OperationID != a.OperationID || (a.SessionID != "" && o.SessionID != a.SessionID) {
		return qualification.ErrUnverified
	}
	switch o.State {
	case "alive":
		if !domain.SwarmValidID(o.SessionID) {
			return qualification.ErrUnverified
		}
		return s.Store.ConfirmSwarmRunning(ctx, fence(a), o.SessionID, o.ReportedModel, s.now())
	case "absent":
		if !o.Terminal {
			return qualification.ErrUnverified
		}
		run, err := s.Store.GetSwarmRun(ctx, a.RunID)
		if err != nil {
			return err
		}
		state := "failed"
		if run.Cancelled {
			state = "cancelled"
		}
		return s.Store.FinishSwarmAttempt(ctx, fence(a), state, "", true, s.now())
	default:
		return qualification.ErrUnverified
	}
}

// Guard the complete dispatch/reconciliation operation, including Start's
// ambiguous interval. Calls for unrelated attempts remain concurrent.
func (s *Service) beginOperation(id string) bool {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.operations[id] {
		return false
	}
	if s.operations == nil {
		s.operations = make(map[string]bool)
	}
	s.operations[id] = true
	return true
}
func (s *Service) endOperation(id string) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	delete(s.operations, id)
}
func fence(a domain.SwarmAttempt) domain.SwarmFence {
	return domain.SwarmFence{AttemptID: a.ID, Owner: a.Owner, Generation: a.Generation}
}
func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
