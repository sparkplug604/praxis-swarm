package supervised

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
)

type memoryRunStore struct {
	mu       sync.Mutex
	runs     map[string][]byte
	requests map[string]string
}

func newMemoryRunStore() *memoryRunStore {
	return &memoryRunStore{runs: map[string][]byte{}, requests: map[string]string{}}
}

func (s *memoryRunStore) CreateSupervisedSwarmRun(ctx context.Context, id, request, hash string, raw []byte, _ time.Time) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if priorID, ok := s.requests[request]; ok {
		var prior Run
		if err := json.Unmarshal(s.runs[priorID], &prior); err != nil {
			return nil, false, err
		}
		if prior.RequestHash != hash || priorID != id {
			return nil, false, domain.ErrSwarmConflict
		}
		return append([]byte(nil), s.runs[priorID]...), false, nil
	}
	if _, ok := s.runs[id]; ok {
		return nil, false, domain.ErrSwarmConflict
	}
	s.runs[id] = append([]byte(nil), raw...)
	s.requests[request] = id
	return append([]byte(nil), raw...), true, nil
}

func (s *memoryRunStore) GetSupervisedSwarmRun(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.runs[id]
	if !ok {
		return nil, domain.ErrSwarmNotFound
	}
	return append([]byte(nil), raw...), nil
}

func (s *memoryRunStore) UpdateSupervisedSwarmRun(ctx context.Context, id string, raw []byte, _ time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[id]; !ok {
		return domain.ErrSwarmNotFound
	}
	s.runs[id] = append([]byte(nil), raw...)
	return nil
}

// This runtime only creates local Git worktrees and fixture files. No agent,
// model, provider, terminal host or shell acceptance command is executed.
type fakeAORuntime struct {
	mu             sync.Mutex
	source         string
	root           string
	launches       int
	stops          map[string]int
	requests       map[string]string
	paths          map[string]string
	stopped        map[string]bool
	unknownTasks   map[string]bool
	findErrors     map[string]error
	ackOnly        bool
	complete       bool
	workspaceCalls int
	launched       chan string
	overlap        bool
	sharedPath     string
	extraFile      string
	sourceChange   string
	changedSource  bool
}

func (r *fakeAORuntime) ProjectPath(context.Context, domain.ProjectID) (string, error) {
	return r.source, nil
}

func (r *fakeAORuntime) Launch(ctx context.Context, _ domain.ProjectID, request, branch string, packet planning.SupervisedPacket) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.launches++
	if r.unknownTasks[packet.TaskID] {
		return "", errors.New("synthetic ambiguous launch reply")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sid := "session-" + packet.TaskID
	path := filepath.Join(r.root, packet.TaskID)
	if _, err := fixtureGit(ctx, r.source, "worktree", "add", "-q", "-b", branch, path, "HEAD"); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(path, packet.TaskID+".txt"), []byte("candidate "+packet.TaskID+"\n"), 0600); err != nil {
		return "", err
	}
	if r.extraFile != "" {
		if err := os.WriteFile(filepath.Join(path, r.extraFile), []byte("out of scope\n"), 0644); err != nil {
			return "", err
		}
	}
	if r.sourceChange != "" && !r.changedSource {
		var err error
		if r.sourceChange == "mode" {
			err = os.Chmod(filepath.Join(r.source, "PROGRESS.md"), 0644)
		} else {
			err = os.WriteFile(filepath.Join(r.source, "PROGRESS.md"), []byte("source changed\n"), 0644)
		}
		if err != nil {
			return "", err
		}
		r.changedSource = true
	}
	if r.overlap && r.sharedPath == "" {
		r.sharedPath = path
	}
	r.requests[request], r.paths[sid] = sid, path
	select {
	case r.launched <- sid:
	default:
	}
	return sid, nil
}

func (r *fakeAORuntime) Find(_ context.Context, request string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.findErrors[request]; err != nil {
		return "", err
	}
	return r.requests[request], nil
}

func (r *fakeAORuntime) Observe(_ context.Context, sid string) (Observation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.paths[sid] == "" {
		return Observation{}, errors.New("unknown fixture session")
	}
	now := time.Now().UTC()
	started := now.Add(-time.Second)
	obs := Observation{Controller: "busy", TurnState: "running", StartedAt: &started, ProviderTurnID: "fixture-turn-" + sid, Model: "fixture-model", Effort: "low", ModelEvidence: "test_fake", ObservedAt: now}
	if r.complete {
		obs.Controller = "ready"
		obs.TurnState = "completed"
		obs.CompletedAt = &now
	}
	if r.stopped[sid] && !r.ackOnly {
		obs.Controller = "stopped"
	}
	return obs, nil
}

func (r *fakeAORuntime) Stop(_ context.Context, sid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops[sid]++
	r.stopped[sid] = true
	return nil
}

func (r *fakeAORuntime) Workspace(_ context.Context, sid string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaceCalls++
	path := r.paths[sid]
	if r.overlap {
		path = r.sharedPath
	}
	if path == "" {
		return "", errors.New("missing fixture workspace")
	}
	return path, nil
}

func (r *fakeAORuntime) counts() (int, map[string]int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stops := map[string]int{}
	for id, n := range r.stops {
		stops[id] = n
	}
	return r.launches, stops, r.workspaceCalls
}

func fixtureGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=0")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, raw)
	}
	return strings.TrimSpace(string(raw)), nil
}

func fixtureDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

type serviceFixture struct {
	service *Service
	store   *memoryRunStore
	runtime *fakeAORuntime
	input   StartInput
	root    string
	source  string
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err = os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := fixtureGit(context.Background(), source, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	git("init", "-q")
	for _, name := range []string{"a.txt", "b.txt", "PROGRESS.md"} {
		// Git worktrees normalize these to0644. Ordinary AO copying must not
		// mistake non-executable permission normalization for a worker edit.
		if err = os.WriteFile(filepath.Join(source, name), []byte("original\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture")
	head := git("rev-parse", "HEAD")
	toolPath := filepath.Join(root, "fixture-tool")
	toolBody := "fixture bytes, never executed"
	if err = os.WriteFile(toolPath, []byte(toolBody), 0700); err != nil {
		t.Fatal(err)
	}
	contexts := []planning.SuppliedContext{{ID: "spec", Body: "Change the assigned file only."}, {ID: "source", Body: "Committed a.txt and b.txt fixture."}}
	plan := domain.SwarmPlan{ProtocolVersion: 1, Version: 1, ProjectID: "fixture", SourceCommit: head, Objective: "Two independent fixture edits", BudgetBucket: "advisory", TokenLimit: 10000, MaxActive: 2}
	for _, id := range []string{"a", "b"} {
		plan.Tasks = append(plan.Tasks, domain.SwarmTaskContract{ID: id, Version: 1, Objective: "Edit " + id + ".txt", OracleHash: fixtureDigest("fixture oracle"), Agent: domain.SwarmSelection{Harness: domain.HarnessCodex, Model: "fixture-model", Effort: "low"}, Resources: []domain.SwarmResource{{Namespace: "repository", Kind: "path", Key: id + ".txt"}}, ReserveTokens: 100,
			Work: &domain.SwarmWorkSpec{Version: 1, TaskType: "implementation", ContextByteLimit: 16384, NonGoals: []string{"Do not change other files"},
				Interfaces: []domain.SwarmInterface{{ID: "write", Contract: "write the assigned file", Ownership: id + ".txt", ErrorRuleIDs: []string{"invalid"}}},
				Errors:     []domain.SwarmErrorRule{{ID: "invalid", When: "input is invalid", Signal: "throw", Value: "ValueError", Destination: "unchanged", Effect: "reject before mutation"}},
				Acceptance: domain.SwarmAcceptance{OracleID: "fixture-oracle", Checks: []domain.SwarmAcceptanceCheck{{ID: "check", Command: []string{toolPath}, ExpectedExitCode: 0, ExpectedResult: "invalid input rejected before mutation", ErrorRuleID: "invalid"}}},
				Context:    []domain.SwarmContextRef{{ID: "spec", Kind: "spec", SHA256: fixtureDigest(contexts[0].Body), Required: true}, {ID: "source", Kind: "source_manifest", SHA256: fixtureDigest(contexts[1].Body), Required: true}},
				Tools:      []domain.SwarmToolIdentity{{Name: "fixture-tool", Path: toolPath, SHA256: fixtureDigest(toolBody), Version: "fixture-v1"}}, ProgressPath: "PROGRESS.md", OnComplete: "submit_and_stop", OnBlocked: "report_and_stop"}})
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryRunStore()
	runtime := &fakeAORuntime{source: source, root: filepath.Join(root, "worktrees"), stops: map[string]int{}, requests: map[string]string{}, paths: map[string]string{}, stopped: map[string]bool{}, unknownTasks: map[string]bool{}, findErrors: map[string]error{}, complete: true, launched: make(chan string, 2)}
	if err = os.Mkdir(runtime.root, 0700); err != nil {
		t.Fatal(err)
	}
	svc := New(context.Background(), store, runtime, filepath.Join(root, "runs"))
	svc.poll = 5 * time.Millisecond
	t.Cleanup(svc.Close)
	return &serviceFixture{service: svc, store: store, runtime: runtime, input: StartInput{RequestID: "fixture-request", Plan: raw, Context: contexts, TimeoutSeconds: 10}, root: filepath.Join(root, "runs"), source: source}
}

func awaitRun(t *testing.T, s *Service, id string) Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := s.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		entry := s.active[id]
		s.mu.Unlock()
		if entry == nil {
			run, err = s.Get(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			return run
		}
		select {
		case <-entry.done:
			run, err = s.Get(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			return run
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatal("fixture coordinator did not finish")
	return Run{}
}

func runSummary(run Run) string {
	tasks := make([]string, 0, len(run.Tasks))
	for _, task := range run.Tasks {
		tasks = append(tasks, task.ID+":"+task.Status+":"+task.Error)
	}
	return fmt.Sprintf("status=%s sourceUnchanged=%t tasks=%v errors=%v", run.Status, run.SourceUnchanged, tasks, run.Errors)
}

func TestSupervisedServiceConcurrentStartAndHashConflict(t *testing.T) {
	f := newServiceFixture(t)
	type result struct {
		run Run
		err error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			run, err := f.service.Start(context.Background(), f.input)
			if err == nil {
				_, err = json.Marshal(run)
			}
			results <- result{run, err}
		}()
	}
	id := ""
	for range 8 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if id != "" && id != got.run.ID {
			t.Fatal("replay returned another run")
		}
		id = got.run.ID
	}
	run := awaitRun(t, f.service, id)
	launches, stops, _ := f.runtime.counts()
	if launches != 2 || len(stops) != 2 {
		t.Fatalf("launches%d stops%v", launches, stops)
	}
	if run.Status != "completed" {
		t.Fatalf("run did not complete: %s; clean=%v source=%v", runSummary(run), cleanSource(context.Background(), run.SourcePath, run.Plan.SourceCommit), sourceMatches(run.SourceSnapshot, run.SourcePath))
	}
	changed := f.input
	changed.TimeoutSeconds++
	if _, err := f.service.Start(context.Background(), changed); !errors.Is(err, domain.ErrSwarmConflict) {
		t.Fatalf("request hash reuse: %v", err)
	}
	restarted := New(context.Background(), f.store, f.runtime, f.root)
	defer restarted.Close()
	if _, err := restarted.Start(context.Background(), f.input); err != nil {
		t.Fatal(err)
	}
	if launches, _, _ := f.runtime.counts(); launches != 2 {
		t.Fatalf("completed replay relaunched %d", launches)
	}
}

func TestSupervisedServiceUnknownLaunchNeverRetriesAfterRestart(t *testing.T) {
	f := newServiceFixture(t)
	f.runtime.unknownTasks = map[string]bool{"a": true, "b": true}
	run, err := f.service.Start(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, f.service, run.ID)
	if run.Status != "needs_attention" {
		t.Fatalf("unknown launch became %s", run.Status)
	}
	for _, task := range run.Tasks {
		if task.Status != "launch_unknown" || task.SessionID != "" || task.SnapshotHash != "" {
			t.Fatalf("unknown launch invented identity/evidence: %+v", task)
		}
	}
	// Simulate a crash before ambiguous launch results were persisted. A stored
	// starting intent is still not authority for a replacement launch.
	run.Status = "running"
	for i := range run.Tasks {
		run.Tasks[i].Status = "starting"
	}
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.UpdateSupervisedSwarmRun(context.Background(), run.ID, raw, time.Now()); err != nil {
		t.Fatal(err)
	}
	restarted := New(context.Background(), f.store, f.runtime, f.root)
	defer restarted.Close()
	replayed, err := restarted.Start(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Status != "needs_attention" {
		t.Fatalf("recovered starting intent became %s", replayed.Status)
	}
	if _, err = restarted.Stop(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	launches, stops, _ := f.runtime.counts()
	if launches != 2 || len(stops) != 0 {
		t.Fatalf("unknown request retried or guessed identity: %d %v", launches, stops)
	}
}

func TestSupervisedServiceStopAcknowledgementCannotFreezeCandidate(t *testing.T) {
	f := newServiceFixture(t)
	f.runtime.ackOnly = true
	run, err := f.service.Start(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, f.service, run.ID)
	if run.Status != "needs_attention" {
		t.Fatalf("unconfirmed stop became %s", run.Status)
	}
	for _, task := range run.Tasks {
		if task.Status != "stop_unknown" || task.SessionID == "" || task.SnapshotHash != "" || task.CandidateHash != "" {
			t.Fatalf("stop acknowledgement released evidence: %+v", task)
		}
	}
	_, stops, workspaceCalls := f.runtime.counts()
	if len(stops) != 2 || workspaceCalls != 0 {
		t.Fatalf("unexpected stop/freeze calls: %v %d", stops, workspaceCalls)
	}
}

func TestSupervisedServiceCancellationStopsKnownSessions(t *testing.T) {
	for _, mode := range []string{"explicit", "parent-deadline"} {
		t.Run(mode, func(t *testing.T) {
			f := newServiceFixture(t)
			f.runtime.complete = false
			if mode == "parent-deadline" {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				f.service.ctx = ctx
			}
			run, err := f.service.Start(context.Background(), f.input)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				select {
				case <-f.runtime.launched:
				case <-time.After(5 * time.Second):
					t.Fatal("fixture sessions did not launch")
				}
			}
			if mode == "explicit" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				run, err = f.service.Stop(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				run = awaitRun(t, f.service, run.ID)
			}
			_, stops, _ := f.runtime.counts()
			if stops["session-a"] != 1 || stops["session-b"] != 1 {
				t.Fatalf("known sessions not stopped: %v", stops)
			}
			if run.Status != "stopped" {
				t.Fatalf("cancelled run: %s", runSummary(run))
			}
		})
	}
}

func TestSupervisedServicePreservesSourceAndFreezesCandidates(t *testing.T) {
	f := newServiceFixture(t)
	run, err := f.service.Start(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, f.service, run.ID)
	if !run.SourceUnchanged || run.Status != "completed" {
		t.Fatalf("source/result failed: %s; clean=%v source=%v", runSummary(run), cleanSource(context.Background(), run.SourcePath, run.Plan.SourceCommit), sourceMatches(run.SourceSnapshot, run.SourcePath))
	}
	seen := map[string]bool{}
	for _, task := range run.Tasks {
		if !domain.SwarmValidHash(task.CandidateHash, false) || !domain.SwarmValidHash(task.SnapshotHash, false) || task.SnapshotPath == "" || task.DiffPath == "" {
			t.Fatalf("missing frozen evidence: %+v", task)
		}
		if task.WorkspacePath == f.source || seen[task.WorkspacePath] {
			t.Fatal("workers share a workspace")
		}
		seen[task.WorkspacePath] = true
		for _, path := range []string{task.SnapshotPath, task.DiffPath} {
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
		}
		snapshot, err := loadSnapshot(task.SnapshotPath)
		if err != nil || snapshot.ManifestHash != task.SnapshotHash || domain.SwarmHash(snapshot.Source) != task.SnapshotHash || domain.SwarmHash(content(snapshot.Source.Entries)) != task.CandidateHash {
			t.Fatalf("frozen hash binding failed: %v", err)
		}
		frozenFile := filepath.Join(snapshot.Destination.Path, task.ID+".txt")
		frozen, err := os.ReadFile(frozenFile)
		if err != nil || string(frozen) != "candidate "+task.ID+"\n" {
			t.Fatalf("frozen content mismatch: %q %v", frozen, err)
		}
		raw, err := os.ReadFile(filepath.Join(f.source, task.ID+".txt"))
		if err != nil || string(raw) != "original\n" {
			t.Fatalf("source changed: %q %v", raw, err)
		}
		raw, err = os.ReadFile(filepath.Join(task.WorkspacePath, task.ID+".txt"))
		if err != nil || string(raw) != "candidate "+task.ID+"\n" {
			t.Fatalf("candidate deleted: %q %v", raw, err)
		}
		if err := os.WriteFile(filepath.Join(task.WorkspacePath, task.ID+".txt"), []byte("later worktree edit"), 0644); err != nil {
			t.Fatal(err)
		}
		frozen, err = os.ReadFile(frozenFile)
		if err != nil || string(frozen) != "candidate "+task.ID+"\n" {
			t.Fatalf("worktree edits changed frozen candidate: %q %v", frozen, err)
		}
	}
}

func TestSupervisedServiceRejectsOverlappingWorkspaces(t *testing.T) {
	f := newServiceFixture(t)
	f.runtime.overlap = true
	run, err := f.service.Start(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, f.service, run.ID)
	if run.Status != "needs_attention" {
		t.Fatalf("overlapping workspaces accepted: %s", runSummary(run))
	}
	found := false
	claims := map[string]bool{}
	for _, task := range run.Tasks {
		found = found || strings.Contains(task.Error, "overlap")
		if task.WorkspacePath != "" {
			if claims[task.WorkspacePath] {
				t.Fatal("two tasks durably claimed the same workspace")
			}
			claims[task.WorkspacePath] = true
		}
	}
	if !found {
		t.Fatalf("missing overlap refusal: %s", runSummary(run))
	}
}

func TestSupervisedServiceRejectsScopeEscapeAndSourceMutation(t *testing.T) {
	for _, mode := range []string{"scope", "content", "mode"} {
		t.Run(mode, func(t *testing.T) {
			f := newServiceFixture(t)
			if mode == "scope" {
				f.runtime.extraFile = "PROGRESS.md"
			} else {
				f.runtime.sourceChange = mode
			}
			run, err := f.service.Start(context.Background(), f.input)
			if err != nil {
				t.Fatal(err)
			}
			run = awaitRun(t, f.service, run.ID)
			if run.Status != "needs_attention" {
				t.Fatalf("escape/mutation accepted: %s", runSummary(run))
			}
			if mode == "scope" {
				for _, task := range run.Tasks {
					if task.Status != "needs_attention" || !strings.Contains(task.Error, "outside task scope") {
						t.Fatalf("scope failure was not exposed: %s", runSummary(run))
					}
				}
			} else if run.SourceUnchanged {
				t.Fatalf("source mutation reported preserved: %s", runSummary(run))
			}
		})
	}
}

type pausedRunRead struct {
	*memoryRunStore
	enabled atomic.Bool
	once    sync.Once
	loaded  chan struct{}
	release chan struct{}
}

func (s *pausedRunRead) GetSupervisedSwarmRun(ctx context.Context, id string) ([]byte, error) {
	raw, err := s.memoryRunStore.GetSupervisedSwarmRun(ctx, id)
	if err == nil && s.enabled.Load() {
		s.once.Do(func() {
			close(s.loaded)
			<-s.release
		})
	}
	return raw, err
}

func TestSupervisedServiceFinishingDoesNotInventRecoveryFailure(t *testing.T) {
	f := newServiceFixture(t)
	f.runtime.complete = false
	paused := &pausedRunRead{memoryRunStore: f.store, loaded: make(chan struct{}), release: make(chan struct{})}
	f.service.store = paused
	run, err := f.service.Start(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-f.runtime.launched:
		case <-time.After(5 * time.Second):
			t.Fatal("fixture sessions did not launch")
		}
	}
	paused.enabled.Store(true)
	result := make(chan Run, 1)
	getErr := make(chan error, 1)
	go func() {
		observed, err := f.service.Get(context.Background(), run.ID)
		result <- observed
		getErr <- err
	}()
	<-paused.loaded
	var release sync.Once
	unblock := func() { release.Do(func() { close(paused.release) }) }
	defer unblock()
	f.runtime.mu.Lock()
	f.runtime.complete = true
	f.runtime.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	finished := false
	for time.Now().Before(deadline) {
		raw, err := f.store.GetSupervisedSwarmRun(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		var current Run
		if err := json.Unmarshal(raw, &current); err != nil {
			t.Fatal(err)
		}
		if current.Status == "completed" {
			finished = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !finished {
		t.Fatal("completion did not persist while an earlier read was suspended")
	}
	// Give the finalizer a chance to retire the active controller. The paused
	// read still legitimately describes the earlier running state, not a crash.
	time.Sleep(20 * time.Millisecond)
	unblock()
	observed := <-result
	if err := <-getErr; err != nil {
		t.Fatal(err)
	}
	if observed.Status == "needs_attention" {
		t.Fatalf("normal completion invented recovery failure: %s", runSummary(observed))
	}
	if final := awaitRun(t, f.service, run.ID); final.Status != "completed" || !final.SourceUnchanged {
		t.Fatalf("final state lost: %s", runSummary(final))
	}
}

func TestSupervisedServiceRecoveryFindFailureDoesNotSkipKnownSession(t *testing.T) {
	f := newServiceFixture(t)
	f.runtime.unknownTasks["a"] = true
	f.runtime.ackOnly = true
	run, err := f.service.Start(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, f.service, run.ID)
	f.runtime.mu.Lock()
	f.runtime.ackOnly = false
	for _, task := range run.Tasks {
		if task.ID == "a" {
			f.runtime.findErrors[task.RequestID] = errors.New("synthetic lookup unavailable")
		}
	}
	f.runtime.mu.Unlock()
	restarted := New(context.Background(), f.store, f.runtime, f.root)
	defer restarted.Close()
	if _, err = restarted.Stop(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	_, stops, _ := f.runtime.counts()
	if stops["session-b"] != 2 {
		t.Fatalf("earlier lookup failure skipped known session stop: %v", stops)
	}
}
