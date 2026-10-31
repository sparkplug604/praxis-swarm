package supervised

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
)

type execution struct {
	mu     sync.Mutex
	run    Run
	cancel context.CancelFunc
	done   chan struct{}
}

// Service coordinates supervised worker sessions and retains their candidate evidence.
type Service struct {
	ctx     context.Context
	store   Store
	runtime Runtime
	root    string
	mu      sync.Mutex
	active  map[string]*execution
	poll    time.Duration
	tools   planning.ToolVerifier
}

// New creates a coordinator whose execution lifetime follows ctx.
func New(ctx context.Context, store Store, runtime Runtime, root string) *Service {
	return &Service{ctx: ctx, store: store, runtime: runtime, root: root, active: make(map[string]*execution), poll: 2 * time.Second, tools: planning.FileToolVerifier{}}
}

// Start validates and records a plan before launching its two isolated workers.
func (s *Service) Start(ctx context.Context, in StartInput) (Run, error) {
	if !domain.SwarmValidID(in.RequestID) || in.TimeoutSeconds < 10 || in.TimeoutSeconds > 600 {
		return Run{}, domain.ErrSwarmInvalid
	}
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > 4*1024*1024 {
		return Run{}, domain.ErrSwarmInvalid
	}
	plan, err := planning.ParsePlan(in.Plan)
	if err != nil {
		return Run{}, err
	}
	if err := planning.ValidateSupervisedPlan(plan); err != nil {
		return Run{}, err
	}
	id := "sao-" + domain.SwarmHash(in.RequestID)[:24]
	hash := domain.SwarmHash(in)
	// This mutex protects initial creation only; it never serializes worker turns.
	s.mu.Lock()
	defer s.mu.Unlock()
	prior, err := s.store.GetSupervisedSwarmRun(ctx, id)
	if err == nil {
		var run Run
		if json.Unmarshal(prior, &run) != nil {
			return Run{}, domain.ErrSwarmInvalid
		}
		if run.RequestHash != hash {
			return Run{}, domain.ErrSwarmConflict
		}
		return s.current(run), nil
	}
	if !errors.Is(err, domain.ErrSwarmNotFound) {
		return Run{}, err
	}
	if _, err := s.checkBrief(ctx, plan.ProjectID, plan.Brief); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	run := Run{ID: id, Profile: Profile, RequestID: in.RequestID, RequestHash: hash, Plan: plan, Status: "running", CreatedAt: now, DeadlineAt: now.Add(time.Duration(in.TimeoutSeconds) * time.Second), Tasks: []Task{}, Errors: []string{}, Limits: []string{
		"Supervised AO worktrees; no exact-copy runtime qualification or adversarial containment claim.",
		"Plan token limits and reservations are advisory; provider usage is observed, not a hard spending cap.",
		"Packet byte bounds exclude inherited AO/native instructions; reported settings are not independent model telemetry.",
		"Provider completion and frozen files are not independent acceptance, integration or measured savings.",
	}}
	for _, task := range plan.Tasks {
		packet, e := planning.ComposeSupervised(ctx, id, plan, task.ID, in.Context, s.tools)
		if e != nil {
			return Run{}, e
		}
		run.Tasks = append(run.Tasks, Task{ID: task.ID, RequestID: id + "-" + domain.SwarmHash(task.ID)[:12], Branch: "swarm/" + id + "/" + domain.SwarmHash(task.ID)[:12], Packet: packet, Status: "intent"})
	}
	source, err := s.runtime.ProjectPath(ctx, plan.ProjectID)
	if err != nil {
		return Run{}, err
	}
	if err := cleanSource(ctx, source, plan.SourceCommit); err != nil {
		return Run{}, err
	}
	dir := filepath.Join(s.root, id)
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return Run{}, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Run{}, fmt.Errorf("%w: preserved preparation directory exists or is unavailable", domain.ErrSwarmConflict)
	}
	baseline, err := workspace.Capture(ctx, workspace.CaptureRequest{SourceRoot: source, Destination: filepath.Join(dir, "source")})
	if err != nil {
		return Run{}, err
	}
	run.SourcePath = baseline.Source.Root.Path
	run.SourceSnapshot = filepath.Join(dir, "source-snapshot.json")
	if err := saveJSON(run.SourceSnapshot, baseline); err != nil {
		return Run{}, err
	}
	if err := workspace.ValidateForLaunch(ctx, baseline); err != nil {
		return Run{}, err
	}
	// Persist both immutable launch intents before either external session start.
	data, err := json.Marshal(run)
	if err != nil {
		return Run{}, err
	}
	stored, created, err := s.store.CreateSupervisedSwarmRun(ctx, id, in.RequestID, hash, data, now)
	if err != nil {
		return Run{}, err
	}
	if !created {
		if err := json.Unmarshal(stored, &run); err != nil {
			return Run{}, err
		}
		return s.current(run), nil
	}
	runctx, cancel := context.WithDeadline(s.ctx, run.DeadlineAt)
	entry := &execution{run: run, cancel: cancel, done: make(chan struct{})}
	s.active[id] = entry
	var response Run
	if err := json.Unmarshal(data, &response); err != nil {
		cancel()
		delete(s.active, id)
		return Run{}, err
	}
	go s.execute(runctx, entry)
	return response, nil
}

// current is called with s.mu held. Restart never dispatches a stored intent.
func (s *Service) current(run Run) Run {
	if run.Status == "running" && s.active[run.ID] == nil {
		run.Status = "needs_attention"
		run.Errors = append(run.Errors, "Controller is not active; use explicit stop to reconcile recorded requests. No relaunch occurred.")
	}
	return run
}

// Get returns persisted evidence and flags runs no longer owned by this controller.
func (s *Service) Get(ctx context.Context, id string) (Run, error) {
	if !domain.SwarmValidID(id) {
		return Run{}, domain.ErrSwarmInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := s.store.GetSupervisedSwarmRun(ctx, id)
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return Run{}, err
	}
	return s.current(run), nil
}
func (s *Service) persist(entry *execution, edit func(*Run)) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	edit(&entry.run)
	raw, err := json.Marshal(entry.run)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.store.UpdateSupervisedSwarmRun(ctx, entry.run.ID, raw, time.Now().UTC())
}
func taskCopy(entry *execution, index int) Task {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.run.Tasks[index]
}
func runCopy(entry *execution) Run {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	raw, _ := json.Marshal(entry.run)
	var snapshot Run
	_ = json.Unmarshal(raw, &snapshot)
	return snapshot
}
func (s *Service) execute(ctx context.Context, entry *execution) {
	defer close(entry.done)
	defer entry.cancel()
	var wg sync.WaitGroup
	for i := range entry.run.Tasks {
		wg.Add(1)
		go func(index int) { defer wg.Done(); s.worker(ctx, entry, index) }(i)
	}
	wg.Wait()
	s.finish(entry)
	s.mu.Lock()
	delete(s.active, entry.run.ID)
	s.mu.Unlock()
}
func (s *Service) worker(ctx context.Context, entry *execution, index int) {
	set := func(t Task) error { return s.persist(entry, func(r *Run) { r.Tasks[index] = t }) }
	t := taskCopy(entry, index)
	t.Status = "starting"
	if err := set(t); err != nil {
		entry.cancel()
		return
	}
	if ctx.Err() != nil {
		t.Status = "not_started"
		t.Error = ctx.Err().Error()
		_ = set(t)
		return
	}
	launchctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	sid, launchErr := s.runtime.Launch(launchctx, entry.run.Plan.ProjectID, t.RequestID, t.Branch, t.Packet)
	cancel()
	if sid == "" {
		lookup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		sid, _ = s.runtime.Find(lookup, t.RequestID)
		cancel()
	}
	if sid == "" {
		t.Status = "launch_unknown"
		t.Error = "Launch did not yield a reconciled session; no retry performed."
		_ = set(t)
		return
	}
	t.SessionID = sid
	t.Status = "running"
	if launchErr != nil {
		t.Error = "Launch returned an error; recorded session is retained for stop reconciliation."
	}
	if err := set(t); err != nil {
		entry.cancel()
		s.stopTask(entry, index, t, false)
		return
	}
	complete := false
	if launchErr == nil {
		for ctx.Err() == nil {
			readctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			obs, err := s.runtime.Observe(readctx, sid)
			cancel()
			if err != nil {
				t.Error = "Observation failed; stop outcome requires verification."
				break
			}
			t.Observation = obs
			if err := set(t); err != nil {
				entry.cancel()
				break
			}
			if obs.CompletedAt != nil {
				complete = obs.TurnState == "completed"
				break
			}
			timer := time.NewTimer(s.poll)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}
	s.stopTask(entry, index, t, complete)
}
func (s *Service) stopTask(entry *execution, index int, t Task, completed bool) {
	stopctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := s.runtime.Stop(stopctx, t.SessionID)
	cancel()
	readctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	observed, readErr := s.runtime.Observe(readctx, t.SessionID)
	cancel()
	if readErr == nil {
		t.Observation = observed
	}
	if err != nil || readErr != nil || observed.Controller != "stopped" {
		t.Status = "stop_unknown"
		t.Error = "Stop is unconfirmed; session and worktree remain recorded. No automatic stop retry."
	} else {
		t.Status = "stopped"
		if completed {
			t.Status = "completed"
		}
		freezeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := s.claimWorkspace(freezeCtx, entry, index, &t)
		if err == nil {
			err = s.freeze(freezeCtx, runCopy(entry), index, &t)
		}
		if err != nil {
			t.Status = "needs_attention"
			t.Error = "Stopped, but candidate capture failed: " + err.Error()
		}
		cancel()
	}
	_ = s.persist(entry, func(r *Run) { r.Tasks[index] = t })
}
func (s *Service) finish(entry *execution) {
	run := runCopy(entry)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	unchanged := cleanSource(ctx, run.SourcePath, run.Plan.SourceCommit) == nil
	if unchanged {
		unchanged = sourceMatches(run.SourceSnapshot, run.SourcePath) == nil
	}
	_ = s.persist(entry, func(r *Run) {
		r.SourceUnchanged = unchanged
		r.Status = "completed"
		if !unchanged {
			r.Status = "needs_attention"
			r.Errors = append(r.Errors, "Source preservation check failed.")
		}
		for _, t := range r.Tasks {
			if t.Status == "stopped" || t.Status == "not_started" {
				if r.Status == "completed" {
					r.Status = "stopped"
				}
			} else if t.Status != "completed" {
				r.Status = "needs_attention"
			}
		}
	})
}

// Stop cancels an active run or reconciles recorded sessions without relaunching them.
func (s *Service) Stop(ctx context.Context, id string) (Run, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Run{}, err
	}
	s.mu.Lock()
	entry := s.active[id]
	s.mu.Unlock()
	if entry != nil {
		entry.cancel()
		select {
		case <-entry.done:
			return s.Get(ctx, id)
		case <-ctx.Done():
			return Run{}, ctx.Err()
		}
	}
	run, err := s.Get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Status == "completed" || run.Status == "stopped" {
		return run, nil
	}
	// Explicit recovery observes existing request identities, never starts a session.
	entry = &execution{run: run, done: make(chan struct{}), cancel: func() {}}
	s.mu.Lock()
	if s.active[id] != nil {
		s.mu.Unlock()
		return Run{}, domain.ErrSwarmConflict
	}
	s.active[id] = entry
	s.mu.Unlock()
	defer func() { close(entry.done); s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }()
	for i, t := range run.Tasks {
		var lookupErr error
		if t.SessionID == "" {
			t.SessionID, lookupErr = s.runtime.Find(ctx, t.RequestID)
		}
		if lookupErr != nil || t.SessionID == "" {
			t.Status = "launch_unknown"
			t.Error = "No reconciled session identity; absence does not authorize another launch."
			_ = s.persist(entry, func(r *Run) { r.Tasks[i] = t })
			continue
		}
		s.stopTask(entry, i, t, t.Observation.TurnState == "completed")
	}
	s.finish(entry)
	return s.Get(ctx, id)
}

// Close cancels active executions and waits a bounded time for each to settle.
func (s *Service) Close() {
	s.mu.Lock()
	entries := make([]*execution, 0, len(s.active))
	for _, entry := range s.active {
		entries = append(entries, entry)
		entry.cancel()
	}
	s.mu.Unlock()
	for _, entry := range entries {
		select {
		case <-entry.done:
		case <-time.After(60 * time.Second):
		}
	}
}

func saveJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
