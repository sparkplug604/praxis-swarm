package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func swarmTestPlan() SwarmPlan {
	task := func(id string, version int64) SwarmTaskContract {
		return SwarmTaskContract{
			ID: id, Version: version, Objective: "Implement " + id,
			OracleHash: strings.Repeat("b", 64),
			Agent:      SwarmSelection{Harness: HarnessCodex, Model: "selected-model", Effort: "high"},
			Resources: []SwarmResource{
				{Namespace: "repo", Kind: "semantic", Key: "schema"},
				{Namespace: "repo", Kind: "path", Key: "src/" + id},
			},
			ReserveTokens: 100,
		}
	}
	p := SwarmPlan{
		ProtocolVersion: 1, Version: 1, ProjectID: "project", SourceCommit: strings.Repeat("a", 40),
		Objective: "Build a reviewed change", BudgetBucket: "account", TokenLimit: 1000, MaxActive: 2,
		Tasks: []SwarmTaskContract{task("gamma", 1), task("beta", 3), task("alpha", 2)},
	}
	p.Tasks[0].Dependencies = []SwarmDependency{{TaskID: "beta", Version: 3}, {TaskID: "alpha", Version: 2}}
	return p
}

func TestNormalizeSwarmPlanIndependentAndDeterministic(t *testing.T) {
	input := swarmTestPlan()
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := NormalizeSwarmPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	permuted := swarmTestPlan()
	slices.Reverse(permuted.Tasks)
	for i := range permuted.Tasks {
		slices.Reverse(permuted.Tasks[i].Resources)
		slices.Reverse(permuted.Tasks[i].Dependencies)
	}
	again, err := NormalizeSwarmPlan(permuted)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalized, again) || SwarmHash(normalized) != SwarmHash(again) {
		t.Fatal("equivalent graphs produce different normalized contracts or hashes")
	}
	if got := []string{normalized.Tasks[0].ID, normalized.Tasks[1].ID, normalized.Tasks[2].ID}; !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("task order = %v", got)
	}
	if got := normalized.Tasks[2].Dependencies; !reflect.DeepEqual(got, []SwarmDependency{{TaskID: "alpha", Version: 2}, {TaskID: "beta", Version: 3}}) {
		t.Fatalf("dependency order = %v", got)
	}
	if got := normalized.Tasks[0].Resources[0].Kind; got != "path" {
		t.Fatalf("first resource kind = %q, want path", got)
	}
	// Mutating every nested slice in the returned contract must not change the
	// caller's draft or invalidate another component's snapshot of that draft.
	normalized.Tasks[2].Dependencies[0].Version++
	normalized.Tasks[0].Resources[0].Key = "changed"
	normalized.Tasks[0].Objective = "changed"
	after, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("normalization or a returned-contract mutation changed the caller's plan")
	}
}

func TestNormalizeSwarmPlanRejectsInvalidGraphsWithoutMutatingInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*SwarmPlan)
	}{
		{"cycle", func(p *SwarmPlan) { p.Tasks[1].Dependencies = []SwarmDependency{{TaskID: "gamma", Version: 1}} }},
		{"self dependency", func(p *SwarmPlan) { p.Tasks[1].Dependencies = []SwarmDependency{{TaskID: "beta", Version: 3}} }},
		{"missing dependency", func(p *SwarmPlan) { p.Tasks[0].Dependencies[0].TaskID = "missing" }},
		{"stale dependency", func(p *SwarmPlan) { p.Tasks[0].Dependencies[0].Version = 2 }},
		{"duplicate dependency", func(p *SwarmPlan) {
			p.Tasks[0].Dependencies = append(p.Tasks[0].Dependencies, p.Tasks[0].Dependencies[0])
		}},
		{"duplicate task", func(p *SwarmPlan) { p.Tasks[1].ID = p.Tasks[2].ID }},
		{"duplicate resource", func(p *SwarmPlan) { p.Tasks[0].Resources = append(p.Tasks[0].Resources, p.Tasks[0].Resources[0]) }},
		{"unknown resource kind", func(p *SwarmPlan) { p.Tasks[0].Resources[0].Kind = "glob" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := swarmTestPlan()
			tc.change(&input)
			before, _ := json.Marshal(input)
			_, err := NormalizeSwarmPlan(input)
			if !errors.Is(err, ErrSwarmInvalid) {
				t.Fatalf("error = %v, want ErrSwarmInvalid", err)
			}
			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("rejected plan was modified")
			}
		})
	}
}

func TestNormalizeSwarmPlanRejectsAmbiguousPathScopes(t *testing.T) {
	for _, key := range []string{"/absolute", "..", "../outside", "src/../other", "src//file", "src/", "src\\file", "src/*", "src/?", "src/[ab]", "C:/src"} {
		t.Run(key, func(t *testing.T) {
			input := swarmTestPlan()
			input.Tasks[0].Resources = []SwarmResource{{Namespace: "repo", Kind: "path", Key: key}}
			if _, err := NormalizeSwarmPlan(input); !errors.Is(err, ErrSwarmInvalid) {
				t.Fatalf("path %q: error = %v, want ErrSwarmInvalid", key, err)
			}
		})
	}
	input := swarmTestPlan()
	input.Tasks[0].Resources = []SwarmResource{{Namespace: "repo", Kind: "path", Key: "."}}
	if _, err := NormalizeSwarmPlan(input); err != nil {
		t.Fatalf("explicit repository root must be a valid reservation: %v", err)
	}
}

func TestSwarmResourcesConflict(t *testing.T) {
	resource := func(kind, key string) SwarmResource {
		return SwarmResource{Namespace: "repo", Kind: kind, Key: key}
	}
	for _, tc := range []struct {
		name string
		a, b SwarmResource
		want bool
	}{
		{"same file", resource("path", "src/a.go"), resource("path", "src/a.go"), true},
		{"directory and descendant", resource("path", "src"), resource("path", "src/nested/a.go"), true},
		{"repository root", resource("path", "."), resource("path", "src/a.go"), true},
		{"sibling files", resource("path", "src/a.go"), resource("path", "src/b.go"), false},
		{"prefix without separator", resource("path", "src"), resource("path", "src-other/a.go"), false},
		{"separate repositories", resource("path", "src"), SwarmResource{Namespace: "other", Kind: "path", Key: "src"}, false},
		{"shared invariant", resource("semantic", "schema"), resource("semantic", "schema"), true},
		{"distinct invariant", resource("semantic", "schema"), resource("semantic", "schema/version"), false},
		{"separate kinds", resource("semantic", "schema"), resource("path", "schema"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SwarmResourcesConflict(tc.a, tc.b); got != tc.want {
				t.Fatalf("conflict(%+v, %+v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			if got := SwarmResourcesConflict(tc.b, tc.a); got != tc.want {
				t.Fatalf("reverse conflict = %v, want %v", got, tc.want)
			}
		})
	}
}
