package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/qualification"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

type cancelStore struct {
	Store
	sessionmanager.SwarmExecutionStore
	attempt               domain.SwarmAttempt
	run                   domain.SwarmRun
	receipt               domain.SwarmExecutionReceipt
	result                domain.SwarmExecutionResult
	receiptErr, resultErr error
	onResult              func()
	finishes, confirms    int
}

func (s *cancelStore) GetSwarmAttempt(context.Context, string) (domain.SwarmAttempt, error) {
	return s.attempt, nil
}
func (s *cancelStore) GetSwarmRun(context.Context, string) (domain.SwarmRun, error) {
	return s.run, nil
}
func (s *cancelStore) GetSwarmExecution(context.Context, string) (domain.SwarmExecutionReceipt, error) {
	return s.receipt, s.receiptErr
}
func (s *cancelStore) GetSwarmExecutionResult(context.Context, string) (domain.SwarmExecutionResult, error) {
	if s.onResult != nil {
		s.onResult()
	}
	return s.result, s.resultErr
}
func (s *cancelStore) FinishSwarmAttempt(_ context.Context, _ domain.SwarmFence, state, _ string, stopped bool, _ time.Time) error {
	if !stopped || state != "cancelled" {
		return domain.ErrSwarmInvalid
	}
	s.finishes++
	return nil
}
func (s *cancelStore) ConfirmSwarmRunning(context.Context, domain.SwarmFence, string, string, time.Time) error {
	s.confirms++
	return nil
}

type cancelPort struct {
	ports.Runtime
	stop          func(context.Context, ports.FencedRuntimeRef) (ports.FencedStopResult, error)
	probe         ports.FencedProbeResult
	stops, probes int
	ref           ports.FencedRuntimeRef
}

func (p *cancelPort) StopFencedRuntime(ctx context.Context, ref ports.FencedRuntimeRef) (ports.FencedStopResult, error) {
	p.stops++
	p.ref = ref
	if p.stop != nil {
		return p.stop(ctx, ref)
	}
	return ports.FencedStopResult{SessionID: ref.SessionID, Generation: ref.Generation, Acknowledged: true, Effect: ports.RuntimeEffectApplied, Cleanup: ports.RuntimeCleanupSucceeded}, nil
}
func (p *cancelPort) ProbeFencedRuntime(_ context.Context, ref ports.FencedRuntimeRef) ports.FencedProbeResult {
	p.probes++
	return p.probe
}

func cancelFixture() (*Service, *ExactRuntime, *cancelStore, *cancelPort, time.Time) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	h := strings.Repeat("a", 64)
	a := domain.SwarmAttempt{ID: "attempt", RunID: "run", OperationID: "operation", Owner: "owner", Generation: 1, State: "running", LeaseUntil: now.Add(time.Hour), CreatedAt: now.Add(-time.Minute), CapabilityFingerprint: h,
		Schedule: &domain.SwarmScheduleBinding{WorkspaceRoot: "/fixture/worker", WorkspaceIdentityHash: h, SourceManifestHash: h}, SessionID: exactSessionID("operation")}
	receipt := domain.SwarmExecutionReceipt{Version: 1, AttemptID: a.ID, OperationID: a.OperationID, Owner: a.Owner, Generation: a.Generation, SessionID: a.SessionID, CapabilityFingerprint: h, QualificationBinding: h, PacketHash: h, WorkspaceRoot: a.Schedule.WorkspaceRoot, WorkspaceIdentityHash: h, SourceManifestHash: h, ConfigHash: h, PermissionReceiptID: "permission", EvidenceMode: "test", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	store := &cancelStore{attempt: a, run: domain.SwarmRun{ID: a.RunID, Cancelled: true}, receipt: receipt, result: domain.SwarmExecutionResult{AttemptID: a.ID, ReceiptHash: domain.SwarmHash(receipt), SessionID: a.SessionID, HandleID: "handle", Outcome: "created", CreatedAt: now}}
	port := &cancelPort{probe: ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}}
	runtime := &ExactRuntime{Launcher: sessionmanager.SwarmExactLauncher{Store: store, Runtime: port}, Now: func() time.Time { return now }}
	service := &Service{Store: store, Runtime: runtime, Owner: a.Owner, Now: runtime.Now, OperationTimeout: time.Second}
	return service, runtime, store, port, now
}

func TestExactCancelRequiresCurrentCancelledAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*cancelStore, *domain.SwarmAttempt, time.Time)
		want   error
	}{
		{"not cancelled", func(s *cancelStore, _ *domain.SwarmAttempt, _ time.Time) { s.run.Cancelled = false }, domain.ErrSwarmNotReady},
		{"expired", func(s *cancelStore, a *domain.SwarmAttempt, now time.Time) {
			s.attempt.LeaseUntil = now
			a.LeaseUntil = now
		}, domain.ErrSwarmStale},
		{"takeover", func(s *cancelStore, _ *domain.SwarmAttempt, _ time.Time) {
			s.attempt.Owner = "new-owner"
			s.attempt.Generation++
		}, domain.ErrSwarmStale},
		{"same owner new epoch", func(s *cancelStore, _ *domain.SwarmAttempt, _ time.Time) { s.attempt.Generation++ }, domain.ErrSwarmStale},
		{"renewed lease snapshot", func(s *cancelStore, _ *domain.SwarmAttempt, _ time.Time) {
			s.attempt.LeaseUntil = s.attempt.LeaseUntil.Add(time.Hour)
		}, domain.ErrSwarmStale},
		{"reserved", func(s *cancelStore, a *domain.SwarmAttempt, _ time.Time) {
			s.attempt.State = "reserved"
			a.State = "reserved"
		}, domain.ErrSwarmNotReady},
		{"finished", func(s *cancelStore, a *domain.SwarmAttempt, _ time.Time) {
			s.attempt.State = "cancelled"
			a.State = "cancelled"
		}, domain.ErrSwarmNotReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, runtime, store, port, now := cancelFixture()
			a := store.attempt
			tc.change(store, &a, now)
			_, err := runtime.Stop(context.Background(), a)
			if !errors.Is(err, tc.want) || port.stops != 0 || port.probes != 0 {
				t.Fatalf("err=%v stops=%d probes=%d", err, port.stops, port.probes)
			}
		})
	}
}

func TestExactCancelRechecksAuthorityImmediatelyBeforeEffect(t *testing.T) {
	for _, change := range []string{"takeover", "expiry", "cancelled flag"} {
		t.Run(change, func(t *testing.T) {
			_, runtime, store, port, now := cancelFixture()
			a := store.attempt
			store.onResult = func() {
				switch change {
				case "takeover":
					store.attempt.Owner = "other"
					store.attempt.Generation++
				case "expiry":
					runtime.Now = func() time.Time { return now.Add(2 * time.Hour) }
				case "cancelled flag":
					store.run.Cancelled = false
				}
			}
			if _, err := runtime.Stop(context.Background(), a); err == nil || port.stops != 0 {
				t.Fatalf("err=%v stops=%d", err, port.stops)
			}
		})
	}
}

func TestExactCancelRejectsUncertainAndMismatchedExecution(t *testing.T) {
	for name, change := range map[string]func(*cancelStore){
		"absent receipt": func(s *cancelStore) { s.receiptErr = domain.ErrSwarmNotFound },
		"pending result": func(s *cancelStore) { s.resultErr = domain.ErrSwarmNotFound },
		"possible":       func(s *cancelStore) { s.result.Outcome = "possible" },
		"unknown joined": func(s *cancelStore) {
			s.result.Outcome = "unknown"
			s.result.AdditionalHandles = []string{"other-handle"}
		},
		"created with additional handle": func(s *cancelStore) { s.result.AdditionalHandles = []string{"other-handle"} },
		"contradictory created":          func(s *cancelStore) { s.result.RuntimeEffect = "none" },
		"created cleanup failed":         func(s *cancelStore) { s.result.RuntimeCleanup = "failed" },
		"created cleanup succeeded":      func(s *cancelStore) { s.result.RuntimeCleanup = "succeeded" },
		"no effect cleanup failed": func(s *cancelStore) {
			s.result.Outcome = "no_effect"
			s.result.HandleID = ""
			s.result.RuntimeCleanup = "failed"
		},
		"no effect cleanup succeeded": func(s *cancelStore) {
			s.result.Outcome = "no_effect"
			s.result.HandleID = ""
			s.result.RuntimeCleanup = "succeeded"
		},
		"contradictory no effect": func(s *cancelStore) {
			s.result.Outcome = "no_effect"
			s.result.HandleID = ""
			s.result.RuntimeEffect = "possible"
		},
		"attempt":                 func(s *cancelStore) { s.receipt.AttemptID = "other" },
		"operation":               func(s *cancelStore) { s.receipt.OperationID = "other" },
		"owner":                   func(s *cancelStore) { s.receipt.Owner = "other" },
		"future generation":       func(s *cancelStore) { s.receipt.Generation++ },
		"capability":              func(s *cancelStore) { s.receipt.CapabilityFingerprint = strings.Repeat("b", 64) },
		"workspace":               func(s *cancelStore) { s.receipt.WorkspaceRoot = "/other" },
		"workspace identity":      func(s *cancelStore) { s.receipt.WorkspaceIdentityHash = strings.Repeat("b", 64) },
		"source identity":         func(s *cancelStore) { s.receipt.SourceManifestHash = strings.Repeat("b", 64) },
		"session":                 func(s *cancelStore) { s.result.SessionID = "other" },
		"result attempt":          func(s *cancelStore) { s.result.AttemptID = "other" },
		"result predates receipt": func(s *cancelStore) { s.result.CreatedAt = s.receipt.CreatedAt.Add(-time.Second) },
		"live receipt":            func(s *cancelStore) { s.receipt.EvidenceMode = "live" },
	} {
		t.Run(name, func(t *testing.T) {
			svc, runtime, store, port, _ := cancelFixture()
			change(store)
			// Rehashing a corrupt receipt must not bypass relationship checks.
			store.result.ReceiptHash = domain.SwarmHash(store.receipt)
			if err := svc.Cancel(context.Background(), store.attempt.ID); err == nil || port.stops != 0 || store.finishes != 0 {
				t.Fatalf("err=%v stops=%d finishes=%d", err, port.stops, store.finishes)
			}
			if observed, _ := runtime.Observe(context.Background(), store.attempt); observed.Terminal || observed.Exact {
				t.Fatalf("uncertain execution became exact during reconciliation: %+v", observed)
			}
		})
	}
}

func TestExactCancelAcknowledgementRequiresIndependentObservation(t *testing.T) {
	stopFailure := errors.New("lost or failed stop")
	for _, tc := range []struct {
		name                  string
		change                func(*ports.FencedStopResult)
		probe                 ports.FencedProbeResult
		stopErr               error
		wantErr               bool
		wantFinish, wantProbe int
	}{
		{"terminal", nil, ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}, nil, false, 1, 1},
		{"PTY success descendants unknown", nil, ports.FencedProbeResult{Liveness: ports.FencedUnknown, Reason: ports.FencedReasonIdentityMissing}, nil, true, 0, 1},
		{"still alive", nil, ports.FencedProbeResult{Liveness: ports.FencedAlive, Reason: ports.FencedReasonExactMatch}, nil, false, 0, 1},
		{"mismatched session", func(r *ports.FencedStopResult) { r.SessionID = "other" }, ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}, nil, true, 0, 1},
		{"mismatched generation", func(r *ports.FencedStopResult) { r.Generation = "other" }, ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}, nil, true, 0, 1},
		{"cleanup failed", func(r *ports.FencedStopResult) { r.Cleanup = ports.RuntimeCleanupFailed }, ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}, nil, true, 0, 1},
		{"effect uncertain", func(r *ports.FencedStopResult) { r.Effect = ports.RuntimeEffectPossible }, ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}, nil, true, 0, 1},
		{"ack error", nil, ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}, stopFailure, true, 0, 1},
		{"unacknowledged", func(r *ports.FencedStopResult) { r.Acknowledged = false }, ports.FencedProbeResult{Liveness: ports.FencedDead, Reason: ports.FencedReasonExactAbsent}, nil, true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, store, port, _ := cancelFixture()
			port.probe = tc.probe
			port.stop = func(_ context.Context, ref ports.FencedRuntimeRef) (ports.FencedStopResult, error) {
				out := ports.FencedStopResult{SessionID: ref.SessionID, Generation: ref.Generation, Acknowledged: true, Effect: ports.RuntimeEffectApplied, Cleanup: ports.RuntimeCleanupSucceeded}
				if tc.change != nil {
					tc.change(&out)
				}
				return out, tc.stopErr
			}
			err := svc.Cancel(context.Background(), store.attempt.ID)
			if (err != nil) != tc.wantErr || store.finishes != tc.wantFinish || port.probes != tc.wantProbe || port.stops != 1 {
				t.Fatalf("err=%v finishes=%d probes=%d stops=%d", err, store.finishes, port.probes, port.stops)
			}
			if tc.stopErr != nil && !errors.Is(err, tc.stopErr) {
				t.Fatal("stop error lost")
			}
		})
	}
}

func TestExactCancelNoEffectAndTakeoverKeepImmutableLaunchIdentity(t *testing.T) {
	t.Run("final no effect", func(t *testing.T) {
		svc, _, store, port, _ := cancelFixture()
		store.result.Outcome, store.result.HandleID, store.result.RuntimeEffect = "no_effect", "", "none"
		if err := svc.Cancel(context.Background(), store.attempt.ID); err != nil || store.finishes != 1 || port.stops != 0 || port.probes != 0 {
			t.Fatalf("err=%v finishes=%d stops=%d probes=%d", err, store.finishes, port.stops, port.probes)
		}
	})
	t.Run("takeover", func(t *testing.T) {
		svc, _, store, port, _ := cancelFixture()
		store.attempt.Owner, store.attempt.Generation = "new-owner", 2
		svc.Owner = "new-owner"
		if err := svc.Cancel(context.Background(), store.attempt.ID); err != nil {
			t.Fatal(err)
		}
		if port.ref.Generation != domain.SwarmHash(store.attempt.OperationID) || port.ref.Handle.ID != store.result.HandleID || store.finishes != 1 {
			t.Fatalf("wrong launch fenced: %+v", port.ref)
		}
	})
}

type executionWithoutAuthority struct {
	sessionmanager.SwarmExecutionStore
}

func TestExactCancelRequiresExplicitStopAndStoreCapabilities(t *testing.T) {
	t.Run("store has only execution receipts", func(t *testing.T) {
		_, runtime, store, port, _ := cancelFixture()
		runtime.Launcher.Store = executionWithoutAuthority{store}
		if _, err := runtime.Stop(context.Background(), store.attempt); !errors.Is(err, qualification.ErrUnverified) || port.stops != 0 {
			t.Fatal(err)
		}
	})
	t.Run("runtime has only probe", func(t *testing.T) {
		_, runtime, store, _, _ := cancelFixture()
		runtime.Launcher.Runtime = &exactRuntimePort{}
		if _, err := runtime.Stop(context.Background(), store.attempt); !errors.Is(err, qualification.ErrUnverified) {
			t.Fatal(err)
		}
	})
	t.Run("result hash mismatch", func(t *testing.T) {
		_, runtime, store, port, _ := cancelFixture()
		store.result.ReceiptHash = strings.Repeat("b", 64)
		if _, err := runtime.Stop(context.Background(), store.attempt); !errors.Is(err, domain.ErrSwarmConflict) || port.stops != 0 {
			t.Fatal(err)
		}
	})
}

type cancelModeRuntime struct {
	Runtime
	mode qualification.EvidenceMode
}

func (r cancelModeRuntime) Mode() qualification.EvidenceMode { return r.mode }

func TestSchedulerCancelFailsClosedAndHonorsOperationExclusion(t *testing.T) {
	t.Run("store returned another attempt", func(t *testing.T) {
		svc, _, _, port, _ := cancelFixture()
		if err := svc.Cancel(context.Background(), "other-attempt"); !errors.Is(err, domain.ErrSwarmConflict) || port.stops != 0 {
			t.Fatal(err)
		}
	})
	t.Run("foreign owner", func(t *testing.T) {
		svc, _, store, port, _ := cancelFixture()
		svc.Owner = "other"
		if err := svc.Cancel(context.Background(), store.attempt.ID); !errors.Is(err, domain.ErrSwarmStale) || port.stops != 0 {
			t.Fatal(err)
		}
	})
	for _, mode := range []qualification.EvidenceMode{qualification.LiveEvidence, qualification.TestEvidence, "unknown"} {
		t.Run("unsupported "+string(mode), func(t *testing.T) {
			svc, runtime, store, port, _ := cancelFixture()
			svc.Runtime = cancelModeRuntime{Runtime: runtime, mode: mode} // deliberately lacks Stop
			if err := svc.Cancel(context.Background(), store.attempt.ID); !errors.Is(err, qualification.ErrUnverified) || port.stops != 0 {
				t.Fatal(err)
			}
		})
	}
	t.Run("operation in progress", func(t *testing.T) {
		svc, _, store, port, _ := cancelFixture()
		if !svc.beginOperation(store.attempt.ID) {
			t.Fatal("fixture operation unavailable")
		}
		defer svc.endOperation(store.attempt.ID)
		if err := svc.Cancel(context.Background(), store.attempt.ID); !errors.Is(err, domain.ErrSwarmNotReady) || port.stops != 0 {
			t.Fatal(err)
		}
	})
	t.Run("cancellation excludes reconciliation and another cancel", func(t *testing.T) {
		svc, _, store, port, _ := cancelFixture()
		entered, release := make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		defer unblock()
		port.stop = func(_ context.Context, ref ports.FencedRuntimeRef) (ports.FencedStopResult, error) {
			close(entered)
			<-release
			return ports.FencedStopResult{SessionID: ref.SessionID, Generation: ref.Generation, Acknowledged: true, Effect: ports.RuntimeEffectApplied, Cleanup: ports.RuntimeCleanupSucceeded}, nil
		}
		done := make(chan error, 1)
		go func() { done <- svc.Cancel(context.Background(), store.attempt.ID) }()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("stop not entered")
		}
		if err := svc.Cancel(context.Background(), store.attempt.ID); !errors.Is(err, domain.ErrSwarmNotReady) {
			t.Fatal(err)
		}
		if err := svc.Reconcile(context.Background(), store.attempt.ID); !errors.Is(err, domain.ErrSwarmNotReady) {
			t.Fatal(err)
		}
		unblock()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if port.stops != 1 || port.probes != 1 || store.finishes != 1 {
			t.Fatal("concurrent operation entered runtime")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		svc, _, store, port, _ := cancelFixture()
		svc.OperationTimeout = time.Millisecond
		port.stop = func(ctx context.Context, _ ports.FencedRuntimeRef) (ports.FencedStopResult, error) {
			<-ctx.Done()
			return ports.FencedStopResult{}, ctx.Err()
		}
		if err := svc.Cancel(context.Background(), store.attempt.ID); !errors.Is(err, context.DeadlineExceeded) || store.finishes != 0 {
			t.Fatal(err)
		}
	})
}

type cancelExactPort struct {
	*exactRuntimePort
	unknownAfterStop bool
	stops            int
	onStop           func()
}

func (p *cancelExactPort) StopFencedRuntime(_ context.Context, ref ports.FencedRuntimeRef) (ports.FencedStopResult, error) {
	p.stops++
	if p.onStop != nil {
		p.onStop()
	}
	p.mu.Lock()
	p.dead, p.unknown = true, p.unknownAfterStop
	p.mu.Unlock()
	return ports.FencedStopResult{SessionID: ref.SessionID, Generation: ref.Generation, Acknowledged: true, Effect: ports.RuntimeEffectApplied, Cleanup: ports.RuntimeCleanupSucceeded}, nil
}

func TestSchedulerCancelSQLiteRetainsUnknownThenReconcilesExactAbsence(t *testing.T) {
	f, runtime, original := exactFixture(t)
	port := &cancelExactPort{exactRuntimePort: original, unknownAfterStop: true}
	runtime.Launcher.Runtime = port
	ctx := context.Background()
	results, err := f.svc.Tick(ctx, f.candidates[:1])
	if err != nil || len(results) != 1 || results[0].Err != nil {
		t.Fatalf("dispatch=%+v %v", results, err)
	}
	id := results[0].Attempt.ID
	if err = f.svc.Cancel(ctx, id); !errors.Is(err, domain.ErrSwarmNotReady) || port.stops != 0 {
		t.Fatalf("noncancelled run: %v", err)
	}
	if err = f.store.CancelSwarmRun(ctx, "run"); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.Cancel(ctx, id); !errors.Is(err, qualification.ErrUnverified) {
		t.Fatal(err)
	}
	a, err := f.store.GetSwarmAttempt(ctx, id)
	if err != nil || a.State != "running" {
		t.Fatalf("uncertainty released attempt: %+v %v", a, err)
	}
	port.mu.Lock()
	port.unknown = false
	port.mu.Unlock()
	if err = f.svc.Reconcile(ctx, id); err != nil {
		t.Fatal(err)
	}
	a, err = f.store.GetSwarmAttempt(ctx, id)
	if err != nil || a.State != "cancelled" || len(port.configs) != 1 || port.stops != 1 {
		t.Fatalf("terminal reconciliation: %+v %v", a, err)
	}
	budget, err := f.store.GetSwarmBudget(ctx, "budget")
	if err != nil || budget.Reserved != 100 {
		t.Fatalf("unknown usage was lost: %+v %v", budget, err)
	}
}

func TestSchedulerCancelSQLiteLateNoEffectNeverRelaunches(t *testing.T) {
	f, runtime, port := exactFixture(t)
	ctx := context.Background()
	c := f.candidates[0]
	if err := f.svc.bindCandidate(ctx, &c); err != nil {
		t.Fatal(err)
	}
	a, _, err := f.svc.Admission.Admit(ctx, c.Admission)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := f.svc.Composer.Compose(ctx, a.RunID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := f.svc.Preparer.Prepare(ctx, a, packet, c.Workspace)
	if !errors.Is(err, qualification.ErrUnverified) {
		t.Fatal(err)
	}
	a, won, err := f.store.BeginSwarmDispatch(ctx, fence(a), f.now)
	if err != nil || !won {
		t.Fatalf("dispatch: %t %v", won, err)
	}
	receipt, cfg, err := exactLaunch(Launch{a, packet, c.Workspace, preparation}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if won, err = f.store.AuthorizeSwarmExecution(ctx, fence(a), receipt, f.now); err != nil || !won {
		t.Fatalf("authorize: %t %v", won, err)
	}
	if err = f.store.CancelSwarmRun(ctx, a.RunID); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.Cancel(ctx, a.ID); !errors.Is(err, qualification.ErrUnverified) {
		t.Fatalf("pending result: %v", err)
	}
	current, err := f.store.GetSwarmAttempt(ctx, a.ID)
	if err != nil || current.State != "dispatching" {
		t.Fatalf("pending launch was released: %+v %v", current, err)
	}
	result := domain.SwarmExecutionResult{AttemptID: a.ID, ReceiptHash: domain.SwarmHash(receipt), SessionID: receipt.SessionID, Outcome: "no_effect", CreatedAt: f.now}
	if err = f.store.RecordSwarmExecutionResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.Cancel(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	current, err = f.store.GetSwarmAttempt(ctx, a.ID)
	if err != nil || current.State != "cancelled" {
		t.Fatalf("no effect did not finish: %+v %v", current, err)
	}
	launcher := runtime.Launcher
	launcher.Validate = func(context.Context, domain.SwarmExecutionReceipt, ports.RuntimeConfig) error {
		t.Fatal("replay revalidated launch")
		return nil
	}
	replayed, err := launcher.Start(ctx, receipt, cfg)
	if err != nil || replayed.Outcome != "no_effect" || len(port.configs) != 0 {
		t.Fatalf("cancelled replay launched: %+v %v", replayed, err)
	}
}

func TestSchedulerCancelSQLiteTakeoverStopsOriginalLaunch(t *testing.T) {
	f, runtime, original := exactFixture(t)
	port := &cancelExactPort{exactRuntimePort: original}
	runtime.Launcher.Runtime = port
	ctx := context.Background()
	results, err := f.svc.Tick(ctx, f.candidates[:1])
	if err != nil || len(results) != 1 || results[0].Err != nil {
		t.Fatalf("dispatch: %+v %v", results, err)
	}
	a, err := f.store.GetSwarmAttempt(ctx, results[0].Attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.CancelSwarmRun(ctx, a.RunID); err != nil {
		t.Fatal(err)
	}
	now := f.now.Add(2 * time.Hour)
	runtime.Now, f.svc.Now = func() time.Time { return now }, func() time.Time { return now }
	if _, err = runtime.Stop(ctx, a); !errors.Is(err, domain.ErrSwarmStale) || port.stops != 0 {
		t.Fatalf("expired owner stopped: %v", err)
	}
	recovered, err := f.store.TakeOverSwarmLease(ctx, fence(a), "new-owner", now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.Stop(ctx, a); !errors.Is(err, domain.ErrSwarmStale) || port.stops != 0 {
		t.Fatalf("old epoch stopped: %v", err)
	}
	f.svc.Owner = recovered.Owner
	// A lease can change during external I/O. The immutable cancelled operation
	// remains a safe stop target, but the old epoch cannot finish the attempt.
	port.onStop = func() {
		now = now.Add(2 * time.Hour)
		_, takeoverErr := f.store.TakeOverSwarmLease(ctx, fence(recovered), "third-owner", now, now.Add(time.Hour))
		if takeoverErr != nil {
			t.Fatal(takeoverErr)
		}
	}
	if err = f.svc.Cancel(ctx, recovered.ID); !errors.Is(err, domain.ErrSwarmStale) {
		t.Fatalf("old epoch committed after stop: %v", err)
	}
	current, err := f.store.GetSwarmAttempt(ctx, a.ID)
	if err != nil || current.State != "running" || current.Generation != 3 || port.stops != 1 || len(port.configs) != 1 {
		t.Fatalf("takeover cancellation: %+v %v", current, err)
	}
	f.svc.Owner = current.Owner
	if err = f.svc.Reconcile(ctx, current.ID); err != nil {
		t.Fatal(err)
	}
	current, err = f.store.GetSwarmAttempt(ctx, a.ID)
	if err != nil || current.State != "cancelled" {
		t.Fatalf("current epoch failed to finish: %+v %v", current, err)
	}
	receipt, err := f.store.GetSwarmExecution(ctx, a.ID)
	if err != nil || receipt.Generation != 1 || receipt.Owner != a.Owner {
		t.Fatalf("immutable receipt changed: %+v %v", receipt, err)
	}
}
