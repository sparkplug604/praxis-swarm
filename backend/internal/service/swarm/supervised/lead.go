package supervised

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
)

// Prepare builds packets from lead decisions and daemon-observed source facts, without launching.
func (s *Service) Prepare(ctx context.Context, p planning.LeadProposal) (planning.PreparedProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	brief, err := s.checkBrief(ctx, p.ProjectID, p.Brief)
	if err != nil {
		return planning.PreparedProposal{}, err
	}
	if brief.Revision > 0 {
		body, _ := json.Marshal(brief.Brief)
		p.Specification = "Project brief (supplied decisions; do not override):\n" + string(body) + "\nTask-specific details:\n" + p.Specification
	}
	source, err := s.runtime.ProjectPath(ctx, p.ProjectID)
	if err != nil {
		return planning.PreparedProposal{}, err
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return planning.PreparedProposal{}, err
	}
	dir, err := os.MkdirTemp(s.root, "lead-prepare-")
	if err != nil {
		return planning.PreparedProposal{}, err
	}
	snapshot, err := workspace.Capture(ctx, workspace.CaptureRequest{SourceRoot: source, Destination: filepath.Join(dir, "source")})
	if err != nil {
		return planning.PreparedProposal{}, err
	}
	if err := cleanSource(ctx, source, snapshot.Source.Head); err != nil {
		return planning.PreparedProposal{}, err
	}
	result, err := planning.BuildProposal(ctx, p, snapshot, s.tools)
	if err != nil {
		return planning.PreparedProposal{}, err
	}
	if err := saveJSON(filepath.Join(dir, "prepared.json"), result); err != nil {
		return planning.PreparedProposal{}, err
	}
	return result, nil
}

// ReviewBundle binds a combined candidate and executable check specifications to a run.
// It requests lead review; it never treats worker reports as acceptance or runs model-supplied commands.
type ReviewBundle struct {
	Run          Run                           `json:"run"`
	Directory    string                        `json:"directory"`
	Checks       []domain.SwarmAcceptanceCheck `json:"checks"`
	CreatedAt    time.Time                     `json:"createdAt"`
	Instructions string                        `json:"instructions"`
}

// Review assembles a fresh combined candidate from verified frozen snapshots for the lead.
func (s *Service) Review(ctx context.Context, id string) (ReviewBundle, error) {
	out := ReviewBundle{}
	run, err := s.Get(ctx, id)
	if err != nil {
		return out, err
	}
	if run.Status != "completed" {
		return out, fmt.Errorf("%w: review requires a completed run; inspect stopped/uncertain evidence with get", domain.ErrSwarmNotReady)
	}
	baseline, err := loadSnapshot(run.SourceSnapshot)
	if err != nil {
		return out, err
	}
	if err := workspace.ValidateForLaunch(ctx, baseline); err != nil {
		return out, err
	}
	dir, err := os.MkdirTemp(filepath.Join(s.root, id), "lead-review-")
	if err != nil {
		return out, err
	}
	candidate := filepath.Join(dir, "files")
	if err := os.Mkdir(candidate, 0o700); err != nil {
		return out, err
	}
	if err := copyReviewEntries(ctx, baseline, baseline.Source.Entries, candidate); err != nil {
		return out, err
	}
	before := content(baseline.Source.Entries)
	for i, task := range run.Tasks {
		snapshot, err := loadSnapshot(task.SnapshotPath)
		if err != nil {
			return out, err
		}
		if err := workspace.ValidateForLaunch(ctx, snapshot); err != nil {
			return out, err
		}
		if snapshot.ManifestHash != task.SnapshotHash || domain.SwarmHash(content(snapshot.Source.Entries)) != task.CandidateHash {
			return out, workspace.ErrChanged
		}
		if err := validateScope(baseline.Source.Entries, snapshot.Source.Entries, run.Plan.Tasks[i].Resources); err != nil {
			return out, err
		}
		after := content(snapshot.Source.Entries)
		changed := []workspace.Entry{}
		for path := range before {
			if _, ok := after[path]; !ok {
				if err := os.Remove(filepath.Join(candidate, path)); err != nil {
					return out, err
				}
			}
		}
		for _, entry := range snapshot.Source.Entries {
			if !entry.Directory && !entry.Missing && before[entry.Path] != after[entry.Path] {
				changed = append(changed, entry)
			}
		}
		if err := copyReviewEntries(ctx, snapshot, changed, candidate); err != nil {
			return out, err
		}
		out.Checks = append(out.Checks, run.Plan.Tasks[i].Work.Acceptance.Checks...)
	}
	if err := s.tools.Verify(ctx, run.Plan.Tasks[0].Work.Tools); err != nil {
		return out, err
	}
	if err := s.tools.Verify(ctx, run.Plan.Tasks[1].Work.Tools); err != nil {
		return out, err
	}
	out.Run, out.Directory, out.CreatedAt = run, candidate, time.Now().UTC()
	out.Instructions = "This is a review candidate, not an accepted integration. Inspect changes against both task contracts. In directory, run each checks.command as argv using the recorded tools, compare actual exit/output with expectedExitCode/expectedResult, and report actual results, defects and blockers. Candidate code executes with your review session's permissions; retain its logs. Do not claim tests ran from worker reports. Do not merge, publish or relaunch workers without an explicit request."
	if err := saveJSON(filepath.Join(dir, "review.json"), out); err != nil {
		return ReviewBundle{}, err
	}
	return out, nil
}

func copyReviewEntries(ctx context.Context, snapshot workspace.Snapshot, entries []workspace.Entry, destination string) error {
	root, err := os.OpenRoot(snapshot.Destination.Path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Missing || entry.Directory {
			continue
		}
		target := filepath.Join(destination, entry.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := root.Open(entry.Path)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(entry.Mode))
		if err != nil {
			_ = source.Close()
			return err
		}
		_, copyErr := io.Copy(file, source)
		readClose := source.Close()
		writeClose := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if readClose != nil {
			return readClose
		}
		if writeClose != nil {
			return writeClose
		}
		if err := os.Chmod(target, os.FileMode(entry.Mode)); err != nil {
			return err
		}
	}
	return workspace.ValidateForLaunch(ctx, snapshot)
}
