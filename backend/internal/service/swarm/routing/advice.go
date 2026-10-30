// Package routing calculates route advice from explicit inputs. It neither
// discovers providers nor reserves capacity or dispatches work.
package routing

import (
	"fmt"
	"slices"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// The bounded effort and quota-selection concepts adapt OpenSwarm
// b2a70d436ad72f0c5b036730fc86b749ddc68af7 (Apache-2.0). This is a new Go
// implementation: unknown inputs are rejected, overrides remain bounded, and
// unused quota never increases effort or selects a premium model.
type Task struct {
	Role          string
	Requested     domain.SwarmSelection
	ContextTokens int64
	ReserveTokens int64
	EffortShift   int
}

type RouteAdvice struct {
	Requested    domain.SwarmSelection
	Resolved     domain.SwarmSelection
	Binding      domain.SwarmRouteBinding
	QuotaUnknown bool
	// Replayed is positive evidence of recovery. False does not prove creation:
	// the store cannot distinguish a concurrent same-hash claim with identical
	// delivery IDs. Neither value grants launch authority.
	Replayed bool
	Reasons  []string
}

// RecommendRoute validates one explicit selection or the role's exact default.
// It does not search for alternate models, providers, or tiers. A successful
// recommendation still requires AO's atomic admission and launch qualification.
func RecommendRoute(task Task, catalog ports.AgentModelCatalog, quota domain.SwarmQuotaSnapshot, policy domain.SwarmRoutingPolicy, now time.Time) (RouteAdvice, error) {
	out := RouteAdvice{Requested: task.Requested}
	if !domain.SwarmValidRole(task.Role) || task.ContextTokens <= 0 || task.ReserveTokens <= 0 || now.IsZero() {
		return out, fmt.Errorf("%w: role, context and reservation must be explicit", domain.ErrSwarmInvalid)
	}
	if err := domain.ValidateSwarmRoutingPolicy(policy); err != nil {
		return out, err
	}
	if err := domain.ValidateSwarmQuota(quota); err != nil {
		return out, err
	}
	if quota.Bucket != policy.Bucket {
		return out, fmt.Errorf("%w: quota and policy buckets differ", domain.ErrSwarmInvalid)
	}
	selection := task.Requested
	explicit := selection != (domain.SwarmSelection{})
	if !explicit {
		var found bool
		selection, found = policy.Defaults[task.Role]
		if !found {
			return out, fmt.Errorf("%w: role has no default selection", domain.ErrSwarmNotReady)
		}
		out.Reasons = append(out.Reasons, "selected the configured role default")
	}
	if !domain.SwarmValidID(string(selection.Harness)) || !domain.SwarmValidID(selection.Model) || (selection.Effort != "" && !domain.SwarmValidID(selection.Effort)) {
		return out, fmt.Errorf("%w: select an exact harness, model and advertised effort", domain.ErrSwarmInvalid)
	}
	rule, allowed := domain.SwarmPolicyModel(policy, task.Role, selection)
	if !allowed {
		return out, fmt.Errorf("%w: model is not allowed for this role", domain.ErrSwarmInvalid)
	}
	model, err := catalogModel(catalog, selection, now)
	if err != nil {
		return out, err
	}
	if model.ContextWindow <= 0 {
		return out, fmt.Errorf("%w: model context capacity is unknown", domain.ErrSwarmNotReady)
	}
	if task.ContextTokens > model.ContextWindow {
		return out, fmt.Errorf("%w: task exceeds model context capacity", domain.ErrSwarmCapacity)
	}
	supported, err := allowedEfforts(model, rule)
	if err != nil {
		return out, err
	}
	if explicit {
		if task.EffortShift != 0 {
			return out, fmt.Errorf("%w: an explicit effort cannot be shifted", domain.ErrSwarmInvalid)
		}
		if (len(model.Efforts) == 0 && selection.Effort != "") || (len(model.Efforts) > 0 && !slices.Contains(supported, selection.Effort)) {
			return out, fmt.Errorf("%w: explicit effort is outside catalog or policy bounds", domain.ErrSwarmInvalid)
		}
		out.Reasons = append(out.Reasons, "validated the explicit selection without substitution")
	} else if len(model.Efforts) == 0 {
		if selection.Effort != "" || task.EffortShift != 0 {
			return out, fmt.Errorf("%w: model does not accept an effort setting", domain.ErrSwarmInvalid)
		}
	} else {
		if selection.Effort == "" {
			selection.Effort = model.DefaultEffort
			out.Reasons = append(out.Reasons, "used the model's advertised default effort")
		}
		selection.Effort, err = ShiftEffort(model.Efforts, selection.Effort, task.EffortShift)
		if err != nil {
			return out, err
		}
		shifted := selection.Effort
		selection.Effort, err = ClampEffort(model.Efforts, supported, shifted)
		if err != nil {
			return out, err
		}
		if task.EffortShift != 0 {
			out.Reasons = append(out.Reasons, "applied the requested effort shift within catalog bounds")
		}
		if selection.Effort != shifted {
			out.Reasons = append(out.Reasons, "bounded default effort by role policy")
		}
	}
	out.Resolved = selection
	out.Binding = domain.SwarmRouteBinding{
		Role: task.Role, Premium: rule.Premium, QuotaScope: rule.QuotaScope,
		PolicyHash: domain.SwarmHash(policy), QuotaHash: domain.SwarmHash(quota),
		CatalogFingerprint: catalog.InputFingerprint,
		ContextTokens:      task.ContextTokens,
	}
	out.QuotaUnknown = quotaUnknown(quota, rule.QuotaScope)
	if out.QuotaUnknown {
		out.Reasons = append(out.Reasons, "provider token quota remains unknown")
	}
	if err := domain.CheckSwarmQuota(quota, rule.QuotaScope, now, domain.SwarmQuotaUsage{}, task.ReserveTokens, policy.AllowUnknownQuota); err != nil {
		return out, err
	}
	return out, nil
}

func catalogModel(catalog ports.AgentModelCatalog, selection domain.SwarmSelection, now time.Time) (ports.AgentModelInfo, error) {
	if catalog.WarningCode == ports.ModelCatalogWarningAuthRequired || catalog.WarningCode == ports.ModelCatalogWarningAuthExpired {
		return ports.AgentModelInfo{}, fmt.Errorf("%w: selected adapter requires authentication", ports.ErrAgentAuthRequired)
	}
	if catalog.AgentID != string(selection.Harness) || !domain.SwarmValidID(catalog.InputFingerprint) || catalog.Stale || catalog.RefreshRecommended || (catalog.RefreshState != "" && catalog.RefreshState != "idle") {
		return ports.AgentModelInfo{}, fmt.Errorf("%w: refresh the selected adapter's catalog", domain.ErrSwarmStale)
	}
	if catalog.FetchedAt.After(now) || catalog.ValidatedAt.After(now) || (catalog.LastSuccessAt != nil && catalog.LastSuccessAt.After(now)) {
		return ports.AgentModelInfo{}, fmt.Errorf("%w: catalog observation is in the future", domain.ErrSwarmStale)
	}
	if catalog.SelectionMode != ports.ModelSelectionCatalog && catalog.SelectionMode != ports.ModelSelectionText {
		return ports.AgentModelInfo{}, fmt.Errorf("%w: catalog must expose explicit model identities", domain.ErrSwarmNotReady)
	}
	var selected ports.AgentModelInfo
	seen := make(map[string]bool, len(catalog.Models))
	for _, model := range catalog.Models {
		if !domain.SwarmValidID(model.ID) || seen[model.ID] {
			return ports.AgentModelInfo{}, fmt.Errorf("%w: catalog model identities are ambiguous", domain.ErrSwarmInvalid)
		}
		seen[model.ID] = true
		if model.ID == selection.Model {
			selected = model
		}
	}
	if selected.ID == "" {
		return ports.AgentModelInfo{}, fmt.Errorf("%w: selected model is absent from the catalog", domain.ErrSwarmNotReady)
	}
	if len(catalog.RunnableModels) > 0 && !slices.Contains(catalog.RunnableModels, selection.Model) {
		return ports.AgentModelInfo{}, fmt.Errorf("%w: selected model is not runnable", domain.ErrSwarmNotReady)
	}
	return selected, nil
}

func allowedEfforts(model ports.AgentModelInfo, rule domain.SwarmRouteModel) ([]string, error) {
	if len(model.Efforts) == 0 {
		if model.DefaultEffort != "" || rule.MinEffort != "" || rule.MaxEffort != "" {
			return nil, fmt.Errorf("%w: effort policy requires unsupported model capabilities", domain.ErrSwarmInvalid)
		}
		return nil, nil
	}
	positions, err := effortPositions(model.Efforts)
	if err != nil {
		return nil, err
	}
	if model.DefaultEffort != "" {
		if _, found := positions[model.DefaultEffort]; !found {
			return nil, fmt.Errorf("%w: advertised default effort is absent from catalog order", domain.ErrSwarmInvalid)
		}
	}
	low, high := 0, len(model.Efforts)-1
	for _, bound := range []string{rule.MinEffort, rule.MaxEffort} {
		if bound != "" {
			if _, found := positions[bound]; !found {
				return nil, fmt.Errorf("%w: policy effort bound is absent from catalog order", domain.ErrSwarmInvalid)
			}
		}
	}
	if rule.MinEffort != "" {
		low = positions[rule.MinEffort]
	}
	if rule.MaxEffort != "" {
		high = positions[rule.MaxEffort]
	}
	if low > high {
		return nil, fmt.Errorf("%w: policy effort bounds are reversed", domain.ErrSwarmInvalid)
	}
	return model.Efforts[low : high+1], nil
}

// ShiftEffort follows the provider's explicit ordering and saturates at its
// endpoints. Unknown or duplicate efforts are errors, never assumed defaults.
func ShiftEffort(order []string, current string, delta int) (string, error) {
	positions, err := effortPositions(order)
	if err != nil {
		return "", err
	}
	i, found := positions[current]
	if !found {
		return "", fmt.Errorf("%w: effort is absent from catalog order", domain.ErrSwarmInvalid)
	}
	// Compare before adding to avoid integer overflow for arbitrary deltas.
	if delta < -i {
		return order[0], nil
	}
	if delta > len(order)-1-i {
		return order[len(order)-1], nil
	}
	return order[i+delta], nil
}

// ClampEffort selects the nearest supported effort, choosing the lower effort
// on a tie, independent of the order in which supported values were supplied.
func ClampEffort(order, supported []string, current string) (string, error) {
	positions, err := effortPositions(order)
	if err != nil {
		return "", err
	}
	want, found := positions[current]
	if !found || len(supported) == 0 {
		return "", fmt.Errorf("%w: current and supported efforts must be known", domain.ErrSwarmInvalid)
	}
	seen := make(map[string]bool, len(supported))
	best, distance := -1, len(order)
	for _, effort := range supported {
		i, found := positions[effort]
		if !found || seen[effort] {
			return "", fmt.Errorf("%w: supported efforts are unknown or ambiguous", domain.ErrSwarmInvalid)
		}
		seen[effort] = true
		d := i - want
		if d < 0 {
			d = -d
		}
		if d < distance || (d == distance && i < best) {
			best, distance = i, d
		}
	}
	return order[best], nil
}

func effortPositions(order []string) (map[string]int, error) {
	if len(order) == 0 {
		return nil, fmt.Errorf("%w: effort order is empty", domain.ErrSwarmInvalid)
	}
	positions := make(map[string]int, len(order))
	for i, effort := range order {
		_, duplicate := positions[effort]
		if !domain.SwarmValidID(effort) || duplicate {
			return nil, fmt.Errorf("%w: effort order is invalid or ambiguous", domain.ErrSwarmInvalid)
		}
		positions[effort] = i
	}
	return positions, nil
}

// WorstWindow reports the largest known usage percentage (100 means fully
// used), including token-based observations when available. Nil means no known
// usage; a returned value does not establish completeness or freshness. Callers
// must select the appropriate quota scopes and perform admission separately.
func WorstWindow(snapshot domain.SwarmQuotaSnapshot) (*float64, error) {
	if err := domain.ValidateSwarmQuota(snapshot); err != nil {
		return nil, err
	}
	var worst *float64
	for _, window := range snapshot.Windows {
		var used *float64
		if window.UsedPercent != nil {
			value := *window.UsedPercent
			used = &value
		}
		if window.LimitTokens != nil && window.UsedTokens != nil {
			value := float64(*window.UsedTokens) / float64(*window.LimitTokens) * 100
			if used == nil || value > *used {
				used = &value
			}
		}
		if used != nil && (worst == nil || *used > *worst) {
			worst = used
		}
	}
	return worst, nil
}

func quotaUnknown(snapshot domain.SwarmQuotaSnapshot, scope string) bool {
	exactScope := false
	for _, window := range snapshot.Windows {
		if window.Scope != "*" && window.Scope != scope {
			continue
		}
		if window.Scope == scope {
			exactScope = true
		}
		if window.LimitTokens == nil || window.UsedTokens == nil {
			return true
		}
	}
	// Account-wide capacity constrains every route, but cannot establish that
	// an absent model-specific meter is known or available.
	return !exactScope
}
