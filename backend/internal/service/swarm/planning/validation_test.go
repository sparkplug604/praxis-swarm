package planning

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func validationFixturePlan() domain.SwarmPlan {
	hash := strings.Repeat("a", 64)
	return domain.SwarmPlan{
		ProtocolVersion: 1, Version: 1, ProjectID: "test-project", SourceCommit: strings.Repeat("b", 40),
		Objective: "Define deterministic setter failure behavior", BudgetBucket: "test-budget", TokenLimit: 10000, MaxActive: 3,
		Tasks: []domain.SwarmTaskContract{{
			ID: "setter", Version: 1, Objective: "Reject an invalid setter value before mutation", OracleHash: hash,
			Agent:     domain.SwarmSelection{Harness: "command-code", Model: "test-worker", Effort: "medium"},
			Resources: []domain.SwarmResource{{Namespace: "repository", Kind: "path", Key: "src/setter"}}, ReserveTokens: 100,
			Work: &domain.SwarmWorkSpec{
				Version: 1, TaskType: "implementation", ContextByteLimit: MaxPacketBytes, NonGoals: []string{"Do not change unrelated getters"},
				Interfaces:   []domain.SwarmInterface{{ID: "set-value", Contract: "void setValue(double value)", Ownership: "Worker owns src/setter", ErrorRuleIDs: []string{"invalid-value"}}},
				Errors:       []domain.SwarmErrorRule{{ID: "invalid-value", When: "value is negative or non-finite", Signal: "throw", Value: "std::invalid_argument", Destination: "unchanged", Effect: "Throw BEFORE mutation; the destination and every other field retain their original values"}},
				Acceptance:   domain.SwarmAcceptance{OracleID: "setter-oracle-v1", Checks: []domain.SwarmAcceptanceCheck{{ID: "invalid-value-before-mutation", Command: []string{"/recorded/toolchain/ctest", "-R", "setter_invalid"}, ExpectedExitCode: 0, ExpectedResult: "std::invalid_argument is thrown BEFORE mutation and destination remains unchanged", ErrorRuleID: "invalid-value"}}},
				Context:      []domain.SwarmContextRef{{ID: "spec", Kind: "spec", SHA256: hash, Required: true}, {ID: "source", Kind: "source_manifest", SHA256: hash, Required: true}},
				Tools:        []domain.SwarmToolIdentity{{Name: "ctest", Path: "/recorded/toolchain/ctest", SHA256: hash, Version: "3.30.1"}},
				ProgressPath: "docs/swarm/PROGRESS.md", OnComplete: "submit_and_stop", OnBlocked: "report_and_stop",
			},
		}},
	}
}

func validationJSON(t *testing.T, plan domain.SwarmPlan) []byte {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParsePlanQ1SetterExceptionBeforeMutation(t *testing.T) {
	plan, err := ParsePlan(validationJSON(t, validationFixturePlan()))
	if err != nil {
		t.Fatal(err)
	}
	rule := plan.Tasks[0].Work.Errors[0]
	if rule.Signal != "throw" || rule.Value != "std::invalid_argument" || rule.Destination != "unchanged" || !strings.Contains(rule.Effect, "BEFORE mutation") {
		t.Fatalf("lost precise setter error contract: %+v", rule)
	}
	if !strings.Contains(plan.Tasks[0].Work.Acceptance.Checks[0].ExpectedResult, "BEFORE mutation") {
		t.Fatal("missing behavioral acceptance expectation")
	}
}

func TestParsePlanStrictJSON(t *testing.T) {
	baseline := string(validationJSON(t, validationFixturePlan()))
	cases := map[string]string{
		"empty":                    "",
		"oversized":                strings.Repeat(" ", MaxPlannerBytes+1),
		"invalid utf8":             baseline[:len(baseline)-1] + string([]byte{0xff}) + "}",
		"trailing object":          baseline + "{}",
		"trailing scalar":          baseline + "true",
		"trailing malformed":       baseline + "!",
		"duplicate plan key":       strings.Replace(baseline, `"version":1`, `"version":2,"version":1`, 1),
		"escaped duplicate key":    strings.Replace(baseline, `"version":1`, `"\u0076ersion":2,"version":1`, 1),
		"duplicate nested key":     strings.Replace(baseline, `"signal":"throw"`, `"signal":"reject","signal":"throw"`, 1),
		"wrong case alias":         strings.Replace(baseline, `"onComplete":`, `"OnComplete":`, 1),
		"unknown root":             strings.Replace(baseline, `"protocolVersion":1`, `"unexpected":true,"protocolVersion":1`, 1),
		"unknown task":             strings.Replace(baseline, `"id":"setter"`, `"id":"setter","startupInstructions":"restart old task"`, 1),
		"unknown agent":            strings.Replace(baseline, `"model":"test-worker"`, `"model":"test-worker","providerFallback":true`, 1),
		"unknown resource":         strings.Replace(baseline, `"namespace":"repository"`, `"namespace":"repository","allowAll":true`, 1),
		"unknown work":             strings.Replace(baseline, `"taskType":"implementation"`, `"taskType":"implementation","nextTask":"old-trial"`, 1),
		"unknown interface":        strings.Replace(baseline, `"id":"set-value"`, `"id":"set-value","extra":true`, 1),
		"unknown error":            strings.Replace(baseline, `"signal":"throw"`, `"signal":"throw","optional":true`, 1),
		"unknown acceptance":       strings.Replace(baseline, `"oracleId":"setter-oracle-v1"`, `"oracleId":"setter-oracle-v1","allowSkip":true`, 1),
		"unknown check":            strings.Replace(baseline, `"expectedExitCode":0`, `"expectedExitCode":0,"allowFailure":true`, 1),
		"missing expected exit":    strings.Replace(baseline, `"expectedExitCode":0,`, ``, 1),
		"null expected exit":       strings.Replace(baseline, `"expectedExitCode":0`, `"expectedExitCode":null`, 1),
		"fractional expected exit": strings.Replace(baseline, `"expectedExitCode":0`, `"expectedExitCode":0.1`, 1),
		"unknown context":          strings.Replace(baseline, `"id":"spec"`, `"id":"spec","approved":true`, 1),
		"unknown selector":         strings.Replace(baseline, `"taskTypes":null`, `"taskTypes":null,"all":true`, 1),
		"unknown tool":             strings.Replace(baseline, `"name":"ctest"`, `"name":"ctest","trustMe":true`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePlan([]byte(raw)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
	if _, err := ParsePlan([]byte(baseline + " \r\n\t")); err != nil {
		t.Fatalf("valid trailing whitespace rejected: %v", err)
	}
}

func TestValidateWorkRejectsIncompleteOrAmbiguousContract(t *testing.T) {
	cases := map[string]func(*domain.SwarmWorkSpec){
		"missing context budget":  func(w *domain.SwarmWorkSpec) { w.ContextByteLimit = 0 },
		"large context budget":    func(w *domain.SwarmWorkSpec) { w.ContextByteLimit = MaxPacketBytes + 1 },
		"old version":             func(w *domain.SwarmWorkSpec) { w.Version = 2 },
		"no type":                 func(w *domain.SwarmWorkSpec) { w.TaskType = "" },
		"no non goals":            func(w *domain.SwarmWorkSpec) { w.NonGoals = nil },
		"unbounded prose":         func(w *domain.SwarmWorkSpec) { w.NonGoals[0] = strings.Repeat("n", 4097) },
		"no interfaces":           func(w *domain.SwarmWorkSpec) { w.Interfaces = nil },
		"duplicate interface":     func(w *domain.SwarmWorkSpec) { w.Interfaces = append(w.Interfaces, w.Interfaces[0]) },
		"no owner":                func(w *domain.SwarmWorkSpec) { w.Interfaces[0].Ownership = "" },
		"no interface error":      func(w *domain.SwarmWorkSpec) { w.Interfaces[0].ErrorRuleIDs = nil },
		"unknown interface error": func(w *domain.SwarmWorkSpec) { w.Interfaces[0].ErrorRuleIDs = []string{"missing"} },
		"no errors":               func(w *domain.SwarmWorkSpec) { w.Errors = nil },
		"duplicate error":         func(w *domain.SwarmWorkSpec) { w.Errors = append(w.Errors, w.Errors[0]) },
		"ambiguous signal":        func(w *domain.SwarmWorkSpec) { w.Errors[0].Signal = "throw_or_return_error" },
		"ambiguous value":         func(w *domain.SwarmWorkSpec) { w.Errors[0].Value = "error" },
		"no condition":            func(w *domain.SwarmWorkSpec) { w.Errors[0].When = "" },
		"ambiguous destination":   func(w *domain.SwarmWorkSpec) { w.Errors[0].Destination = "maybe changed" },
		"missing mutation effect": func(w *domain.SwarmWorkSpec) { w.Errors[0].Effect = "" },
		"no oracle":               func(w *domain.SwarmWorkSpec) { w.Acceptance.OracleID = "" },
		"no checks":               func(w *domain.SwarmWorkSpec) { w.Acceptance.Checks = nil },
		"duplicate check": func(w *domain.SwarmWorkSpec) {
			w.Acceptance.Checks = append(w.Acceptance.Checks, w.Acceptance.Checks[0])
		},
		"no argv":               func(w *domain.SwarmWorkSpec) { w.Acceptance.Checks[0].Command = nil },
		"nul argv":              func(w *domain.SwarmWorkSpec) { w.Acceptance.Checks[0].Command = []string{"tool", "bad\x00arg"} },
		"negative exit":         func(w *domain.SwarmWorkSpec) { w.Acceptance.Checks[0].ExpectedExitCode = -1 },
		"large exit":            func(w *domain.SwarmWorkSpec) { w.Acceptance.Checks[0].ExpectedExitCode = 256 },
		"no result":             func(w *domain.SwarmWorkSpec) { w.Acceptance.Checks[0].ExpectedResult = "" },
		"untested error":        func(w *domain.SwarmWorkSpec) { w.Acceptance.Checks[0].ErrorRuleID = "" },
		"unknown checked error": func(w *domain.SwarmWorkSpec) { w.Acceptance.Checks[0].ErrorRuleID = "missing" },
		"no spec":               func(w *domain.SwarmWorkSpec) { w.Context[0].Required = false },
		"no source manifest":    func(w *domain.SwarmWorkSpec) { w.Context[1].Required = false },
		"two required manifests": func(w *domain.SwarmWorkSpec) {
			ref := w.Context[1]
			ref.ID = "second-manifest"
			w.Context = append(w.Context, ref)
		},
		"duplicate context": func(w *domain.SwarmWorkSpec) { w.Context = append(w.Context, w.Context[0]) },
		"bad context hash":  func(w *domain.SwarmWorkSpec) { w.Context[0].SHA256 = strings.Repeat("A", 64) },
		"bad context kind":  func(w *domain.SwarmWorkSpec) { w.Context[0].Kind = "active_instructions" },
		"too many refs": func(w *domain.SwarmWorkSpec) {
			for len(w.Context) < 129 {
				w.Context = append(w.Context, w.Context[0])
			}
		},
		"duplicate selectors": func(w *domain.SwarmWorkSpec) {
			w.Context[0].Selectors.TaskTypes = []string{"implementation", "implementation"}
		},
		"invalid selector id": func(w *domain.SwarmWorkSpec) { w.Context[0].Selectors.Backends = []string{"backend\n"} },
		"no tools":            func(w *domain.SwarmWorkSpec) { w.Tools = nil },
		"duplicate tool":      func(w *domain.SwarmWorkSpec) { w.Tools = append(w.Tools, w.Tools[0]) },
		"too many tools": func(w *domain.SwarmWorkSpec) {
			for len(w.Tools) < 33 {
				w.Tools = append(w.Tools, w.Tools[0])
			}
		},
		"relative tool":         func(w *domain.SwarmWorkSpec) { w.Tools[0].Path = "bin/ctest" },
		"unclean tool":          func(w *domain.SwarmWorkSpec) { w.Tools[0].Path = "/bin/../bin/ctest" },
		"no tool version":       func(w *domain.SwarmWorkSpec) { w.Tools[0].Version = "" },
		"bad tool hash":         func(w *domain.SwarmWorkSpec) { w.Tools[0].SHA256 = "unknown" },
		"no progress":           func(w *domain.SwarmWorkSpec) { w.ProgressPath = "" },
		"directory progress":    func(w *domain.SwarmWorkSpec) { w.ProgressPath = "." },
		"progress escapes":      func(w *domain.SwarmWorkSpec) { w.ProgressPath = "../PROGRESS.md" },
		"restart on completion": func(w *domain.SwarmWorkSpec) { w.OnComplete = "restart_old_trial" },
		"continue when blocked": func(w *domain.SwarmWorkSpec) { w.OnBlocked = "continue_anyway" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			task := validationFixturePlan().Tasks[0]
			mutate(task.Work)
			if err := ValidateWork(task); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

func TestValidateWorkContextPaths(t *testing.T) {
	for _, selector := range []string{".", "src", "src/setter.cc", "src/**", "src/internal/**"} {
		t.Run("valid "+selector, func(t *testing.T) {
			task := validationFixturePlan().Tasks[0]
			task.Work.Context[0].Selectors.Paths = []string{selector}
			if err := ValidateWork(task); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, selector := range []string{"", "/src", "../src", "src/../lib", "./src", "./**", "src//lib", "src/", "src/*", "src/**/file", "src/a?", "src/[a]", "src\\file", "C:/src", "src/***", "**"} {
		t.Run("invalid "+selector, func(t *testing.T) {
			task := validationFixturePlan().Tasks[0]
			task.Work.Context[0].Selectors.Paths = []string{selector}
			if err := ValidateWork(task); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

func TestValidateWorkBindsAcceptanceCommandToRecordedTool(t *testing.T) {
	for name, command := range map[string][]string{
		"PATH lookup":        {"ctest", "-R", "setter_invalid"},
		"relative path":      {"./ctest", "-R", "setter_invalid"},
		"unrecorded path":    {"/another/toolchain/ctest", "-R", "setter_invalid"},
		"unclean alias":      {"/recorded/toolchain/../toolchain/ctest", "-R", "setter_invalid"},
		"unrecorded shell":   {"/bin/sh", "-c", "/recorded/toolchain/ctest -R setter_invalid"},
		"different spelling": {"/recorded/toolchain/CTEST", "-R", "setter_invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			plan := validationFixturePlan()
			plan.Tasks[0].Work.Acceptance.Checks[0].Command = command
			if err := ValidateWork(plan.Tasks[0]); !errors.Is(err, ErrInvalid) {
				t.Fatalf("unbound command accepted: %v", err)
			}
			if _, err := ParsePlan(validationJSON(t, plan)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("planner accepted unbound command: %v", err)
			}
		})
	}
	t.Run("duplicate tool path", func(t *testing.T) {
		task := validationFixturePlan().Tasks[0]
		other := task.Work.Tools[0]
		other.Name = "second-tool-name"
		task.Work.Tools = append(task.Work.Tools, other)
		if err := ValidateWork(task); !errors.Is(err, ErrInvalid) {
			t.Fatalf("duplicate tool path accepted: %v", err)
		}
	})
	t.Run("conflicting identity at one path", func(t *testing.T) {
		task := validationFixturePlan().Tasks[0]
		other := task.Work.Tools[0]
		other.Name = "different-identity"
		other.SHA256 = strings.Repeat("b", 64)
		task.Work.Tools = append(task.Work.Tools, other)
		if err := ValidateWork(task); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ambiguous path identity accepted: %v", err)
		}
	})
	t.Run("multiple recorded executables", func(t *testing.T) {
		task := validationFixturePlan().Tasks[0]
		other := task.Work.Tools[0]
		other.Name, other.Path = "checker", "/recorded/toolchain/checker"
		task.Work.Tools = append(task.Work.Tools, other)
		check := task.Work.Acceptance.Checks[0]
		check.ID, check.Command = "second-check", []string{other.Path, "--check", ""}
		task.Work.Acceptance.Checks = append(task.Work.Acceptance.Checks, check)
		if err := ValidateWork(task); err != nil {
			t.Fatalf("recorded executable rejected: %v", err)
		}
	})
}

func TestValidateWorkToolVersionMatchesFileVerifier(t *testing.T) {
	for name, version := range map[string]string{
		"empty": "", "too long": strings.Repeat("v", 257),
		"leading whitespace": " version", "trailing whitespace": "version ",
		"newline": "version\nnext", "return": "version\rnext", "tab": "version\tnext",
		"NUL": "version\x00next", "invalid UTF8": string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			task := validationFixturePlan().Tasks[0]
			task.Work.Tools[0].Version = version
			if err := ValidateWork(task); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid tool version accepted: %v", err)
			}
		})
	}
	for _, version := range []string{"3.30.1", "3.30.1 (synthetic fixture)", strings.Repeat("v", 256)} {
		task := validationFixturePlan().Tasks[0]
		task.Work.Tools[0].Version = version
		if err := ValidateWork(task); err != nil {
			t.Fatalf("bounded tool version rejected: %v", err)
		}
	}
}

func TestParsePlanPreservesLegacyDomainContract(t *testing.T) {
	plan := validationFixturePlan()
	plan.Tasks[0].Work = nil
	originalHash := domain.SwarmHash(plan.Tasks[0])
	normalized, err := domain.NormalizeSwarmPlan(plan)
	if err != nil || originalHash != domain.SwarmHash(normalized.Tasks[0]) {
		t.Fatalf("legacy domain contract changed: %v", err)
	}
	raw := validationJSON(t, normalized)
	if strings.Contains(string(raw), `"work"`) {
		t.Fatal("legacy Work must remain omitted")
	}
	if _, err := ParsePlan(raw); !errors.Is(err, ErrInvalid) {
		t.Fatalf("legacy contract cannot prepare worker: %v", err)
	}
	if _, err := ParallelWaves(normalized); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wave planning must validate Work: %v", err)
	}
}

func TestParallelWavesDependenciesConflictsAndLimit(t *testing.T) {
	plan := validationFixturePlan()
	plan.Tasks = nil
	for _, id := range []string{"e", "d", "c", "b", "a"} {
		task := validationFixturePlan().Tasks[0]
		task.ID = id
		task.Resources[0].Key = "src/" + id
		plan.Tasks = append(plan.Tasks, task)
	}
	plan.MaxActive = 2
	plan.Tasks[4].Resources[0].Key = "src/a"
	plan.Tasks[3].Resources[0].Key = "src/a/child" // b conflicts with a.
	plan.Tasks[2].Dependencies = []domain.SwarmDependency{{TaskID: "a", Version: 1}}
	plan.Tasks[0].Dependencies = []domain.SwarmDependency{{TaskID: "c", Version: 1}, {TaskID: "d", Version: 1}}
	want := [][]string{{"a", "d"}, {"b", "c"}, {"e"}}
	before := domain.SwarmHash(plan)
	for i := 0; i < 5; i++ {
		got, err := ParallelWaves(plan)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, %v; want %v", got, err, want)
		}
	}
	if before != domain.SwarmHash(plan) {
		t.Fatal("planning mutated caller input")
	}
	plan.MaxActive = 1
	waves, err := ParallelWaves(plan)
	if err != nil || len(waves) != len(plan.Tasks) {
		t.Fatalf("concurrency limit not respected: %v %v", waves, err)
	}
}

func TestParallelWavesSemanticConflict(t *testing.T) {
	plan := validationFixturePlan()
	a := plan.Tasks[0]
	b := validationFixturePlan().Tasks[0]
	b.ID = "second"
	a.Resources = []domain.SwarmResource{{Namespace: "repo", Kind: "semantic", Key: "schema"}}
	b.Resources = append([]domain.SwarmResource(nil), a.Resources...)
	plan.Tasks = []domain.SwarmTaskContract{a, b}
	got, err := ParallelWaves(plan)
	if err != nil || len(got) != 2 {
		t.Fatalf("semantic conflicts must serialize: %v, %v", got, err)
	}
	plan.Tasks[1].Resources[0].Namespace = "another-repo"
	got, err = ParallelWaves(plan)
	if err != nil || len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("independent namespaces should share a wave: %v, %v", got, err)
	}
}

func TestParsePlanAndWavesRejectInvalidGraph(t *testing.T) {
	for _, failure := range []string{"cycle", "missing", "stale", "duplicate dependency", "duplicate task", "unknown dependency field"} {
		t.Run(failure, func(t *testing.T) {
			plan := validationFixturePlan()
			second := validationFixturePlan().Tasks[0]
			second.ID = "second"
			plan.Tasks = append(plan.Tasks, second)
			plan.Tasks[0].Dependencies = []domain.SwarmDependency{{TaskID: "second", Version: 1}}
			switch failure {
			case "cycle":
				plan.Tasks[1].Dependencies = []domain.SwarmDependency{{TaskID: "setter", Version: 1}}
			case "missing":
				plan.Tasks[0].Dependencies[0].TaskID = "absent"
			case "stale":
				plan.Tasks[0].Dependencies[0].Version = 2
			case "duplicate dependency":
				plan.Tasks[0].Dependencies = append(plan.Tasks[0].Dependencies, plan.Tasks[0].Dependencies[0])
			case "duplicate task":
				plan.Tasks[1].ID = "setter"
			}
			raw := validationJSON(t, plan)
			if failure == "unknown dependency field" {
				raw = []byte(strings.Replace(string(raw), `"taskId":"second"`, `"taskId":"second","latest":true`, 1))
			} else if _, err := ParallelWaves(plan); !errors.Is(err, ErrInvalid) {
				t.Fatalf("waves expected ErrInvalid, got %v", err)
			}
			if _, err := ParsePlan(raw); !errors.Is(err, ErrInvalid) {
				t.Fatalf("parse expected ErrInvalid, got %v", err)
			}
		})
	}
}
