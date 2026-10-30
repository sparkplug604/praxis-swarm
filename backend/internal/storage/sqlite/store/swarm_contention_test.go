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

// Different runs have different task identities and per-run active limits.
// Only the shared resource/budget transaction can serialize these admissions.
func TestSwarmCrossRunContentionAcrossStores(t *testing.T) {
	for _, tc := range []struct {
		name       string
		scopes     [2]string
		reserve    int64
		loserError error
	}{
		{"ancestor scope", [2]string{"src", "src/child"}, 30, domain.ErrSwarmConflict},
		{"shared budget", [2]string{"src/a", "src/b"}, 60, domain.ErrSwarmCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, dir := context.Background(), t.TempDir()
			first := sqlitetest.MustOpenAt(t, dir)
			second, err := sqlite.OpenPreMigrated(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := second.Close(); err != nil {
					t.Error(err)
				}
			})
			stores := [2]*sqlite.Store{first, second}
			runs := [2]string{"run-one", "run-two"}
			seedProject(t, first, "swarm-project")
			for i, run := range runs {
				plan := swarmPlan()
				plan.Tasks = plan.Tasks[:1]
				plan.Tasks[0].Resources[0].Key = tc.scopes[i]
				plan.Tasks[0].ReserveTokens = tc.reserve
				swarmCreate(t, stores[i], run, plan)
			}

			start := make(chan struct{})
			var wg sync.WaitGroup
			var attempts [2]domain.SwarmAttempt
			var errs [2]error
			claim := func(i int) (domain.SwarmAttempt, error) {
				return stores[i].ClaimSwarmTask(ctx,
					swarmClaim(runs[i], "a", "claim-"+runs[i]),
					"attempt-"+runs[i], "operation-"+runs[i],
					swarmNow, swarmNow.Add(time.Minute))
			}
			for i := range stores {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					attempts[i], errs[i] = claim(i)
				}(i)
			}
			close(start)
			wg.Wait()
			winner := 0
			if errs[0] != nil {
				winner = 1
			}
			loser := 1 - winner
			if errs[winner] != nil || !errors.Is(errs[loser], tc.loserError) {
				t.Fatalf("claims = %v; want one success and %v", errs, tc.loserError)
			}
			budget, err := second.GetSwarmBudget(ctx, "shared-account")
			if err != nil || budget.Reserved != tc.reserve || budget.Spent != 0 {
				t.Fatalf("budget after contention = %+v, %v", budget, err)
			}
			for i, run := range runs {
				items, err := first.ListSwarmAttempts(ctx, run)
				want := 0
				if i == winner {
					want = 1
				}
				if err != nil || len(items) != want {
					t.Fatalf("run %s attempts = %d, %v; want %d", run, len(items), err, want)
				}
			}

			// Once the winner is confirmed stopped and settled, the rejected
			// request must work unchanged: it left no identity or reservation.
			if err := stores[winner].FinishSwarmAttempt(ctx, swarmFenceFor(attempts[winner]), "failed", "", true, swarmNow); err != nil {
				t.Fatal(err)
			}
			if err := stores[winner].SettleSwarmUsage(ctx, domain.SwarmUsage{
				AttemptID: attempts[winner].ID, ReceiptID: "winner-stopped", ActualTokens: 0,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := claim(loser); err != nil {
				t.Fatalf("retry after capacity release: %v", err)
			}
			budget, err = first.GetSwarmBudget(ctx, "shared-account")
			if err != nil || budget.Reserved != tc.reserve || budget.Spent != 0 {
				t.Fatalf("budget after retry = %+v, %v", budget, err)
			}
		})
	}
}
