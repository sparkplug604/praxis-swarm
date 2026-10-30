package planning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// The dimension filtering and specificity ordering adapt concepts from
// surlymochan/openswarm, b2a70d436ad72f0c5b036730fc86b749ddc68af7,
// bin/openswarm select_lessons/select_patterns (Apache-2.0). This implementation
// uses trusted approval, exact content hashes, normalized scopes, and complete
// packet byte accounting; it does not import upstream code or knowledge stores.
type contextCandidate struct {
	ref          domain.SwarmContextRef
	dimensions   int
	pathLength   int
	alternatives int
	applicable   bool
}

// SelectContext includes required inputs first, then whole optional inputs in
// deterministic specificity order. Every omission is part of the measured
// packet, including optional candidates that have not yet been admitted.
func SelectContext(ctx context.Context, refs []domain.SwarmContextRef, subject Subject, resolver ContextResolver, maxBytes int, measure Measure) (ContextSelection, error) {
	if ctx == nil || measure == nil || maxBytes <= 0 || maxBytes > MaxPacketBytes || len(refs) > 128 || len(subject.Paths) > 256 {
		return ContextSelection{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return ContextSelection{}, err
	}
	for _, value := range []string{subject.TaskType, subject.Backend} {
		if value != "" && (!domain.SwarmValidID(value) || !utf8.ValidString(value)) {
			return ContextSelection{}, ErrInvalid
		}
	}
	for _, value := range subject.Paths {
		if !planningRelativePath(value, false) {
			return ContextSelection{}, ErrInvalid
		}
	}

	candidates := make([]contextCandidate, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	hasRequired := false
	for _, ref := range refs {
		if seen[ref.ID] {
			return ContextSelection{}, fmt.Errorf("%w: duplicate context ID", ErrInvalid)
		}
		seen[ref.ID] = true
		if ref.Required && !domain.SwarmValidHash(ref.SHA256, false) {
			return ContextSelection{}, fmt.Errorf("%w: invalid required context digest", ErrContext)
		}
		if err := planningValidateContextRef(ref); err != nil {
			return ContextSelection{}, err
		}
		hasRequired = hasRequired || ref.Required
		candidates = append(candidates, contextMatch(ref, subject))
	}
	if len(refs) != 0 && resolver == nil {
		if hasRequired {
			return ContextSelection{}, ErrContext
		}
		return ContextSelection{}, ErrInvalid
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.ref.Required != b.ref.Required {
			return a.ref.Required
		}
		if !a.ref.Required {
			if a.dimensions != b.dimensions {
				return a.dimensions > b.dimensions
			}
			if a.pathLength != b.pathLength {
				return a.pathLength > b.pathLength
			}
			if a.alternatives != b.alternatives {
				return a.alternatives < b.alternatives
			}
		}
		return a.ref.ID < b.ref.ID
	})

	selection := ContextSelection{Included: []IncludedContext{}, Omitted: []OmittedContext{}}
	optional := make([]IncludedContext, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return ContextSelection{}, err
		}
		reason := "not_applicable"
		var included IncludedContext
		if candidate.applicable {
			approved, err := resolver.Resolve(ctx, contextSubjectCopy(subject), contextRefCopy(candidate.ref))
			if ctx.Err() != nil {
				return ContextSelection{}, ctx.Err()
			}
			reason = contextApprovalReason(candidate.ref, approved, err)
			if reason == "" {
				included = IncludedContext{ID: candidate.ref.ID, Kind: candidate.ref.Kind, SHA256: approved.SHA256, DecisionID: approved.DecisionID, DecisionVersion: approved.DecisionVersion, Body: approved.Body}
			}
		}
		if reason != "" {
			if candidate.ref.Required {
				return ContextSelection{}, fmt.Errorf("%w: context %q %s", ErrContext, candidate.ref.ID, reason)
			}
			selection.Omitted = append(selection.Omitted, OmittedContext{ID: candidate.ref.ID, Reason: reason})
			continue
		}
		if candidate.ref.Required {
			selection.Included = append(selection.Included, included)
		} else {
			optional = append(optional, included)
			selection.Omitted = append(selection.Omitted, OmittedContext{ID: candidate.ref.ID, Reason: "budget"})
		}
	}
	sort.Slice(selection.Omitted, func(i, j int) bool { return selection.Omitted[i].ID < selection.Omitted[j].ID })
	bytes, err := contextMeasure(selection.Included, selection.Omitted, measure)
	if err != nil {
		return ContextSelection{}, err
	}
	if bytes > maxBytes {
		return ContextSelection{}, ErrBudget
	}
	selection.Bytes = bytes
	for _, candidate := range optional {
		if err := ctx.Err(); err != nil {
			return ContextSelection{}, err
		}
		included := append(append([]IncludedContext{}, selection.Included...), candidate)
		omitted := make([]OmittedContext, 0, len(selection.Omitted)-1)
		for _, item := range selection.Omitted {
			if item.ID != candidate.ID {
				omitted = append(omitted, item)
			}
		}
		bytes, err := contextMeasure(included, omitted, measure)
		if err != nil {
			return ContextSelection{}, err
		}
		if bytes <= maxBytes {
			selection = ContextSelection{Included: included, Omitted: omitted, Bytes: bytes}
		}
	}
	return selection, nil
}

func contextMatch(ref domain.SwarmContextRef, subject Subject) contextCandidate {
	out := contextCandidate{ref: contextRefCopy(ref), applicable: true}
	for _, dimension := range []struct {
		values []string
		value  string
	}{{ref.Selectors.TaskTypes, subject.TaskType}, {ref.Selectors.Backends, subject.Backend}} {
		out.alternatives += len(dimension.values)
		if len(dimension.values) == 0 {
			continue
		}
		out.dimensions++
		matched := false
		for _, value := range dimension.values {
			matched = matched || value == dimension.value
		}
		out.applicable = out.applicable && matched
	}
	out.alternatives += len(ref.Selectors.Paths)
	if len(ref.Selectors.Paths) != 0 {
		out.dimensions++
		matched := false
		for _, selector := range ref.Selectors.Paths {
			literal := strings.TrimSuffix(selector, "/**")
			for _, candidate := range subject.Paths {
				if literal == "." || candidate == literal || strings.HasPrefix(candidate, literal+"/") {
					matched = true
					out.pathLength = max(out.pathLength, len(literal))
				}
			}
		}
		out.applicable = out.applicable && matched
	}
	return out
}

func contextApprovalReason(ref domain.SwarmContextRef, approved ApprovedContext, err error) string {
	if err != nil {
		if errors.Is(err, ErrContext) {
			return "unapproved"
		}
		return "unavailable"
	}
	if approved.ID != ref.ID || !domain.SwarmValidID(approved.DecisionID) || !utf8.ValidString(approved.DecisionID) || approved.DecisionVersion <= 0 || len(approved.Body) == 0 || len(approved.Body) > MaxPacketBytes || !utf8.ValidString(approved.Body) {
		return "unapproved"
	}
	if approved.SHA256 != ref.SHA256 {
		return "hash_mismatch"
	}
	sum := sha256.Sum256([]byte(approved.Body))
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return "hash_mismatch"
	}
	return ""
}

func contextMeasure(included []IncludedContext, omitted []OmittedContext, measure Measure) (int, error) {
	bytes, err := measure(append([]IncludedContext{}, included...), append([]OmittedContext{}, omitted...))
	if err != nil {
		return 0, err
	}
	if bytes < 0 {
		return 0, fmt.Errorf("%w: negative packet measurement", ErrInvalid)
	}
	return bytes, nil
}

func contextRefCopy(ref domain.SwarmContextRef) domain.SwarmContextRef {
	ref.Selectors.TaskTypes = append([]string{}, ref.Selectors.TaskTypes...)
	ref.Selectors.Backends = append([]string{}, ref.Selectors.Backends...)
	ref.Selectors.Paths = append([]string{}, ref.Selectors.Paths...)
	return ref
}

func contextSubjectCopy(subject Subject) Subject {
	subject.Paths = append([]string{}, subject.Paths...)
	return subject
}
