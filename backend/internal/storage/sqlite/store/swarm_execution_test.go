package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func executionReceipt(a domain.SwarmAttempt) domain.SwarmExecutionReceipt {
	return domain.SwarmExecutionReceipt{Version: 1, AttemptID: a.ID, OperationID: a.OperationID, Owner: a.Owner,
		SessionID: "session-" + a.ID, CapabilityFingerprint: a.CapabilityFingerprint,
		QualificationBinding: strings.Repeat("1", 64), PacketHash: strings.Repeat("2", 64),
		WorkspaceIdentityHash: a.Schedule.WorkspaceIdentityHash, SourceManifestHash: a.Schedule.SourceManifestHash,
		WorkspaceRoot: a.Schedule.WorkspaceRoot, ConfigHash: strings.Repeat("3", 64), PermissionReceiptID: "test-permission",
		EvidenceMode: "test", Generation: a.Generation, CreatedAt: swarmNow, ExpiresAt: swarmNow.Add(30 * time.Second)}
}

func executionFixture(t *testing.T) (scheduleFixture, domain.SwarmAttempt, domain.SwarmExecutionReceipt) {
	t.Helper()
	f := newScheduleFixture(t, schedulePolicy())
	a, err := scheduleClaim(f.left, f.claim("one", "a"), swarmNow)
	if err != nil {
		t.Fatal(err)
	}
	a, launch, err := f.left.BeginSwarmDispatch(context.Background(), swarmFenceFor(a), swarmNow)
	if err != nil || !launch {
		t.Fatalf("begin dispatch: %v %v", launch, err)
	}
	return f, a, executionReceipt(a)
}

func TestSwarmExecutionAtomicAuthorizationAcrossStores(t *testing.T) {
	f, a, r := executionFixture(t)
	ctx := context.Background()
	stores := []*sqlite.Store{f.left, f.right}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var allowed [2]bool
	var errs [2]error
	for i := range stores {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			allowed[i], errs[i] = stores[i].AuthorizeSwarmExecution(ctx, swarmFenceFor(a), r, swarmNow)
		}(i)
	}
	close(start)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || allowed[0] == allowed[1] {
		t.Fatalf("want exactly one authorization: allowed=%v errors=%v", allowed, errs)
	}
	got, err := f.right.GetSwarmExecution(ctx, a.ID)
	if err != nil || domain.SwarmHash(got) != domain.SwarmHash(r) {
		t.Fatalf("stored receipt: %+v %v", got, err)
	}
	if _, err := f.right.GetSwarmExecutionResult(ctx, a.ID); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatalf("authorization fabricated a result: %v", err)
	}
	if err := f.left.CancelSwarmRun(ctx, a.RunID); err != nil {
		t.Fatal(err)
	}
	later := swarmNow.Add(2 * time.Minute)
	if _, err := f.left.TakeOverSwarmLease(ctx, swarmFenceFor(a), "new-owner", later, later.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// A durable replay can report the original authorization after any ownership
	// change; it never grants a second launch or silently rebinds the receipt.
	if allowed, err := f.right.AuthorizeSwarmExecution(ctx, swarmFenceFor(a), r, later); err != nil || allowed {
		t.Fatalf("expired cancelled replay: %v %v", allowed, err)
	}
}

func TestSwarmExecutionRejectsChangedReceipt(t *testing.T) {
	f, a, original := executionFixture(t)
	ctx := context.Background()
	if allowed, err := f.left.AuthorizeSwarmExecution(ctx, swarmFenceFor(a), original, swarmNow); err != nil || !allowed {
		t.Fatalf("authorize: %v %v", allowed, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*domain.SwarmExecutionReceipt)
	}{
		{"operation", func(r *domain.SwarmExecutionReceipt) { r.OperationID += "-changed" }},
		{"session", func(r *domain.SwarmExecutionReceipt) { r.SessionID += "-changed" }},
		{"owner", func(r *domain.SwarmExecutionReceipt) { r.Owner += "-changed" }},
		{"generation", func(r *domain.SwarmExecutionReceipt) { r.Generation++ }},
		{"capability", func(r *domain.SwarmExecutionReceipt) { r.CapabilityFingerprint = strings.Repeat("0", 64) }},
		{"qualification", func(r *domain.SwarmExecutionReceipt) { r.QualificationBinding = strings.Repeat("0", 64) }},
		{"packet", func(r *domain.SwarmExecutionReceipt) { r.PacketHash = strings.Repeat("0", 64) }},
		{"identity", func(r *domain.SwarmExecutionReceipt) { r.WorkspaceIdentityHash = strings.Repeat("0", 64) }},
		{"manifest", func(r *domain.SwarmExecutionReceipt) { r.SourceManifestHash = strings.Repeat("0", 64) }},
		{"root", func(r *domain.SwarmExecutionReceipt) { r.WorkspaceRoot += "-changed" }},
		{"config", func(r *domain.SwarmExecutionReceipt) { r.ConfigHash = strings.Repeat("0", 64) }},
		{"permission", func(r *domain.SwarmExecutionReceipt) { r.PermissionReceiptID += "-changed" }},
		{"evidence", func(r *domain.SwarmExecutionReceipt) { r.EvidenceMode = "live" }},
		{"created", func(r *domain.SwarmExecutionReceipt) { r.CreatedAt = r.CreatedAt.Add(time.Nanosecond) }},
		{"expiry", func(r *domain.SwarmExecutionReceipt) { r.ExpiresAt = r.ExpiresAt.Add(time.Nanosecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := original
			tc.edit(&r)
			fence := domain.SwarmFence{AttemptID: r.AttemptID, Owner: r.Owner, Generation: r.Generation}
			allowed, err := f.right.AuthorizeSwarmExecution(ctx, fence, r, swarmNow)
			if allowed || !errors.Is(err, domain.ErrSwarmConflict) {
				t.Fatalf("changed receipt: %v %v", allowed, err)
			}
		})
	}
	// A second legitimate scheduled attempt cannot reuse the first session ID.
	b, err := scheduleClaim(f.left, f.claim("one", "b"), swarmNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.left.BeginSwarmDispatch(ctx, swarmFenceFor(b), swarmNow); err != nil {
		t.Fatal(err)
	}
	r := executionReceipt(b)
	r.SessionID = original.SessionID
	if allowed, err := f.right.AuthorizeSwarmExecution(ctx, swarmFenceFor(b), r, swarmNow); allowed || !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("duplicate session: %v %v", allowed, err)
	}
	r.SessionID, r.OperationID = "other-session", original.OperationID
	if allowed, err := f.right.AuthorizeSwarmExecution(ctx, swarmFenceFor(b), r, swarmNow); allowed || !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("duplicate operation: %v %v", allowed, err)
	}
}

func TestSwarmExecutionRejectsUnreadyAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *scheduleFixture, domain.SwarmAttempt, *domain.SwarmExecutionReceipt, *domain.SwarmFence, *time.Time)
		want   error
	}{
		{"expired receipt", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, now *time.Time) {
			*now = r.ExpiresAt
		}, domain.ErrSwarmStale},
		{"future receipt", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			r.CreatedAt = r.CreatedAt.Add(time.Second)
		}, domain.ErrSwarmStale},
		{"expired lease", func(_ *testing.T, _ *scheduleFixture, a domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, now *time.Time) {
			*now = a.LeaseUntil
			r.ExpiresAt = now.Add(time.Minute)
		}, domain.ErrSwarmStale},
		{"stale owner", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, f *domain.SwarmFence, _ *time.Time) {
			r.Owner = "wrong"
			f.Owner = r.Owner
		}, domain.ErrSwarmStale},
		{"stale generation", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, f *domain.SwarmFence, _ *time.Time) {
			r.Generation++
			f.Generation = r.Generation
		}, domain.ErrSwarmStale},
		{"mismatched fence", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, _ *domain.SwarmExecutionReceipt, f *domain.SwarmFence, _ *time.Time) {
			f.AttemptID = "different"
		}, domain.ErrSwarmInvalid},
		{"operation", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			r.OperationID += "-wrong"
		}, domain.ErrSwarmConflict},
		{"capability", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			r.CapabilityFingerprint = strings.Repeat("0", 64)
		}, domain.ErrSwarmConflict},
		{"workspace", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			r.WorkspaceRoot += "-wrong"
		}, domain.ErrSwarmConflict},
		{"identity", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			r.WorkspaceIdentityHash = strings.Repeat("0", 64)
		}, domain.ErrSwarmConflict},
		{"manifest", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			r.SourceManifestHash = strings.Repeat("0", 64)
		}, domain.ErrSwarmConflict},
		{"before attempt", func(_ *testing.T, _ *scheduleFixture, _ domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			r.CreatedAt = r.CreatedAt.Add(-time.Second)
		}, domain.ErrSwarmConflict},
		{"cancelled", func(t *testing.T, f *scheduleFixture, a domain.SwarmAttempt, _ *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			if err := f.left.CancelSwarmRun(context.Background(), a.RunID); err != nil {
				t.Fatal(err)
			}
		}, domain.ErrSwarmNotReady},
		{"running", func(t *testing.T, f *scheduleFixture, a domain.SwarmAttempt, r *domain.SwarmExecutionReceipt, _ *domain.SwarmFence, _ *time.Time) {
			if err := f.left.ConfirmSwarmRunning(context.Background(), swarmFenceFor(a), r.SessionID, "", swarmNow); err != nil {
				t.Fatal(err)
			}
		}, domain.ErrSwarmNotReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, a, r := executionFixture(t)
			fence, now := swarmFenceFor(a), swarmNow
			tc.change(t, &f, a, &r, &fence, &now)
			allowed, err := f.right.AuthorizeSwarmExecution(context.Background(), fence, r, now)
			if allowed || !errors.Is(err, tc.want) {
				t.Fatalf("authorization: %v %v want %v", allowed, err, tc.want)
			}
			if _, err := f.left.GetSwarmExecution(context.Background(), a.ID); !errors.Is(err, domain.ErrSwarmNotFound) {
				t.Fatalf("rejection stored receipt: %v", err)
			}
		})
	}
}

func TestSwarmExecutionRejectsReservedAndUnboundAttempts(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t, schedulePolicy())
	a, err := scheduleClaim(f.left, f.claim("one", "a"), swarmNow)
	if err != nil {
		t.Fatal(err)
	}
	r := executionReceipt(a)
	if allowed, err := f.right.AuthorizeSwarmExecution(ctx, swarmFenceFor(a), r, swarmNow); allowed || !errors.Is(err, domain.ErrSwarmNotReady) {
		t.Fatalf("reserved authorization: %v %v", allowed, err)
	}
	legacy := sqlitetest.MustOpenAt(t, t.TempDir())
	seedProject(t, legacy, "swarm-project")
	swarmCreate(t, legacy, "legacy", swarmPlan())
	old := swarmReserve(t, legacy, "legacy", "a", "legacy")
	if _, _, err := legacy.BeginSwarmDispatch(ctx, swarmFenceFor(old), swarmNow); err != nil {
		t.Fatal(err)
	}
	r.AttemptID, r.OperationID, r.Owner, r.Generation = old.ID, old.OperationID, old.Owner, old.Generation
	r.CapabilityFingerprint = old.CapabilityFingerprint
	if allowed, err := legacy.AuthorizeSwarmExecution(ctx, swarmFenceFor(old), r, swarmNow); allowed || !errors.Is(err, domain.ErrSwarmNotReady) {
		t.Fatalf("unbound authorization: %v %v", allowed, err)
	}
}

func TestSwarmExecutionResultPersistsLateAfterRestart(t *testing.T) {
	f, a, r := executionFixture(t)
	ctx := context.Background()
	if allowed, err := f.left.AuthorizeSwarmExecution(ctx, swarmFenceFor(a), r, swarmNow); err != nil || !allowed {
		t.Fatalf("authorize: %v %v", allowed, err)
	}
	if err := f.left.CancelSwarmRun(ctx, a.RunID); err != nil {
		t.Fatal(err)
	}
	later := swarmNow.Add(time.Hour)
	if _, err := f.left.TakeOverSwarmLease(ctx, swarmFenceFor(a), "recovery", later, later.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := f.right.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.right, err = sqlite.OpenPreMigrated(filepath.Dir(f.root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.right.Close() })
	got, err := f.right.GetSwarmExecution(ctx, a.ID)
	if err != nil || domain.SwarmHash(got) != domain.SwarmHash(r) {
		t.Fatalf("receipt after restart: %+v %v", got, err)
	}
	result := domain.SwarmExecutionResult{AttemptID: r.AttemptID, ReceiptHash: domain.SwarmHash(r), SessionID: r.SessionID,
		HandleID: "runtime-handle", AdditionalHandles: []string{"fallback-handle", "returned-handle"},
		Outcome: "possible", RuntimeEffect: "possible", RuntimeCleanup: "failed", CreatedAt: later}
	for i := 0; i < 2; i++ {
		if err := f.right.RecordSwarmExecutionResult(ctx, result); err != nil {
			t.Fatalf("late result attempt %d: %v", i, err)
		}
	}
	if err := f.right.Close(); err != nil {
		t.Fatal(err)
	}
	f.right, err = sqlite.OpenPreMigrated(filepath.Dir(f.root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.right.Close() })
	persisted, err := f.right.GetSwarmExecutionResult(ctx, a.ID)
	if err != nil || domain.SwarmHash(persisted) != domain.SwarmHash(result) {
		t.Fatalf("result after restart: %+v %v", persisted, err)
	}
	result.Outcome = "created"
	if err := f.left.RecordSwarmExecutionResult(ctx, result); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("replaced possible result: %v", err)
	}
	if allowed, err := f.right.AuthorizeSwarmExecution(ctx, swarmFenceFor(a), r, later); err != nil || allowed {
		t.Fatalf("result allowed relaunch: %v %v", allowed, err)
	}
}

func TestSwarmExecutionResultRejectsUnboundObservation(t *testing.T) {
	f, a, r := executionFixture(t)
	ctx := context.Background()
	result := domain.SwarmExecutionResult{AttemptID: a.ID, ReceiptHash: domain.SwarmHash(r), SessionID: r.SessionID, Outcome: "no_effect", CreatedAt: swarmNow}
	if err := f.right.RecordSwarmExecutionResult(ctx, result); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatalf("result before authorization: %v", err)
	}
	if allowed, err := f.left.AuthorizeSwarmExecution(ctx, swarmFenceFor(a), r, swarmNow); err != nil || !allowed {
		t.Fatalf("authorize: %v %v", allowed, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*domain.SwarmExecutionResult)
		want error
	}{
		{"hash", func(r *domain.SwarmExecutionResult) { r.ReceiptHash = strings.Repeat("0", 64) }, domain.ErrSwarmConflict},
		{"session", func(r *domain.SwarmExecutionResult) { r.SessionID += "-changed" }, domain.ErrSwarmConflict},
		{"time", func(r *domain.SwarmExecutionResult) { r.CreatedAt = r.CreatedAt.Add(-time.Nanosecond) }, domain.ErrSwarmInvalid},
		{"no-effect handle", func(r *domain.SwarmExecutionResult) { r.HandleID = "possible-process" }, domain.ErrSwarmInvalid},
		{"created missing handle", func(r *domain.SwarmExecutionResult) { r.Outcome = "created" }, domain.ErrSwarmInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := result
			tc.edit(&bad)
			if err := f.right.RecordSwarmExecutionResult(ctx, bad); !errors.Is(err, tc.want) {
				t.Fatalf("invalid observation: %v want %v", err, tc.want)
			}
		})
	}
	if _, err := f.left.GetSwarmExecutionResult(ctx, a.ID); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatalf("invalid observation stored: %v", err)
	}
	if err := f.right.RecordSwarmExecutionResult(ctx, result); err != nil {
		t.Fatal(err)
	}
}
