package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestDiscoveryHistoryCASAndReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlitetest.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	update := domain.SwarmDiscoveryUpdate{Brief: domain.SwarmBriefDraft{Goal: "Initial goal"}}
	first, err := s.SaveSwarmDiscovery(ctx, "project", update, time.Now())
	if err != nil || first.Revision != 1 || first.Status != "needs_input" {
		t.Fatal(first, err)
	}
	update.ExpectedRevision = 1
	update.Brief.Goal = "Revised goal"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.SaveSwarmDiscovery(ctx, "project", update, time.Now())
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	ok, stale := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, domain.ErrSwarmStale) {
			stale++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || stale != 1 {
		t.Fatal("concurrent revision overwritten", ok, stale)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.OpenPreMigrated(dir)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := s.GetSwarmDiscovery(ctx, "project", 1)
	if err != nil || prior.SHA256 != first.SHA256 || prior.Brief.Goal != "Initial goal" {
		t.Fatal(prior, err)
	}
	latest, err := s.GetSwarmDiscovery(ctx, "project", 0)
	if err != nil || latest.Revision != 2 || latest.Brief.Goal != "Revised goal" {
		t.Fatal(latest, err)
	}
	if _, err = s.GetSwarmDiscovery(ctx, "other", 0); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatal(err)
	}
	db := supervisedRawDB(t, dir)
	for _, q := range []string{"UPDATE swarm_discovery_briefs SET payload_json='{}'", "DELETE FROM swarm_discovery_briefs"} {
		if _, err := db.ExecContext(ctx, q); err == nil {
			t.Fatal("history mutable", q)
		}
	}
}
