package routing

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/qualification"
)

// Store provides durable request recovery and atomic policy-bound task admission.
type Store interface {
	GetSwarmAttemptByRequest(context.Context, string) (domain.SwarmAttempt, error)
	GetSwarmRun(context.Context, string) (domain.SwarmRun, error)
	GetSwarmContract(context.Context, string, string, int64) (domain.SwarmTaskContract, error)
	GetSwarmRoutingAccount(context.Context, string) (domain.SwarmRoutingAccount, error)
	ClaimSwarmTask(context.Context, domain.SwarmClaim, string, string, time.Time, time.Time) (domain.SwarmAttempt, error)
}

// Service combines current catalog advice with durable account admission.
type Service struct {
	Store   Store
	Catalog qualification.Catalog
	Now     func() time.Time
}

// AdmissionRequest identifies a claim, context demand and requested lease interval.
type AdmissionRequest struct {
	Claim                         domain.SwarmClaim
	AttemptID, OperationID, Scope string
	ContextTokens                 int64
	LeaseUntil                    time.Time
}

// Admit recovers an existing logical request before discovery. New requests
// revalidate AO's catalog, recommend the frozen contract's exact selection, and
// reserve atomically. Neither a new reservation nor its replay grants permission
// to launch: the scheduler must freshly qualify the actual launch separately.
func (s Service) Admit(ctx context.Context, in AdmissionRequest) (domain.SwarmAttempt, RouteAdvice, error) {
	var out domain.SwarmAttempt
	var advice RouteAdvice
	if ctx == nil || s.Store == nil || !domain.SwarmValidID(in.Claim.RunID) ||
		!domain.SwarmValidID(in.Claim.TaskID) || !domain.SwarmValidID(in.Claim.RequestID) ||
		in.Claim.Version < 1 || !domain.SwarmValidHash(in.Claim.CapabilityFingerprint, false) ||
		in.ContextTokens <= 0 || (in.Scope != "" && !domain.SwarmValidID(in.Scope)) {
		return out, advice, domain.ErrSwarmInvalid
	}
	if err := ctx.Err(); err != nil {
		return out, advice, err
	}
	if prior, replay, found, err := s.replay(ctx, in); err != nil || found {
		return prior, replay, err
	}
	if s.Catalog == nil {
		return out, advice, domain.ErrSwarmInvalid
	}
	run, err := s.Store.GetSwarmRun(ctx, in.Claim.RunID)
	if err != nil {
		return out, advice, err
	}
	task, err := s.Store.GetSwarmContract(ctx, run.ID, in.Claim.TaskID, in.Claim.Version)
	if err != nil {
		return out, advice, err
	}
	account, err := s.Store.GetSwarmRoutingAccount(ctx, run.Plan.BudgetBucket)
	if err != nil {
		return out, advice, err
	}
	catalog, err := s.Catalog.RevalidateModels(ctx, string(task.Agent.Harness), in.Scope)
	if err != nil {
		return out, advice, err
	}
	if err := ctx.Err(); err != nil {
		return out, advice, err
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	advice, err = RecommendRoute(Task{
		Role: domain.SwarmTaskRole(task), Requested: task.Agent,
		ContextTokens: in.ContextTokens, ReserveTokens: task.ReserveTokens,
	}, catalog, account.Quota, account.Policy, now)
	if err != nil {
		return out, advice, err
	}
	advice.Binding.CatalogScope = in.Scope
	if err := ctx.Err(); err != nil {
		return out, advice, err
	}
	// Caller-supplied resolved selections and bindings are never authority.
	in.Claim.Resolved = advice.Resolved
	in.Claim.Routing = &advice.Binding
	out, err = s.Store.ClaimSwarmTask(ctx, in.Claim, in.AttemptID, in.OperationID, now, in.LeaseUntil)
	if errors.Is(err, domain.ErrSwarmConflict) {
		// Another coordinator may have committed after our first lookup using
		// a different quota revision. Recover once without retrying discovery.
		if prior, replay, found, replayErr := s.replay(ctx, in); replayErr != nil || found {
			return prior, replay, replayErr
		}
	}
	if err == nil && (out.ID != in.AttemptID || out.OperationID != in.OperationID || out.Owner != in.Claim.Owner) {
		// The atomic store can return a same-hash concurrent winner directly.
		return out, recoveredAdvice(out), nil
	}
	return out, advice, err
}

func (s Service) replay(ctx context.Context, in AdmissionRequest) (domain.SwarmAttempt, RouteAdvice, bool, error) {
	a, err := s.Store.GetSwarmAttemptByRequest(ctx, in.Claim.RequestID)
	if errors.Is(err, domain.ErrSwarmNotFound) {
		return domain.SwarmAttempt{}, RouteAdvice{}, false, nil
	}
	if err != nil {
		return domain.SwarmAttempt{}, RouteAdvice{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return domain.SwarmAttempt{}, RouteAdvice{}, false, err
	}
	if a.RequestID != in.Claim.RequestID || a.RunID != in.Claim.RunID || a.TaskID != in.Claim.TaskID ||
		a.Contract.Version != in.Claim.Version || a.CapabilityFingerprint != in.Claim.CapabilityFingerprint ||
		domain.SwarmHash(a.Schedule) != domain.SwarmHash(in.Claim.Schedule) ||
		a.Routing == nil || a.Routing.ContextTokens != in.ContextTokens || a.Routing.CatalogScope != in.Scope {
		return domain.SwarmAttempt{}, RouteAdvice{}, true, domain.ErrSwarmConflict
	}
	return a, recoveredAdvice(a), true, nil
}

func recoveredAdvice(a domain.SwarmAttempt) RouteAdvice {
	return RouteAdvice{
		Requested: a.Contract.Agent, Resolved: a.Resolved, Binding: *a.Routing,
		// The quota is retained by hash, not re-read or assumed current.
		QuotaUnknown: true, Replayed: true,
		Reasons: []string{"recovered the existing reservation without refreshing quota; this is not launch permission"},
	}
}

// SwarmSpawnConfig suppresses all optional model-powered naming paths. The
// scheduler must still bind the qualified model, workspace and permissions.
func SwarmSpawnConfig(base ports.SpawnConfig) ports.SpawnConfig {
	base.DisableCosmeticModelCalls = true
	base.NameFromPrompt = false
	return base
}
