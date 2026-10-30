package domain

// SwarmWorkSpec is data from the validated planner, not authority to dispatch.
// It is embedded in the immutable task contract; changing it requires a new
// task version. Context approval is supplied separately by a trusted resolver.
type SwarmWorkSpec struct {
	Version          int64               `json:"version"`
	TaskType         string              `json:"taskType"`
	ContextByteLimit int                 `json:"contextByteLimit"`
	NonGoals         []string            `json:"nonGoals"`
	Interfaces       []SwarmInterface    `json:"interfaces"`
	Errors           []SwarmErrorRule    `json:"errors"`
	Acceptance       SwarmAcceptance     `json:"acceptance"`
	Context          []SwarmContextRef   `json:"context"`
	Tools            []SwarmToolIdentity `json:"tools"`
	ProgressPath     string              `json:"progressPath"`
	OnComplete       string              `json:"onComplete"` // submit_and_stop
	OnBlocked        string              `json:"onBlocked"`  // report_and_stop
}

// SwarmInterface binds an owned interface contract to its exact error rules.
type SwarmInterface struct {
	ID           string   `json:"id"`
	Contract     string   `json:"contract"`
	Ownership    string   `json:"ownership"`
	ErrorRuleIDs []string `json:"errorRuleIds"`
}

// SwarmErrorRule specifies an exact error signal and its mutation consequences.
type SwarmErrorRule struct {
	ID          string `json:"id"`
	When        string `json:"when"`
	Signal      string `json:"signal"`      // throw, return_error, reject
	Value       string `json:"value"`       // exact exception/error/status
	Destination string `json:"destination"` // unchanged or specified
	Effect      string `json:"effect"`      // explicit rollback/partial mutation semantics
}

// SwarmAcceptance names the oracle and checks required for independent acceptance.
type SwarmAcceptance struct {
	OracleID string                 `json:"oracleId"`
	Checks   []SwarmAcceptanceCheck `json:"checks"`
}

// SwarmAcceptanceCheck describes a bounded command and its expected outcome.
type SwarmAcceptanceCheck struct {
	ID               string   `json:"id"`
	Command          []string `json:"command"` // argv, never executed by composition
	ExpectedExitCode int      `json:"expectedExitCode"`
	ExpectedResult   string   `json:"expectedResult"`
	ErrorRuleID      string   `json:"errorRuleId,omitempty"`
}

// SwarmToolIdentity records a canonical executable path, content hash and supplied version.
type SwarmToolIdentity struct {
	Name    string `json:"name"`
	Path    string `json:"path"` // absolute canonical executable/interpreter path
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// SwarmContextRef binds required or optional context to a hash and applicability selectors.
type SwarmContextRef struct {
	ID        string                `json:"id"`
	Kind      string                `json:"kind"` // spec, source_manifest, reference, lesson, pattern
	SHA256    string                `json:"sha256"`
	Required  bool                  `json:"required"`
	Selectors SwarmContextSelectors `json:"selectors"`
}

// SwarmContextSelectors with empty dimensions match all; dimensions AND together and alternatives OR.
// Paths are repository-relative logical scopes, never filesystem permissions.
type SwarmContextSelectors struct {
	TaskTypes []string `json:"taskTypes"`
	Backends  []string `json:"backends"`
	Paths     []string `json:"paths"`
}
