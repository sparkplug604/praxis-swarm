package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// GetSwarmDiscovery reads the latest or a specific immutable revision.
func (s *Store) GetSwarmDiscovery(ctx context.Context, project domain.ProjectID, revision int64) (domain.SwarmDiscoveryBrief, error) {
	var out domain.SwarmDiscoveryBrief
	if !domain.SwarmValidID(string(project)) || revision < 0 {
		return out, domain.ErrSwarmInvalid
	}
	var raw string
	var err error
	if revision == 0 {
		raw, err = s.qr.LatestSwarmDiscovery(ctx, string(project))
	} else {
		raw, err = s.qr.GetSwarmDiscoveryRevision(ctx, gen.GetSwarmDiscoveryRevisionParams{ProjectID: string(project), Revision: revision})
	}
	if err != nil {
		return out, swarmErr(err)
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return domain.SwarmDiscoveryBrief{}, err
	}
	return out, nil
}

// SaveSwarmDiscovery uses the existing database writer reservation for CAS.
func (s *Store) SaveSwarmDiscovery(ctx context.Context, project domain.ProjectID, update domain.SwarmDiscoveryUpdate, now time.Time) (domain.SwarmDiscoveryBrief, error) {
	if update.ExpectedRevision < 0 || update.ExpectedRevision == 1<<63-1 {
		return domain.SwarmDiscoveryBrief{}, domain.ErrSwarmInvalid
	}
	out, err := domain.NewSwarmDiscoveryBrief(project, update.ExpectedRevision+1, update.Brief, now)
	if err != nil {
		return domain.SwarmDiscoveryBrief{}, err
	}
	err = s.swarmWrite(ctx, func(q *gen.Queries) error {
		var prior domain.SwarmDiscoveryBrief
		raw, e := q.LatestSwarmDiscovery(ctx, string(project))
		if e == nil {
			if e := json.Unmarshal([]byte(raw), &prior); e != nil {
				return e
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if prior.Revision != update.ExpectedRevision {
			return fmt.Errorf("%w: discovery expected revision %d; latest is %d", domain.ErrSwarmStale, update.ExpectedRevision, prior.Revision)
		}
		return q.InsertSwarmDiscovery(ctx, gen.InsertSwarmDiscoveryParams{ProjectID: string(project), Revision: out.Revision, PayloadJson: swarmJSON(out)})
	})
	if err != nil {
		return domain.SwarmDiscoveryBrief{}, err
	}
	return out, nil
}
