package qualification

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func validRole(role Role) bool {
	return role == Planner || role == Worker || role == Reviewer || role == Integration
}

func validateSelection(selection Selection) error {
	if selection.AgentID == "" || selection.Model == "" || selection.AgentID != strings.TrimSpace(selection.AgentID) || selection.Model != strings.TrimSpace(selection.Model) || selection.Effort != strings.TrimSpace(selection.Effort) {
		return fmt.Errorf("%w: select an explicit adapter/model and an exact advertised effort", ErrInvalid)
	}
	return nil
}

func validateCatalog(ctx context.Context, selection Selection, catalog ports.AgentModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if catalog.WarningCode == ports.ModelCatalogWarningAuthRequired || catalog.WarningCode == ports.ModelCatalogWarningAuthExpired {
		return fmt.Errorf("%w: authenticate %s using its existing account settings", ports.ErrAgentAuthRequired, selection.AgentID)
	}
	if catalog.AgentID != selection.AgentID || catalog.InputFingerprint == "" || catalog.Stale || catalog.RefreshState == "error" || catalog.RefreshState == "refreshing" || catalog.RefreshState == "queued" {
		return fmt.Errorf("%w: refresh the selected adapter's catalog before dispatch", ports.ErrModelCapabilitiesUnavailable)
	}
	if catalog.SelectionMode == ports.ModelSelectionModeList {
		return fmt.Errorf("%w: adapter exposes modes, not explicit model identities", ports.ErrModelCapabilitiesUnavailable)
	}
	for _, model := range catalog.Models {
		if model.ID != selection.Model {
			continue
		}
		if selection.Effort != "" && !slices.Contains(model.Efforts, selection.Effort) {
			return fmt.Errorf("%w: %q is not advertised by %s; available: %v", ports.ErrUnsupportedEffort, selection.Effort, selection.Model, model.Efforts)
		}
		if len(catalog.RunnableModels) != 0 && !slices.Contains(catalog.RunnableModels, selection.Model) {
			return fmt.Errorf("%w: %s is not in the adapter's runnable models", ErrUnavailable, selection.Model)
		}
		return nil
	}
	return fmt.Errorf("%w: %s is absent from %s's refreshed catalog; select a listed model or an explicit role fallback", ErrUnavailable, selection.Model, selection.AgentID)
}

// RecordReportedIdentity records only runtime-reported values. Missing telemetry
// stays unknown and is not filled from a requested/resolved model or command line.
func RecordReportedIdentity(report Report, model, effort string) (Report, error) {
	if model != "" {
		report.ReportedModel = model
	}
	if effort != "" {
		report.ReportedEffort = effort
	}
	model, effort = report.ReportedModel, report.ReportedEffort
	report.Telemetry = "unknown"
	if model != "" || effort != "" {
		report.Telemetry = "reported"
	}
	if (model != "" && model != report.Resolved.Model) || (effort != "" && report.Resolved.Effort != "" && effort != report.Resolved.Effort) {
		report.Telemetry = "mismatch"
		report.LiveQualified = false
		return report, fmt.Errorf("%w: pause this attempt and review the adapter's reported model/effort", ErrIdentityMismatch)
	}
	return report, nil
}
