package planning

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLeadProposalStrictParsing(t *testing.T) {
	plan := validationFixturePlan()
	p := LeadProposal{Tasks: []ProposalTask{{Acceptance: plan.Tasks[0].Work.Acceptance}}}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	base := string(raw)
	for _, bad := range []string{
		strings.Replace(base, `"projectId":`, `"projectId":"one","projectId":`, 1),
		strings.Replace(base, `"projectId":`, `"ProjectId":`, 1),
		strings.Replace(base, `"expectedExitCode":0,`, ``, 1),
		strings.Replace(base, `"expectedExitCode":0`, `"expectedExitCode":null`, 1),
		base + ` {}`, strings.Replace(base, `"objective":`, `"unexpected":`, 1),
	} {
		if _, err := ParseProposal([]byte(bad)); err == nil {
			t.Fatalf("accepted malformed proposal: %s", bad)
		}
	}
	if _, err := ParseProposal(raw); err != nil {
		t.Fatal(err)
	}
}
