package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// SwarmBriefDraft records supplied project decisions, not proof of human approval.
type SwarmBriefDraft struct {
	Goal        string                   `json:"goal"`
	Audience    string                   `json:"audience"`
	Outcomes    []string                 `json:"outcomes"`
	Acceptance  []string                 `json:"acceptance"`
	Constraints []string                 `json:"constraints"`
	Assumptions []string                 `json:"assumptions"`
	Questions   []SwarmDiscoveryQuestion `json:"questions"`
}

// SwarmDiscoveryQuestion describes versioned project discovery data.
type SwarmDiscoveryQuestion struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Required bool   `json:"required"`
	Answer   string `json:"answer"`
}

// SwarmDiscoveryUpdate describes versioned project discovery data.
type SwarmDiscoveryUpdate struct {
	ExpectedRevision int64           `json:"expectedRevision"`
	Brief            SwarmBriefDraft `json:"brief"`
}

// SwarmBriefRef describes versioned project discovery data.
type SwarmBriefRef struct {
	Revision int64  `json:"revision"`
	SHA256   string `json:"sha256"`
}

// SwarmDiscoveryBrief describes versioned project discovery data.
type SwarmDiscoveryBrief struct {
	ProjectID ProjectID       `json:"projectId"`
	Revision  int64           `json:"revision"`
	SHA256    string          `json:"sha256"`
	Brief     SwarmBriefDraft `json:"brief"`
	Status    string          `json:"status"`
	Missing   []string        `json:"missing"`
	CreatedAt time.Time       `json:"createdAt"`
}

// NewSwarmDiscoveryBrief bounds input and derives readiness and content identity.
func NewSwarmDiscoveryBrief(project ProjectID, revision int64, draft SwarmBriefDraft, now time.Time) (SwarmDiscoveryBrief, error) {
	bad := func() (SwarmDiscoveryBrief, error) {
		return SwarmDiscoveryBrief{}, fmt.Errorf("%w: invalid discovery brief", ErrSwarmInvalid)
	}
	if !SwarmValidID(string(project)) || revision < 1 || now.IsZero() || now.Year() < 1 || now.Year() > 9999 {
		return bad()
	}
	raw, err := json.Marshal(draft)
	if err != nil || len(raw) > 64*1024 {
		return bad()
	}
	// Clone slices so neither callers nor returned revisions alias input state.
	text := func(v string) bool { return utf8.ValidString(v) && !strings.ContainsRune(v, 0) && len(v) <= 8192 }
	if !text(draft.Goal) || !text(draft.Audience) {
		return bad()
	}
	for _, list := range [][]string{draft.Outcomes, draft.Acceptance, draft.Constraints, draft.Assumptions} {
		if len(list) > 32 {
			return bad()
		}
		for _, v := range list {
			if !text(v) || strings.TrimSpace(v) == "" {
				return bad()
			}
		}
	}
	if len(draft.Questions) > 16 {
		return bad()
	}
	seen := map[string]bool{}
	missing := []string{}
	for _, item := range []struct{ name, value string }{{"goal", draft.Goal}, {"audience", draft.Audience}} {
		if strings.TrimSpace(item.value) == "" {
			missing = append(missing, item.name)
		}
	}
	if len(draft.Outcomes) == 0 {
		missing = append(missing, "outcomes")
	}
	if len(draft.Acceptance) == 0 {
		missing = append(missing, "acceptance")
	}
	for _, q := range draft.Questions {
		if !SwarmValidID(q.ID) || strings.ContainsAny(q.ID, "/\\?#") || seen[q.ID] || !text(q.Question) || strings.TrimSpace(q.Question) == "" || !text(q.Answer) {
			return bad()
		}
		seen[q.ID] = true
		if q.Required && strings.TrimSpace(q.Answer) == "" {
			missing = append(missing, "question:"+q.ID)
		}
	}
	var clone SwarmBriefDraft
	if err := json.Unmarshal(raw, &clone); err != nil {
		return bad()
	}
	draft = clone
	status := "ready"
	if len(missing) > 0 {
		status = "needs_input"
	}
	return SwarmDiscoveryBrief{ProjectID: project, Revision: revision, SHA256: SwarmHash(draft), Brief: draft, Status: status, Missing: missing, CreatedAt: now.UTC()}, nil
}
