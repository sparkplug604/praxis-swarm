package planning

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
)

func TestSupervisedGeneratedFixtureSatisfiesServiceContracts(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the standalone fixture generator")
	}
	if _, err = exec.LookPath("git"); err != nil {
		t.Skip("git is required for the standalone fixture generator")
	}
	script, err := filepath.Abs("../../../../../scripts/swarm/create-supervised-demo.py")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "fixture")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-B", script, "--output-dir", output)
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture generation failed: %v\n%s", err, raw)
	}
	raw, err := os.ReadFile(filepath.Join(output, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("generated plan cannot be parsed: %v", err)
	}
	if err = ValidateSupervisedPlan(plan); err != nil {
		t.Fatalf("generated plan violates the supervised profile: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(output, "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	var supplied []SuppliedContext
	if err = json.Unmarshal(raw, &supplied); err != nil {
		t.Fatal(err)
	}

	proposalRaw, err := os.ReadFile(filepath.Join(output, "proposal.json"))
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := ParseProposal(proposalRaw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := workspace.Capture(ctx, workspace.CaptureRequest{SourceRoot: filepath.Join(output, "repository"), Destination: filepath.Join(output, "lead-source")})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := BuildProposal(ctx, proposal, snapshot, FileToolVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan.SourceCommit != plan.SourceCommit || len(prepared.Packets) != 2 {
		t.Fatal("generated lead proposal lost source or packets")
	}
	for _, task := range plan.Tasks {
		t.Run(task.ID, func(t *testing.T) {
			packet, err := ComposeSupervised(ctx, "generated-fixture-validation", plan, task.ID, supplied, FileToolVerifier{})
			if err != nil {
				t.Fatalf("generated assignment cannot be composed with actual tool identity: %v", err)
			}
			if packet.SourceCommit != plan.SourceCommit || packet.Requested != task.Agent || packet.TaskID != task.ID ||
				packet.ByteCount > task.Work.ContextByteLimit || len(packet.Markdown) > MaxSupervisedPromptBytes || len(packet.Omitted) != 0 {
				t.Fatalf("generated assignment identity or context changed: %+v", packet)
			}
			t.Logf("%s: serialized packet %d bytes; prompt %d bytes", task.ID, packet.ByteCount, len(packet.Markdown))
		})
	}
}
