package supervised

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
)

func leadFixture(t *testing.T) (*serviceFixture, planning.LeadProposal) {
	t.Helper()
	f := newServiceFixture(t)
	var plan domain.SwarmPlan
	if err := json.Unmarshal(f.input.Plan, &plan); err != nil {
		t.Fatal(err)
	}
	p := planning.LeadProposal{ProjectID: plan.ProjectID, Objective: plan.Objective, Specification: "Change assigned fixture files only; preserve error semantics.", Model: plan.Tasks[0].Agent.Model, Effort: plan.Tasks[0].Agent.Effort, TokenLimit: plan.TokenLimit}
	for _, tool := range plan.Tasks[0].Work.Tools {
		p.Tools = append(p.Tools, planning.ProposalTool{Name: tool.Name, Path: tool.Path, Version: tool.Version})
	}
	for _, task := range plan.Tasks {
		work := task.Work
		for i := range work.Acceptance.Checks {
			work.Acceptance.Checks[i].Command[0] = p.Tools[0].Name
		}
		p.Tasks = append(p.Tasks, planning.ProposalTask{ID: task.ID, Objective: task.Objective, Paths: []string{task.Resources[0].Key}, NonGoals: work.NonGoals, Interfaces: work.Interfaces, Errors: work.Errors, Acceptance: work.Acceptance, ProgressPath: work.ProgressPath})
	}
	return f, p
}

func TestLeadPrepareExecuteReview(t *testing.T) {
	ctx := context.Background()
	f, p := leadFixture(t)
	prepared, err := f.service.Prepare(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if f.runtime.launches != 0 || len(prepared.Packets) != 2 {
		t.Fatal("prepare launched workers or lost packets")
	}
	if prepared.Plan.Tasks[0].Agent.Model != p.Model || prepared.Plan.SourceCommit == "" {
		t.Fatal("lost explicit model or source identity")
	}
	if p.Tasks[0].Acceptance.Checks[0].Command[0] != p.Tools[0].Name {
		t.Fatal("builder mutated proposal")
	}
	raw, _ := json.Marshal(prepared.Plan)
	f.input.Plan = raw
	f.input.Context = prepared.Context
	f.runtime.complete = true
	run, err := f.service.Start(ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, f.service, run.ID)
	if run.Status != "completed" {
		t.Fatalf("%+v", run)
	}
	// Replay consumes the same launch intent rather than starting replacement workers.
	if _, err := f.service.Start(ctx, f.input); err != nil {
		t.Fatal(err)
	}
	if f.runtime.launches != 2 {
		t.Fatalf("launch count %d", f.runtime.launches)
	}
	review, err := f.service.Review(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		body, err := os.ReadFile(filepath.Join(review.Directory, name+".txt"))
		if err != nil || string(body) != "candidate "+name+"\n" {
			t.Fatalf("combined candidate %s: %q %v", name, body, err)
		}
	}
	if len(review.Checks) != 2 || !strings.Contains(review.Instructions, "not an accepted integration") {
		t.Fatal("review lost checks or acceptance distinction")
	}
	if err := sourceMatches(run.SourceSnapshot, f.source); err != nil {
		t.Fatal(err)
	}
	// A frozen-file mutation must prevent a fresh review from declaring valid evidence.
	snapshot, err := loadSnapshot(run.Tasks[0].SnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot.Destination.Path, "a.txt"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Review(ctx, run.ID); err == nil {
		t.Fatal("tampered capture accepted")
	}
}

func TestLeadPrepareRejectsInvalidBeforeLaunch(t *testing.T) {
	for _, kind := range []string{"overlap", "missing-error", "unknown-tool", "dirty-source", "empty-spec"} {
		t.Run(kind, func(t *testing.T) {
			f, p := leadFixture(t)
			switch kind {
			case "overlap":
				p.Tasks[1].Paths = p.Tasks[0].Paths
			case "missing-error":
				p.Tasks[0].Errors = nil
			case "unknown-tool":
				p.Tasks[0].Acceptance.Checks[0].Command[0] = "undeclared"
			case "dirty-source":
				if err := os.WriteFile(filepath.Join(f.source, "a.txt"), []byte("existing change"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "empty-spec":
				p.Specification = ""
			}
			if _, err := f.service.Prepare(context.Background(), p); err == nil {
				t.Fatal("invalid proposal accepted")
			}
			if f.runtime.launches != 0 {
				t.Fatal("invalid preparation launched")
			}
		})
	}
}
