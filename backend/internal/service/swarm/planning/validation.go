package planning

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ParsePlan accepts one bounded, strictly shaped planner document. It validates
// data only: commands in acceptance checks never execute during planning.
func ParsePlan(raw []byte) (domain.SwarmPlan, error) {
	if len(raw) == 0 || len(raw) > MaxPlannerBytes || !utf8.Valid(raw) {
		return domain.SwarmPlan{}, planningInvalid("planner document size or encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := planningScanJSON(decoder, reflect.TypeOf(domain.SwarmPlan{}), 0); err != nil {
		return domain.SwarmPlan{}, planningInvalid("planner JSON: %v", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return domain.SwarmPlan{}, planningInvalid("trailing planner JSON")
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var plan domain.SwarmPlan
	if err := decoder.Decode(&plan); err != nil {
		return domain.SwarmPlan{}, planningInvalid("planner JSON: %v", err)
	}
	return planningValidatePlan(plan)
}

// Unlike encoding/json alone, this scan rejects duplicate keys, case aliases,
// and null scalar coercions before any information can be lost during decode.
func planningScanJSON(decoder *json.Decoder, schema reflect.Type, depth int) error {
	if depth > 64 {
		return fmt.Errorf("maximum JSON depth exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if schema.Kind() != reflect.Pointer && schema.Kind() != reflect.Slice {
			return fmt.Errorf("null is not valid for %s", schema)
		}
		return nil
	}
	for schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil // Decode checks scalar types and integer ranges.
	}
	switch delimiter {
	case '{':
		if schema.Kind() != reflect.Struct {
			return fmt.Errorf("unexpected object for %s", schema)
		}
		fields := make(map[string]reflect.Type, schema.NumField())
		for i := 0; i < schema.NumField(); i++ {
			field := schema.Field(i)
			fields[strings.Split(field.Tag.Get("json"), ",")[0]] = field.Type
		}
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid object key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate field %q", key)
			}
			seen[key] = true
			field, ok := fields[key]
			if !ok {
				return fmt.Errorf("unknown field %q", key)
			}
			if err := planningScanJSON(decoder, field, depth+1); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
		// Zero is meaningful, so an omitted expectation must not silently become
		// a declaration that a check should succeed.
		if schema == reflect.TypeOf(domain.SwarmAcceptanceCheck{}) && !seen["expectedExitCode"] {
			return fmt.Errorf("missing expectedExitCode")
		}
	case '[':
		if schema.Kind() != reflect.Slice {
			return fmt.Errorf("unexpected array for %s", schema)
		}
		for decoder.More() {
			if err := planningScanJSON(decoder, schema.Elem(), depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func planningValidatePlan(plan domain.SwarmPlan) (domain.SwarmPlan, error) {
	raw, err := json.Marshal(plan)
	if err != nil || len(raw) > MaxPlannerBytes {
		return domain.SwarmPlan{}, planningInvalid("planner document exceeds size limit")
	}
	normalized, err := domain.NormalizeSwarmPlan(plan)
	if err != nil {
		return domain.SwarmPlan{}, planningInvalid("%v", err)
	}
	if !planningText(normalized.Objective, 16384) {
		return domain.SwarmPlan{}, planningInvalid("plan objective")
	}
	for _, task := range normalized.Tasks {
		if !planningText(task.Objective, 16384) || !planningOptionalText(task.Agent.Effort, 256) {
			return domain.SwarmPlan{}, planningInvalid("task %q objective or effort", task.ID)
		}
		if err := ValidateWork(task); err != nil {
			return domain.SwarmPlan{}, err
		}
	}
	return normalized, nil
}

// ValidateWork requires a complete versioned assignment contract. Legacy domain
// contracts may omit Work for hash compatibility, but cannot prepare a worker.
func ValidateWork(task domain.SwarmTaskContract) error {
	work := task.Work
	bad := func(format string, args ...any) error {
		return planningInvalid("task %q: %s", task.ID, fmt.Sprintf(format, args...))
	}
	if work == nil || work.Version != 1 || !planningID(work.TaskType) {
		return bad("work version 1 and taskType required")
	}
	if work.ContextByteLimit < 1 || work.ContextByteLimit > MaxPacketBytes {
		return bad("contextByteLimit must be 1..%d", MaxPacketBytes)
	}
	if err := planningStrings(work.NonGoals, 1, 4096); err != nil {
		return bad("nonGoals: %v", err)
	}
	if len(work.Interfaces) == 0 || len(work.Interfaces) > 128 || len(work.Errors) == 0 || len(work.Errors) > 128 {
		return bad("interfaces and errors must each contain 1..128 entries")
	}
	rules := map[string]bool{}
	for _, rule := range work.Errors {
		if !planningID(rule.ID) || rules[rule.ID] || !planningText(rule.When, 4096) ||
			!planningText(rule.Value, 1024) || !planningText(rule.Effect, 4096) {
			return bad("incomplete or duplicate error rule %q", rule.ID)
		}
		switch rule.Signal {
		case "throw", "return_error", "reject":
		default:
			return bad("error rule %q must specify throw, return_error, or reject", rule.ID)
		}
		if rule.Destination != "unchanged" && rule.Destination != "specified" {
			return bad("error rule %q must specify destination unchanged or specified", rule.ID)
		}
		switch strings.ToLower(strings.TrimSpace(rule.Value)) {
		case "error", "exception", "failure", "unknown", "unspecified", "tbd", "todo", "implementation-defined":
			return bad("error rule %q requires an exact error value", rule.ID)
		}
		rules[rule.ID] = true
	}
	interfaces := map[string]bool{}
	referenced := map[string]bool{}
	for _, contract := range work.Interfaces {
		if !planningID(contract.ID) || interfaces[contract.ID] || !planningText(contract.Contract, 16384) || !planningText(contract.Ownership, 4096) {
			return bad("incomplete or duplicate interface %q", contract.ID)
		}
		interfaces[contract.ID] = true
		if err := planningStrings(contract.ErrorRuleIDs, 1, 256); err != nil {
			return bad("interface %q errorRuleIds: %v", contract.ID, err)
		}
		for _, id := range contract.ErrorRuleIDs {
			if !rules[id] {
				return bad("interface %q references unknown error rule %q", contract.ID, id)
			}
			referenced[id] = true
		}
	}
	acceptance := work.Acceptance
	if !planningID(acceptance.OracleID) || len(acceptance.Checks) == 0 || len(acceptance.Checks) > 128 {
		return bad("acceptance requires oracleId and 1..128 checks")
	}
	checks, tested := map[string]bool{}, map[string]bool{}
	for _, check := range acceptance.Checks {
		if !planningID(check.ID) || checks[check.ID] || check.ExpectedExitCode < 0 || check.ExpectedExitCode > 255 || !planningText(check.ExpectedResult, 16384) {
			return bad("incomplete or duplicate acceptance check %q", check.ID)
		}
		checks[check.ID] = true
		if len(check.Command) == 0 || len(check.Command) > 128 || !planningText(check.Command[0], 4096) {
			return bad("acceptance check %q requires bounded argv", check.ID)
		}
		for _, arg := range check.Command {
			if !planningOptionalText(arg, 4096) {
				return bad("acceptance check %q contains invalid argv", check.ID)
			}
		}
		if check.ErrorRuleID != "" {
			if !rules[check.ErrorRuleID] {
				return bad("acceptance check %q references unknown error rule", check.ID)
			}
			tested[check.ErrorRuleID] = true
		}
	}
	for _, rule := range work.Errors {
		if !referenced[rule.ID] || !tested[rule.ID] {
			return bad("error rule %q must be named by an interface and tested", rule.ID)
		}
	}
	if len(work.Context) == 0 || len(work.Context) > 128 {
		return bad("context must contain 1..128 references")
	}
	contexts := map[string]bool{}
	requiredSpec, requiredManifests := false, 0
	for _, ref := range work.Context {
		if err := planningValidateContextRef(ref); err != nil {
			return bad("context: %v", err)
		}
		if contexts[ref.ID] {
			return bad("duplicate context %q", ref.ID)
		}
		contexts[ref.ID] = true
		if ref.Required && ref.Kind == "spec" {
			requiredSpec = true
		}
		if ref.Required && ref.Kind == "source_manifest" {
			requiredManifests++
		}
	}
	if !requiredSpec || requiredManifests != 1 {
		return bad("required spec and exactly one required source_manifest are necessary")
	}
	if len(work.Tools) == 0 || len(work.Tools) > 32 {
		return bad("tools must contain 1..32 recorded identities")
	}
	tools := map[string]bool{}
	toolPaths := map[string]bool{}
	for _, tool := range work.Tools {
		if !planningID(tool.Name) || tools[tool.Name] || toolPaths[tool.Path] || !planningText(tool.Path, 4096) ||
			!filepath.IsAbs(tool.Path) || filepath.Clean(tool.Path) != tool.Path || strings.ContainsAny(tool.Path, "\\\r\n\t") ||
			!domain.SwarmValidHash(tool.SHA256, false) || !planningID(tool.Version) {
			return bad("invalid or duplicate tool identity %q", tool.Name)
		}
		// Syntax is checked here; ToolVerifier checks the real canonical binary.
		tools[tool.Name] = true
		toolPaths[tool.Path] = true
	}
	for _, check := range acceptance.Checks {
		// argv[0] must use the same exact path whose identity is recorded and
		// verified. PATH lookup or an unrecorded shell would break that binding.
		if !toolPaths[check.Command[0]] {
			return bad("acceptance check %q executable must match a recorded tool path", check.ID)
		}
	}
	if !planningRelativePath(work.ProgressPath, false) || work.ProgressPath == "." {
		return bad("progressPath must be a normalized relative file path")
	}
	if work.OnComplete != "submit_and_stop" || work.OnBlocked != "report_and_stop" {
		return bad("onComplete must be submit_and_stop and onBlocked must be report_and_stop")
	}
	return nil
}

func planningValidateContextRef(ref domain.SwarmContextRef) error {
	if !planningID(ref.ID) || !domain.SwarmValidHash(ref.SHA256, false) {
		return planningInvalid("invalid context identity %q", ref.ID)
	}
	switch ref.Kind {
	case "spec", "source_manifest", "reference", "lesson", "pattern":
	default:
		return planningInvalid("invalid context kind %q", ref.Kind)
	}
	for _, dimension := range [][]string{ref.Selectors.TaskTypes, ref.Selectors.Backends} {
		if err := planningStrings(dimension, 0, 256); err != nil {
			return planningInvalid("context selectors: %v", err)
		}
		for _, item := range dimension {
			if !planningID(item) {
				return planningInvalid("invalid context selector %q", item)
			}
		}
	}
	if err := planningStrings(ref.Selectors.Paths, 0, 4096); err != nil {
		return planningInvalid("context paths: %v", err)
	}
	for _, selector := range ref.Selectors.Paths {
		if !planningRelativePath(selector, true) {
			return planningInvalid("invalid context path selector %q", selector)
		}
	}
	return nil
}

func planningRelativePath(value string, allowTree bool) bool {
	if !planningText(value, 4096) || strings.TrimSpace(value) != value || path.Clean(value) != value {
		return false
	}
	if allowTree && strings.HasSuffix(value, "/**") {
		value = strings.TrimSuffix(value, "/**")
	}
	return value != "" && !path.IsAbs(value) && path.Clean(value) == value && value != ".." &&
		!strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\*?[]:\n\r\t")
}

func planningID(value string) bool {
	return utf8.ValidString(value) && domain.SwarmValidID(value)
}

func planningText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && planningOptionalText(value, limit)
}

func planningOptionalText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}

func planningStrings(values []string, minimum, textLimit int) error {
	const maximum = 128
	if len(values) < minimum || len(values) > maximum {
		return fmt.Errorf("requires %d..%d entries", minimum, maximum)
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !planningText(value, textLimit) || seen[value] {
			return fmt.Errorf("invalid or duplicate entry")
		}
		seen[value] = true
	}
	return nil
}

func planningInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ParallelWaves gives deterministic planning advice, not admission or dispatch.
// Dependencies finish in earlier waves; conflicting resources never share one.
func ParallelWaves(plan domain.SwarmPlan) ([][]string, error) {
	normalized, err := planningValidatePlan(plan)
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	waves := [][]string{}
	for len(done) < len(normalized.Tasks) {
		wave := []string{}
		resources := []domain.SwarmResource{}
		for _, task := range normalized.Tasks {
			if done[task.ID] || int64(len(wave)) >= normalized.MaxActive {
				continue
			}
			ready := true
			for _, dependency := range task.Dependencies {
				if !done[dependency.TaskID] {
					ready = false
					break
				}
			}
			for _, resource := range task.Resources {
				for _, held := range resources {
					if domain.SwarmResourcesConflict(resource, held) {
						ready = false
						break
					}
				}
			}
			if ready {
				wave = append(wave, task.ID)
				resources = append(resources, task.Resources...)
			}
		}
		if len(wave) == 0 {
			return nil, planningInvalid("no dependency-ready task")
		}
		for _, id := range wave {
			done[id] = true
		}
		waves = append(waves, wave)
	}
	return waves, nil
}
