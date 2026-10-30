package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// ConfigureSwarmScheduler installs one immutable database-wide pool. Existing
// active legacy claims must be confirmed stopped first, so no uncounted work can
// escape the new limits. Repeating the exact configuration is idempotent.
func (s *Store) ConfigureSwarmScheduler(ctx context.Context, p domain.SwarmSchedulerPolicy) error {
	if err := domain.ValidateSwarmSchedulerPolicy(p); err != nil {
		return err
	}
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, err := q.GetConfiguredSwarmScheduler(ctx)
		if err == nil {
			if prior.PolicyJson == swarmJSON(p) {
				return nil
			}
			return domain.ErrSwarmConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		active, err := q.CountSwarmActiveAll(ctx)
		if err != nil {
			return err
		}
		if active != 0 {
			return domain.ErrSwarmConflict
		}
		return q.InsertSwarmSchedulerPolicy(ctx, gen.InsertSwarmSchedulerPolicyParams{PoolID: p.PoolID, PolicyHash: domain.SwarmHash(p), PolicyJson: swarmJSON(p)})
	})
}

// GetSwarmSchedulerPolicy returns the immutable scheduling limits for the named pool.
func (s *Store) GetSwarmSchedulerPolicy(ctx context.Context, poolID string) (p domain.SwarmSchedulerPolicy, err error) {
	if !domain.SwarmValidID(poolID) {
		return p, domain.ErrSwarmInvalid
	}
	row, err := s.qr.GetSwarmSchedulerPolicy(ctx, poolID)
	if err != nil {
		return p, swarmErr(err)
	}
	err = json.Unmarshal([]byte(row.PolicyJson), &p)
	return p, err
}

func swarmScheduleSlot(t domain.SwarmTaskContract) string {
	if domain.SwarmTaskRole(t) == "reviewer" {
		return "reviewer"
	}
	return "worker"
}

// Called only inside the claim's existing SQLite writer transaction. The
// resulting reservation, original/resolved resource locks, usage, and attempt
// either all commit or all roll back. No clock-based slot reclamation exists.
func checkSwarmSchedule(ctx context.Context, q *gen.Queries, c domain.SwarmClaim, t domain.SwarmTaskContract, run domain.SwarmRun) error {
	row, err := q.GetConfiguredSwarmScheduler(ctx)
	if errors.Is(err, sql.ErrNoRows) && c.Schedule == nil {
		return nil
	}
	if err != nil {
		return swarmErr(err)
	}
	b := c.Schedule
	if b == nil {
		return domain.ErrSwarmNotReady
	}
	if err := domain.ValidateSwarmScheduleBinding(*b); err != nil {
		return err
	}
	if b.PoolID != row.PoolID || b.PolicyHash != row.PolicyHash {
		return domain.ErrSwarmStale
	}
	var p domain.SwarmSchedulerPolicy
	if err := json.Unmarshal([]byte(row.PolicyJson), &p); err != nil {
		return err
	}
	// Resolve account identity from immutable routing configuration; caller keys
	// cannot create a second provider pool for another model, harness, or bucket.
	account, err := q.GetSwarmRoutingAccount(ctx, run.Plan.BudgetBucket)
	if err != nil {
		return swarmErr(err)
	}
	if c.Routing == nil || b.ProviderKey != account.AccountID {
		return domain.ErrSwarmConflict
	}
	roleLimit := p.MaxWorkers
	if swarmScheduleSlot(t) == "reviewer" {
		roleLimit = p.MaxReviewers
	}
	providerLimit, projectLimit := p.ProviderLimits[b.ProviderKey], p.ProjectLimits[string(run.Plan.ProjectID)]
	if roleLimit == 0 || providerLimit == 0 || projectLimit == 0 {
		return domain.ErrSwarmCapacity
	}
	roles, err := q.CountSwarmScheduleRole(ctx, gen.CountSwarmScheduleRoleParams{PoolID: b.PoolID, SlotKind: swarmScheduleSlot(t)})
	if err != nil {
		return err
	}
	providers, err := q.CountSwarmScheduleProvider(ctx, gen.CountSwarmScheduleProviderParams{PoolID: b.PoolID, ProviderKey: b.ProviderKey})
	if err != nil {
		return err
	}
	projects, err := q.CountSwarmScheduleProject(ctx, gen.CountSwarmScheduleProjectParams{PoolID: b.PoolID, ProjectID: string(run.Plan.ProjectID)})
	if err != nil {
		return err
	}
	if roles >= roleLimit || providers >= providerLimit || projects >= projectLimit {
		return domain.ErrSwarmCapacity
	}
	workspaces, err := q.CountSwarmScheduleWorkspace(ctx, gen.CountSwarmScheduleWorkspaceParams{PoolID: b.PoolID, WorkspaceRoot: b.WorkspaceRoot})
	if err != nil {
		return err
	}
	if workspaces != 0 {
		return domain.ErrSwarmConflict
	}
	return nil
}

func swarmClaimResources(t domain.SwarmTaskContract, b *domain.SwarmScheduleBinding) []domain.SwarmResource {
	resources := append([]domain.SwarmResource(nil), t.Resources...)
	if b != nil {
		resources = append(resources, b.Resources...)
	}
	// Resolved identities may intentionally include the original logical key.
	// Preserve all scopes, deduplicating only exact identity before insertion.
	seen := make(map[domain.SwarmResource]bool, len(resources))
	unique := resources[:0]
	for _, r := range resources {
		if !seen[r] {
			unique = append(unique, r)
			seen[r] = true
		}
	}
	return unique
}

func reserveSwarmSchedule(ctx context.Context, q *gen.Queries, a domain.SwarmAttempt, projectID domain.ProjectID) error {
	if a.Schedule == nil {
		return nil
	}
	b := a.Schedule
	return q.InsertSwarmScheduleBinding(ctx, gen.InsertSwarmScheduleBindingParams{AttemptID: a.ID, PoolID: b.PoolID, ProviderKey: b.ProviderKey, ProjectID: string(projectID), WorkspaceRoot: b.WorkspaceRoot, SlotKind: swarmScheduleSlot(a.Contract)})
}
