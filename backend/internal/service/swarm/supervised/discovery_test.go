package supervised

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestDiscoveryBlocksIncompleteAndStaleLaunches(t *testing.T) {
	ctx := context.Background()
	f, p := leadFixture(t)
	store, err := sqlitetest.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f.service.store = store
	draft := domain.SwarmBriefDraft{Goal: "Implement independent helpers", Questions: []domain.SwarmDiscoveryQuestion{{ID: "users", Question: "Who will use them?", Required: true}}}
	first, err := f.service.SaveDiscovery(ctx, p.ProjectID, domain.SwarmDiscoveryUpdate{Brief: draft})
	if err != nil {
		t.Fatal(err)
	}
	p.Brief = &domain.SwarmBriefRef{Revision: first.Revision, SHA256: first.SHA256}
	if _, err = f.service.Prepare(ctx, p); !errors.Is(err, domain.ErrSwarmNotReady) {
		t.Fatal("incomplete brief prepared", err)
	}
	draft.Audience = "Fixture users"
	draft.Outcomes = []string{"Implement both helpers"}
	draft.Acceptance = []string{"Existing named tests pass"}
	draft.Questions[0].Answer = "Fixture users"
	ready, err := f.service.SaveDiscovery(ctx, p.ProjectID, domain.SwarmDiscoveryUpdate{ExpectedRevision: 1, Brief: draft})
	if err != nil {
		t.Fatal(err)
	}
	p.Brief = &domain.SwarmBriefRef{Revision: ready.Revision, SHA256: ready.SHA256}
	prepared, err := f.service.Prepare(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	hasBrief := false
	for _, item := range prepared.Context {
		hasBrief = hasBrief || strings.Contains(item.Body, "Fixture users")
	}
	if prepared.Plan.Brief == nil || prepared.Plan.Brief.Revision != 2 || !hasBrief {
		t.Fatal("brief not bound into prepared context", prepared.Plan.Brief)
	}
	raw, _ := json.Marshal(prepared.Plan)
	f.input.Plan = raw
	f.input.Context = prepared.Context
	draft.Constraints = []string{"A newly supplied constraint"}
	revised, err := f.service.SaveDiscovery(ctx, p.ProjectID, domain.SwarmDiscoveryUpdate{ExpectedRevision: 2, Brief: draft})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Start(ctx, f.input); !errors.Is(err, domain.ErrSwarmStale) {
		t.Fatal("stale plan launched", err)
	}
	if f.runtime.launches != 0 {
		t.Fatal("stale discovery launched workers")
	}
	prepared.Plan.Brief = nil
	f.input.Plan, _ = json.Marshal(prepared.Plan)
	if _, err = f.service.Start(ctx, f.input); !errors.Is(err, domain.ErrSwarmStale) {
		t.Fatal("stripped binding bypassed discovery", err)
	}
	p.Brief = &domain.SwarmBriefRef{Revision: revised.Revision, SHA256: revised.SHA256}
	prepared, err = f.service.Prepare(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	f.input.Plan, _ = json.Marshal(prepared.Plan)
	f.input.Context = prepared.Context
	f.runtime.complete = true
	run, err := f.service.Start(ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, f.service, run.ID)
	if run.Status != "completed" {
		t.Fatal(run.Status)
	}
	if _, err = f.service.SaveDiscovery(ctx, p.ProjectID, domain.SwarmDiscoveryUpdate{ExpectedRevision: 3, Brief: draft}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Start(ctx, f.input); err != nil {
		t.Fatal("historical replay rejected", err)
	}
	if f.runtime.launches != 2 {
		t.Fatal("replay launched replacements", f.runtime.launches)
	}
	restarted := New(ctx, store, f.runtime, t.TempDir())
	defer restarted.Close()
	latest, err := restarted.Discovery(ctx, p.ProjectID, 0)
	if err != nil || latest.Revision != 4 {
		t.Fatal(latest, err)
	}
}
