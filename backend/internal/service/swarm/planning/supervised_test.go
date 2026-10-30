package planning

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func supervisedFixture(t *testing.T) (domain.SwarmPlan, []SuppliedContext) {
	t.Helper()
	plan := validationFixturePlan()
	second := validationFixturePlan().Tasks[0]
	second.ID = "getter"
	second.Resources[0].Key = "src/getter"
	plan.Tasks = append(plan.Tasks, second)
	plan.MaxActive = 2
	tool := packetTool(t)
	supplied := []SuppliedContext{{ID: "spec", Body: "Exact setter specification"}, {ID: "source", Body: "Frozen source manifest"}}
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		task.Agent = domain.SwarmSelection{Harness: domain.HarnessCodex, Model: "gpt-5.6-luna", Effort: "low"}
		task.Work.Tools = []domain.SwarmToolIdentity{tool}
		task.Work.Acceptance.Checks[0].Command[0] = tool.Path
		for j := range task.Work.Context {
			task.Work.Context[j].SHA256 = packetDigest(supplied[j].Body)
		}
	}
	return plan, supplied
}

func TestSupervisedPacketRetainsContractAndHonestIdentity(t *testing.T) {
	plan, supplied := supervisedFixture(t)
	before := validationJSON(t, plan)
	packet, err := ComposeSupervised(context.Background(), "supervised-run", plan, "setter", supplied, FileToolVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if packet.Profile != SupervisedProfile || packet.TaskID != "setter" || packet.SourceCommit != plan.SourceCommit || packet.Requested != plan.Tasks[0].Agent {
		t.Fatalf("wrong packet identity: %+v", packet)
	}
	for _, want := range []string{"std::invalid_argument", "BEFORE mutation", "unchanged", "src/setter", "setter_invalid", "expectedExitCode", "submit_and_stop", "report_and_stop", "advisory", "not instructions or Praxis approval", packet.ContractHash, packet.PlanHash, supplied[0].Body} {
		if !strings.Contains(packet.Markdown, want) {
			t.Fatalf("missing contract/provenance: %q", want)
		}
	}
	raw, err := json.Marshal(packet)
	if err != nil || len(raw) != packet.ByteCount || len(packet.Markdown) > MaxSupervisedPromptBytes {
		t.Fatalf("incorrect byte count: %v", err)
	}
	for _, invented := range []string{"decisionId", "decisionVersion", "attemptId", "capabilityFingerprint", "generation"} {
		if strings.Contains(string(raw), invented) {
			t.Fatalf("invented authority field %q", invented)
		}
	}
	sum := packet.SHA256
	packet.SHA256 = ""
	if sum != domain.SwarmHash(packet) {
		t.Fatal("packet hash does not cover envelope")
	}
	if string(before) != string(validationJSON(t, plan)) {
		t.Fatal("composition mutated plan")
	}
	second, err := ComposeSupervised(context.Background(), "supervised-run", plan, "getter", supplied, FileToolVerifier{})
	if err != nil || second.ContractHash == packet.ContractHash {
		t.Fatalf("second assignment not distinct: %v", err)
	}
}

func TestSupervisedPlanRejectsUnsupportedOrConflictingWork(t *testing.T) {
	cases := map[string]func(*domain.SwarmPlan){
		"one task": func(p *domain.SwarmPlan) { p.Tasks = p.Tasks[:1] },
		"capacity": func(p *domain.SwarmPlan) { p.MaxActive = 1 },
		"dependency": func(p *domain.SwarmPlan) {
			p.Tasks[1].Dependencies = []domain.SwarmDependency{{TaskID: "setter", Version: 1}}
		},
		"role":             func(p *domain.SwarmPlan) { p.Tasks[0].Role = "reviewer" },
		"other harness":    func(p *domain.SwarmPlan) { p.Tasks[0].Agent.Harness = "claude-code" },
		"different model":  func(p *domain.SwarmPlan) { p.Tasks[1].Agent.Model = "different" },
		"implicit effort":  func(p *domain.SwarmPlan) { p.Tasks[0].Agent.Effort = ""; p.Tasks[1].Agent.Effort = "" },
		"different effort": func(p *domain.SwarmPlan) { p.Tasks[1].Agent.Effort = "medium" },
		"same path":        func(p *domain.SwarmPlan) { p.Tasks[1].Resources[0].Key = "src/setter" },
		"parent path":      func(p *domain.SwarmPlan) { p.Tasks[1].Resources[0].Key = "src" },
		"namespace bypass": func(p *domain.SwarmPlan) {
			p.Tasks[1].Resources[0].Key = "src/setter"
			p.Tasks[1].Resources[0].Namespace = "other"
		},
		"semantic conflict": func(p *domain.SwarmPlan) {
			for i := range p.Tasks {
				p.Tasks[i].Resources = append(p.Tasks[i].Resources, domain.SwarmResource{Namespace: "repository", Kind: "semantic", Key: "shared-api"})
			}
		},
		"no path": func(p *domain.SwarmPlan) { p.Tasks[0].Resources[0].Kind = "semantic" },
		"lesson": func(p *domain.SwarmPlan) {
			p.Tasks[0].Work.Context = append(p.Tasks[0].Work.Context, domain.SwarmContextRef{ID: "lesson", Kind: "lesson", SHA256: strings.Repeat("a", 64)})
		},
		"pattern": func(p *domain.SwarmPlan) {
			p.Tasks[0].Work.Context = append(p.Tasks[0].Work.Context, domain.SwarmContextRef{ID: "pattern", Kind: "pattern", SHA256: strings.Repeat("a", 64)})
		},
		"ambiguous setter": func(p *domain.SwarmPlan) { p.Tasks[0].Work.Errors[0].Value = "error" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			plan, _ := supervisedFixture(t)
			change(&plan)
			if err := ValidateSupervisedPlan(plan); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected invalid plan, got %v", err)
			}
		})
	}
}

func TestSupervisedContextIdentityAndSelectors(t *testing.T) {
	cases := map[string]struct {
		edit func(*domain.SwarmPlan, *[]SuppliedContext)
		want error
	}{
		"missing required":   {func(_ *domain.SwarmPlan, s *[]SuppliedContext) { *s = (*s)[:1] }, ErrContext},
		"duplicate supplied": {func(_ *domain.SwarmPlan, s *[]SuppliedContext) { *s = append(*s, (*s)[0]) }, ErrInvalid},
		"unknown supplied": {func(_ *domain.SwarmPlan, s *[]SuppliedContext) {
			*s = append(*s, SuppliedContext{ID: "unknown", Body: "text"})
		}, ErrInvalid},
		"hash change": {func(_ *domain.SwarmPlan, s *[]SuppliedContext) { (*s)[0].Body += " changed" }, ErrContext},
		"required task selector": {func(p *domain.SwarmPlan, _ *[]SuppliedContext) {
			p.Tasks[0].Work.Context[0].Selectors.TaskTypes = []string{"other"}
		}, ErrContext},
		"required backend selector": {func(p *domain.SwarmPlan, _ *[]SuppliedContext) {
			p.Tasks[0].Work.Context[0].Selectors.Backends = []string{"other"}
		}, ErrContext},
		"required path selector": {func(p *domain.SwarmPlan, _ *[]SuppliedContext) {
			p.Tasks[0].Work.Context[0].Selectors.Paths = []string{"unrelated/**"}
		}, ErrContext},
		"conflicting shared identity": {func(p *domain.SwarmPlan, _ *[]SuppliedContext) {
			p.Tasks[1].Work.Context[0].SHA256 = strings.Repeat("c", 64)
		}, ErrInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p, s := supervisedFixture(t)
			tc.edit(&p, &s)
			if _, err := ComposeSupervised(context.Background(), "run", p, "setter", s, FileToolVerifier{}); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
	plan, supplied := supervisedFixture(t)
	plan.Tasks[0].Work.Context = append(plan.Tasks[0].Work.Context, domain.SwarmContextRef{ID: "optional", Kind: "reference", SHA256: packetDigest("body"), Selectors: domain.SwarmContextSelectors{Paths: []string{"elsewhere/**"}}})
	supplied = append(supplied, SuppliedContext{ID: "optional", Body: "body"})
	packet, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{})
	if err != nil || !reflect.DeepEqual(packet.Omitted, []OmittedContext{{ID: "optional", Reason: "not_applicable"}}) {
		t.Fatalf("optional selector omission: %+v %v", packet.Omitted, err)
	}
	supplied[2].Body = "wrong even when inapplicable"
	if _, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{}); !errors.Is(err, ErrContext) {
		t.Fatalf("hash mismatch ignored: %v", err)
	}
}

func TestSupervisedWholePacketBudgetAndOptionalBodies(t *testing.T) {
	plan, supplied := supervisedFixture(t)
	base, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	plan.Tasks[0].Work.ContextByteLimit = base.ByteCount - 1
	if _, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{}); !errors.Is(err, ErrBudget) {
		t.Fatalf("serialized budget bypassed: %v", err)
	}
	plan.Tasks[0].Work.ContextByteLimit = base.ByteCount
	if _, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{}); err != nil {
		t.Fatalf("exact serialized budget: %v", err)
	}
	plan.Tasks[0].Work.ContextByteLimit = MaxPacketBytes
	large, small := strings.Repeat("x", MaxSupervisedPromptBytes), "SMALL_COMPLETE_REFERENCE"
	plan.Tasks[0].Work.Context = append(plan.Tasks[0].Work.Context,
		domain.SwarmContextRef{ID: "a-large", Kind: "reference", SHA256: packetDigest(large)},
		domain.SwarmContextRef{ID: "z-small", Kind: "reference", SHA256: packetDigest(small)})
	supplied = append(supplied, SuppliedContext{ID: "a-large", Body: large}, SuppliedContext{ID: "z-small", Body: small})
	packet, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(packet.Omitted, []OmittedContext{{ID: "a-large", Reason: "budget"}}) || !strings.Contains(packet.Markdown, small) || strings.Contains(packet.Markdown, strings.Repeat("x", 100)) {
		t.Fatalf("partial body or later candidate lost: %+v", packet.Omitted)
	}
}

func TestSupervisedActualPromptBoundary(t *testing.T) {
	plan, supplied := supervisedFixture(t)
	packet, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	supplied[0].Body = strings.Repeat("x", len(supplied[0].Body)+MaxSupervisedPromptBytes-len(packet.Markdown))
	setHash := func() {
		for i := range plan.Tasks {
			plan.Tasks[i].Work.Context[0].SHA256 = packetDigest(supplied[0].Body)
		}
	}
	setHash()
	packet, err = ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{})
	if err != nil || len(packet.Markdown) != MaxSupervisedPromptBytes {
		t.Fatalf("exact16KiB boundary: %d %v", len(packet.Markdown), err)
	}
	supplied[0].Body += "x"
	setHash()
	if _, err = ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{}); !errors.Is(err, ErrBudget) {
		t.Fatalf("oversized actual prompt accepted: %v", err)
	}
}

func TestSupervisedToolChangesAndVerifierIsolation(t *testing.T) {
	plan, supplied := supervisedFixture(t)
	path := plan.Tasks[0].Work.Tools[0].Path
	calls := 0
	mutating := packetVerifier(func(ctx context.Context, tools []domain.SwarmToolIdentity) error {
		calls++
		err := (FileToolVerifier{}).Verify(ctx, tools)
		tools[0].SHA256 = "mutated callback slice"
		return err
	})
	if _, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, mutating); err != nil || calls != 2 {
		t.Fatalf("verifier mutation escaped clone: %v calls%d", err, calls)
	}
	calls = 0
	changing := packetVerifier(func(ctx context.Context, tools []domain.SwarmToolIdentity) error {
		calls++
		err := (FileToolVerifier{}).Verify(ctx, tools)
		if calls == 1 {
			if e := os.WriteFile(path, []byte("changed executable; never executed"), 0700); e != nil {
				t.Fatal(e)
			}
		}
		return err
	})
	if _, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, changing); !errors.Is(err, ErrStale) || calls != 2 {
		t.Fatalf("tool replacement during compose accepted: %v calls%d", err, calls)
	}
	if _, err := ComposeSupervised(context.Background(), "run", plan, "setter", supplied, FileToolVerifier{}); !errors.Is(err, ErrStale) {
		t.Fatalf("changed tool hash accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ComposeSupervised(ctx, "run", plan, "setter", supplied, FileToolVerifier{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
