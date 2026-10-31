package domain

import (
	"strings"
	"testing"
	"time"
)

func TestDiscoveryReadinessAndIsolation(t *testing.T) {
	draft := SwarmBriefDraft{Goal: "Build a dashboard", Audience: "Operators", Outcomes: []string{"Inspect records"}, Acceptance: []string{"Filtering changes displayed records"}, Questions: []SwarmDiscoveryQuestion{{ID: "data", Question: "Which data source?", Required: true}}}
	b, err := NewSwarmDiscoveryBrief("project", 1, draft, time.Now())
	if err != nil || b.Status != "needs_input" || len(b.Missing) != 1 || b.Missing[0] != "question:data" {
		t.Fatalf("%+v %v", b, err)
	}
	draft.Questions[0].Answer = "Use the existing API"
	ready, err := NewSwarmDiscoveryBrief("project", 2, draft, time.Now())
	if err != nil || ready.Status != "ready" || ready.SHA256 == b.SHA256 {
		t.Fatalf("%+v %v", ready, err)
	}
	draft.Outcomes[0] = "Mutated"
	draft.Questions[0].Answer = "Mutated"
	if ready.Brief.Outcomes[0] != "Inspect records" || ready.Brief.Questions[0].Answer != "Use the existing API" {
		t.Fatal("brief aliases supplied data")
	}
	empty, err := NewSwarmDiscoveryBrief("project", 1, SwarmBriefDraft{}, time.Now())
	if err != nil || empty.Status != "needs_input" || len(empty.Missing) != 4 {
		t.Fatal(empty, err)
	}
}

func TestDiscoveryRejectsMalformedOrUnboundedData(t *testing.T) {
	for _, draft := range []SwarmBriefDraft{
		{Goal: strings.Repeat("x", 8193)},
		{Goal: string([]byte{255})},
		{Audience: "bad\x00value"},
		{Acceptance: []string{" "}},
		{Questions: []SwarmDiscoveryQuestion{{ID: "same", Question: "One?"}, {ID: "same", Question: "Two?"}}},
		{Questions: []SwarmDiscoveryQuestion{{ID: "../bad", Question: "Bad?"}}},
	} {
		if _, err := NewSwarmDiscoveryBrief("project", 1, draft, time.Now()); err == nil {
			t.Fatalf("accepted %+v", draft)
		}
	}
}
