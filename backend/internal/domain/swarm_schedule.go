package domain

import (
	"path"
	"path/filepath"
	"strings"
)

// One immutable pool governs every new claim in a database. Explicit provider
// and project entries are allowlists; an absent or zero limit admits no work.
// Every non-reviewer role consumes a worker slot, including planner/integration.
type SwarmSchedulerPolicy struct {
	PoolID         string           `json:"poolId"`
	MaxWorkers     int64            `json:"maxWorkers"`
	MaxReviewers   int64            `json:"maxReviewers"`
	ProviderLimits map[string]int64 `json:"providerLimits"`
	ProjectLimits  map[string]int64 `json:"projectLimits"`
}

// Resources supplement the original contract's logical resources. The trusted
// coordinator resolves filesystem aliases; these keys alone enforce no sandbox.
// ProviderKey is the routing policy's canonical AccountID, shared across models
// and harnesses, so changing either cannot evade account concurrency limits.
type SwarmScheduleBinding struct {
	PoolID                string          `json:"poolId"`
	PolicyHash            string          `json:"policyHash"`
	ProviderKey           string          `json:"providerKey"`
	SourceManifestHash    string          `json:"sourceManifestHash"`
	WorkspaceIdentityHash string          `json:"workspaceIdentityHash"`
	WorkspaceRoot         string          `json:"workspaceRoot"`
	Resources             []SwarmResource `json:"resources"`
}

func ValidateSwarmSchedulerPolicy(p SwarmSchedulerPolicy) error {
	if !SwarmValidID(p.PoolID) || p.MaxWorkers < 0 || p.MaxReviewers < 0 ||
		p.MaxWorkers > 1000 || p.MaxReviewers > 1000 || (p.MaxWorkers == 0 && p.MaxReviewers == 0) {
		return ErrSwarmInvalid
	}
	for _, limits := range []map[string]int64{p.ProviderLimits, p.ProjectLimits} {
		if len(limits) == 0 || len(limits) > 1000 {
			return ErrSwarmInvalid
		}
		for key, limit := range limits {
			if !SwarmValidID(key) || limit < 0 || limit > 1000 {
				return ErrSwarmInvalid
			}
		}
	}
	return nil
}

func ValidateSwarmScheduleBinding(b SwarmScheduleBinding) error {
	if !SwarmValidID(b.PoolID) || !SwarmValidHash(b.PolicyHash, false) ||
		!SwarmValidID(b.ProviderKey) || !SwarmValidHash(b.SourceManifestHash, false) ||
		!SwarmValidHash(b.WorkspaceIdentityHash, false) || !filepath.IsAbs(b.WorkspaceRoot) ||
		filepath.Clean(b.WorkspaceRoot) != b.WorkspaceRoot || strings.ContainsRune(b.WorkspaceRoot, '\x00') ||
		len(b.Resources) == 0 || len(b.Resources) > 4096 {
		return ErrSwarmInvalid
	}
	for _, r := range b.Resources {
		if !SwarmValidID(r.Namespace) || !SwarmValidID(r.Key) {
			return ErrSwarmInvalid
		}
		switch r.Kind {
		case "path":
			if path.IsAbs(r.Key) || path.Clean(r.Key) != r.Key || r.Key == ".." || strings.HasPrefix(r.Key, "../") || strings.ContainsAny(r.Key, "\\*?[]:") {
				return ErrSwarmInvalid
			}
		case "semantic":
		default:
			return ErrSwarmInvalid
		}
	}
	return nil
}
