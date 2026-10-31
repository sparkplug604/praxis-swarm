package planning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
)

// LeadProposal contains project decisions; the builder supplies machine identities.
type LeadProposal struct {
	Brief         *domain.SwarmBriefRef `json:"brief,omitempty"`
	ProjectID     domain.ProjectID      `json:"projectId"`
	Objective     string                `json:"objective"`
	Specification string                `json:"specification"`
	Model         string                `json:"model"`
	Effort        string                `json:"effort"`
	TokenLimit    int64                 `json:"tokenLimit"`
	Tools         []ProposalTool        `json:"tools"`
	Tasks         []ProposalTask        `json:"tasks"`
}

// ProposalTool names an executable; version is supplied metadata, never executed.
type ProposalTool struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// ProposalTask carries semantic requirements without hashes or source identifiers.
type ProposalTask struct {
	ID           string                  `json:"id"`
	Objective    string                  `json:"objective"`
	Paths        []string                `json:"paths"`
	NonGoals     []string                `json:"nonGoals"`
	Interfaces   []domain.SwarmInterface `json:"interfaces"`
	Errors       []domain.SwarmErrorRule `json:"errors"`
	Acceptance   domain.SwarmAcceptance  `json:"acceptance"`
	ProgressPath string                  `json:"progressPath"`
}

// PreparedProposal holds validated execution input and preview packets.
// Run startup binds final packet identities to the actual run ID.
type PreparedProposal struct {
	Plan    domain.SwarmPlan   `json:"plan"`
	Context []SuppliedContext  `json:"context"`
	Packets []SupervisedPacket `json:"packets"`
}

// ParseProposal rejects duplicate/unknown fields, aliases and omitted exit expectations.
func ParseProposal(raw []byte) (LeadProposal, error) {
	var p LeadProposal
	if len(raw) == 0 || len(raw) > MaxPlannerBytes || !utf8.Valid(raw) {
		return p, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := planningScanJSON(d, reflect.TypeOf(p), 0); err != nil {
		return p, planningInvalid("proposal: %v", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return p, planningInvalid("trailing proposal JSON")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, planningInvalid("proposal: %v", err)
	}
	return p, nil
}

// BuildProposal derives source/context/tool identities and reuses supervised validation.
// It launches no models or acceptance commands and grants no execution authority.
func BuildProposal(ctx context.Context, p LeadProposal, snapshot workspace.Snapshot, verifier ToolVerifier) (PreparedProposal, error) {
	out := PreparedProposal{}
	raw, err := json.Marshal(p)
	if err != nil {
		return out, err
	}
	p, err = ParseProposal(raw)
	if err != nil {
		return out, err
	}
	if ctx == nil || verifier == nil || len(p.Tasks) != 2 || len(p.Tools) < 1 || len(p.Tools) > 32 || p.TokenLimit < 2 || !planningText(p.Specification, MaxPacketBytes) {
		return out, planningInvalid("proposal requires specification, two tasks and tokenLimit >= 2")
	}
	if err := workspace.ValidateForLaunch(ctx, snapshot); err != nil {
		return out, err
	}
	tools := []domain.SwarmToolIdentity{}
	paths := map[string]string{}
	for _, t := range p.Tools {
		canonical, err := filepath.EvalSymlinks(t.Path)
		if err != nil {
			return out, fmt.Errorf("tool %q: %w", t.Name, err)
		}
		if !filepath.IsAbs(canonical) {
			return out, planningInvalid("tool %q requires an absolute path", t.Name)
		}
		f, err := openToolFile(canonical)
		if err != nil {
			return out, err
		}
		info, statErr := f.Stat()
		h := sha256.New()
		n, readErr := io.Copy(h, io.LimitReader(f, (1<<30)+1))
		closeErr := f.Close()
		if statErr != nil {
			return out, statErr
		}
		if readErr != nil {
			return out, readErr
		}
		if closeErr != nil {
			return out, closeErr
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || n > 1<<30 {
			return out, planningInvalid("tool %q must be a bounded executable file", t.Name)
		}
		tools = append(tools, domain.SwarmToolIdentity{Name: t.Name, Path: canonical, Version: t.Version, SHA256: hex.EncodeToString(h.Sum(nil))})
		paths[t.Name] = canonical
	}
	plan := domain.SwarmPlan{ProtocolVersion: 1, Version: 1, ProjectID: p.ProjectID, SourceCommit: snapshot.Source.Head, Objective: p.Objective, BudgetBucket: "lead-supervised-advisory", TokenLimit: p.TokenLimit, MaxActive: 2}
	if p.Brief != nil {
		ref := *p.Brief
		plan.Brief = &ref
	}
	for _, t := range p.Tasks {
		progressFound := false
		for _, entry := range snapshot.Source.Entries {
			if entry.Path == t.ProgressPath && !entry.Missing && !entry.Directory {
				progressFound = true
				break
			}
		}
		if !progressFound {
			return out, planningInvalid("task %q progressPath must name an existing captured file", t.ID)
		}

		// Only relevant captured file identities enter each packet, not the complete repository.
		entries := []workspace.Entry{}
		for _, entry := range snapshot.Source.Entries {
			for _, scope := range append(append([]string{}, t.Paths...), t.ProgressPath) {
				key := strings.TrimSuffix(scope, "/**")
				if entry.Path == key || strings.HasPrefix(entry.Path, key+"/") {
					entries = append(entries, entry)
					break
				}
			}
		}
		manifest, _ := json.Marshal(struct {
			SourceCommit string            `json:"sourceCommit"`
			Files        []workspace.Entry `json:"files"`
		}{snapshot.Source.Head, entries})
		contexts := []SuppliedContext{{ID: t.ID + "-spec", Body: p.Specification}, {ID: t.ID + "-source", Body: string(manifest)}}
		refs := []domain.SwarmContextRef{}
		for i, c := range contexts {
			sum := sha256.Sum256([]byte(c.Body))
			kind := "spec"
			if i == 1 {
				kind = "source_manifest"
			}
			refs = append(refs, domain.SwarmContextRef{ID: c.ID, Kind: kind, SHA256: hex.EncodeToString(sum[:]), Required: true})
		}
		resources := []domain.SwarmResource{}
		for _, path := range t.Paths {
			resources = append(resources, domain.SwarmResource{Namespace: "repository", Kind: "path", Key: path})
		}
		// Check argv[0] names a declared tool; do not discover executables through PATH.
		work := &domain.SwarmWorkSpec{Version: 1, TaskType: "implementation", ContextByteLimit: 32768, NonGoals: t.NonGoals, Interfaces: t.Interfaces, Errors: t.Errors, Acceptance: t.Acceptance, Context: refs, Tools: tools, ProgressPath: t.ProgressPath, OnComplete: "submit_and_stop", OnBlocked: "report_and_stop"}
		for i, c := range work.Acceptance.Checks {
			if len(c.Command) == 0 {
				return out, planningInvalid("task %q check %q has no command", t.ID, c.ID)
			}
			canonical, ok := paths[c.Command[0]]
			if !ok {
				return out, planningInvalid("task %q check %q must name a declared tool", t.ID, c.ID)
			}
			work.Acceptance.Checks[i].Command = append([]string{canonical}, c.Command[1:]...)
		}
		plan.Tasks = append(plan.Tasks, domain.SwarmTaskContract{ID: t.ID, Role: "worker", Version: 1, Objective: t.Objective, OracleHash: domain.SwarmHash(t.Acceptance), Agent: domain.SwarmSelection{Harness: domain.HarnessCodex, Model: p.Model, Effort: p.Effort}, Resources: resources, ReserveTokens: p.TokenLimit / 2, Work: work})
		out.Context = append(out.Context, contexts...)
	}
	if err := ValidateSupervisedPlan(plan); err != nil {
		return out, err
	}
	out.Plan = plan
	for _, t := range plan.Tasks {
		packet, err := ComposeSupervised(ctx, "lead-preview", plan, t.ID, out.Context, verifier)
		if err != nil {
			return PreparedProposal{}, err
		}
		out.Packets = append(out.Packets, packet)
	}
	if err := workspace.ValidateForLaunch(ctx, snapshot); err != nil {
		return PreparedProposal{}, err
	}
	return out, nil
}
