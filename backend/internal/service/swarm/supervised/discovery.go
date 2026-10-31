package supervised

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type discoveryStore interface {
	GetSwarmDiscovery(context.Context, domain.ProjectID, int64) (domain.SwarmDiscoveryBrief, error)
	SaveSwarmDiscovery(context.Context, domain.ProjectID, domain.SwarmDiscoveryUpdate, time.Time) (domain.SwarmDiscoveryBrief, error)
}

// Discovery reads decisions without launching agents.
func (s *Service) Discovery(ctx context.Context, project domain.ProjectID, revision int64) (domain.SwarmDiscoveryBrief, error) {
	if _, err := s.runtime.ProjectPath(ctx, project); err != nil {
		return domain.SwarmDiscoveryBrief{}, err
	}
	store, ok := s.store.(discoveryStore)
	if !ok {
		return domain.SwarmDiscoveryBrief{}, domain.ErrSwarmNotFound
	}
	return store.GetSwarmDiscovery(ctx, project, revision)
}

// SaveDiscovery appends decisions without changing source files.
func (s *Service) SaveDiscovery(ctx context.Context, project domain.ProjectID, update domain.SwarmDiscoveryUpdate) (domain.SwarmDiscoveryBrief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.runtime.ProjectPath(ctx, project); err != nil {
		return domain.SwarmDiscoveryBrief{}, err
	}
	store, ok := s.store.(discoveryStore)
	if !ok {
		return domain.SwarmDiscoveryBrief{}, domain.ErrSwarmNotReady
	}
	return store.SaveSwarmDiscovery(ctx, project, update, time.Now().UTC())
}

// checkBrief is called before preparing and before recording a new launch.
// Historical run replay stays readable after the project brief changes.
func (s *Service) checkBrief(ctx context.Context, project domain.ProjectID, ref *domain.SwarmBriefRef) (domain.SwarmDiscoveryBrief, error) {
	store, ok := s.store.(discoveryStore)
	if !ok {
		if ref != nil {
			return domain.SwarmDiscoveryBrief{}, domain.ErrSwarmNotReady
		}
		return domain.SwarmDiscoveryBrief{}, nil
	}
	brief, err := store.GetSwarmDiscovery(ctx, project, 0)
	if errors.Is(err, domain.ErrSwarmNotFound) && ref == nil {
		return domain.SwarmDiscoveryBrief{}, nil
	}
	if err != nil {
		return domain.SwarmDiscoveryBrief{}, err
	}
	if brief.Status != "ready" {
		return domain.SwarmDiscoveryBrief{}, fmt.Errorf("%w: discovery needs answers for %v", domain.ErrSwarmNotReady, brief.Missing)
	}
	if ref == nil || ref.Revision != brief.Revision || ref.SHA256 != brief.SHA256 {
		return domain.SwarmDiscoveryBrief{}, fmt.Errorf("%w: proposal must reference current discovery revision %d", domain.ErrSwarmStale, brief.Revision)
	}
	return brief, nil
}
