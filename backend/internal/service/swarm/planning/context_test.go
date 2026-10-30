package planning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type contextTestResolver func(context.Context, Subject, domain.SwarmContextRef) (ApprovedContext, error)

func (f contextTestResolver) Resolve(ctx context.Context, subject Subject, ref domain.SwarmContextRef) (ApprovedContext, error) {
	return f(ctx, subject, ref)
}

func contextTestDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// These approval IDs exist only in synthetic fixtures. No real approval store,
// model session, Praxis data, or external runtime participates in these tests.
func contextTestInput(id, body string, required bool) (domain.SwarmContextRef, ApprovedContext) {
	return domain.SwarmContextRef{ID: id, Kind: "reference", SHA256: contextTestDigest(body), Required: required}, ApprovedContext{ID: id, SHA256: contextTestDigest(body), DecisionID: "synthetic-test-approval/" + id, DecisionVersion: 1, Body: body}
}

func contextTestMeasure(included []IncludedContext, omitted []OmittedContext) (int, error) {
	data, err := json.Marshal(struct {
		Entry    string            `json:"entry"`
		Included []IncludedContext `json:"included"`
		Omitted  []OmittedContext  `json:"omitted"`
	}{"Synthetic complete packet fixture, including framing and receipts.", included, omitted})
	return len(data), err
}

func contextTestIDs(selection ContextSelection) []string {
	ids := make([]string, 0, len(selection.Included))
	for _, included := range selection.Included {
		ids = append(ids, included.ID)
	}
	return ids
}

func TestSelectContextRequiredFailures(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.SwarmContextRef, *ApprovedContext) error
	}{
		{"missing", func(_ *domain.SwarmContextRef, _ *ApprovedContext) error {
			return errors.New("private resolver detail")
		}},
		{"revoked", func(_ *domain.SwarmContextRef, _ *ApprovedContext) error {
			return fmt.Errorf("%w: private decision detail", ErrContext)
		}},
		{"reference digest invalid", func(ref *domain.SwarmContextRef, _ *ApprovedContext) error { ref.SHA256 = "invalid"; return nil }},
		{"resolved digest changed", func(_ *domain.SwarmContextRef, approved *ApprovedContext) error {
			approved.SHA256 = strings.Repeat("a", 64)
			return nil
		}},
		{"body digest changed", func(_ *domain.SwarmContextRef, approved *ApprovedContext) error {
			approved.Body += " modified"
			return nil
		}},
		{"wrong ID", func(_ *domain.SwarmContextRef, approved *ApprovedContext) error {
			approved.ID = "another-context"
			return nil
		}},
		{"missing approval", func(_ *domain.SwarmContextRef, approved *ApprovedContext) error { approved.DecisionID = ""; return nil }},
		{"zero decision version", func(_ *domain.SwarmContextRef, approved *ApprovedContext) error {
			approved.DecisionVersion = 0
			return nil
		}},
		{"empty body", func(_ *domain.SwarmContextRef, approved *ApprovedContext) error { approved.Body = ""; return nil }},
		{"invalid UTF8", func(_ *domain.SwarmContextRef, approved *ApprovedContext) error {
			approved.Body = string([]byte{0xff})
			return nil
		}},
		{"oversized body", func(_ *domain.SwarmContextRef, approved *ApprovedContext) error {
			approved.Body = strings.Repeat("x", MaxPacketBytes+1)
			return nil
		}},
		{"wrong task scope", func(ref *domain.SwarmContextRef, _ *ApprovedContext) error {
			ref.Selectors.TaskTypes = []string{"docs"}
			return nil
		}},
		{"wrong backend scope", func(ref *domain.SwarmContextRef, _ *ApprovedContext) error {
			ref.Selectors.Backends = []string{"other"}
			return nil
		}},
		{"wrong path scope", func(ref *domain.SwarmContextRef, _ *ApprovedContext) error {
			ref.Selectors.Paths = []string{"tests"}
			return nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, approved := contextTestInput("required", "Required instruction.", true)
			resolveErr := tc.change(&ref, &approved)
			resolver := contextTestResolver(func(context.Context, Subject, domain.SwarmContextRef) (ApprovedContext, error) {
				return approved, resolveErr
			})
			selection, err := SelectContext(context.Background(), []domain.SwarmContextRef{ref}, Subject{TaskType: "fix", Backend: "codex", Paths: []string{"src/file.go"}}, resolver, MaxPacketBytes, contextTestMeasure)
			if !errors.Is(err, ErrContext) || len(selection.Included) != 0 {
				t.Fatalf("expected required failure, got %#v, %v", selection, err)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatalf("resolver details leaked: %v", err)
			}
		})
	}
}

func TestSelectContextOptionalOmissionReceipts(t *testing.T) {
	ids := []string{"wrong-path", "unavailable", "revoked", "missing-approval", "digest", "body", "invalid-utf8", "too-large"}
	refs := make([]domain.SwarmContextRef, 0, len(ids))
	approved := map[string]ApprovedContext{}
	for _, id := range ids {
		ref, body := contextTestInput(id, "Optional instruction.", false)
		if id == "wrong-path" {
			ref.Selectors.Paths = []string{"src"}
		}
		refs = append(refs, ref)
		approved[id] = body
	}
	resolver := contextTestResolver(func(_ context.Context, _ Subject, ref domain.SwarmContextRef) (ApprovedContext, error) {
		body := approved[ref.ID]
		switch ref.ID {
		case "wrong-path":
			t.Fatal("out-of-scope context must not reach approval resolver")
		case "unavailable":
			return ApprovedContext{}, errors.New("secret transport detail")
		case "revoked":
			return ApprovedContext{}, fmt.Errorf("%w: secret revocation detail", ErrContext)
		case "missing-approval":
			body.DecisionID = ""
		case "digest":
			body.SHA256 = strings.Repeat("a", 64)
		case "body":
			body.Body += " mutation"
		case "invalid-utf8":
			body.Body = string([]byte{0xff})
		case "too-large":
			body.Body = strings.Repeat("x", MaxPacketBytes+1)
		}
		return body, nil
	})
	selection, err := SelectContext(context.Background(), refs, Subject{Paths: []string{"srcish/file.go"}}, resolver, MaxPacketBytes, contextTestMeasure)
	if err != nil || len(selection.Included) != 0 {
		t.Fatalf("optional-only omissions must succeed: %#v, %v", selection, err)
	}
	want := []OmittedContext{{"body", "hash_mismatch"}, {"digest", "hash_mismatch"}, {"invalid-utf8", "unapproved"}, {"missing-approval", "unapproved"}, {"revoked", "unapproved"}, {"too-large", "unapproved"}, {"unavailable", "unavailable"}, {"wrong-path", "not_applicable"}}
	if !reflect.DeepEqual(selection.Omitted, want) {
		t.Fatalf("omissions = %#v, want %#v", selection.Omitted, want)
	}
	bytes, _ := contextTestMeasure(selection.Included, selection.Omitted)
	if selection.Bytes != bytes {
		t.Fatalf("receipt bytes not included: %d != %d", selection.Bytes, bytes)
	}
}

func TestSelectContextRelevanceRequiredFirstAndInputOrderIndependent(t *testing.T) {
	selectors := map[string]domain.SwarmContextSelectors{
		"zz-required": {},
		"two-path":    {TaskTypes: []string{"fix"}, Paths: []string{"src"}},
		"two-other":   {TaskTypes: []string{"fix"}, Backends: []string{"codex"}},
		"deep":        {Paths: []string{"src/deep"}},
		"literal-a":   {Paths: []string{"src"}},
		"literal-b":   {Paths: []string{"src/**"}},
		"alternates":  {Paths: []string{"src", "unmatched/very/long/path"}},
		"global":      {},
	}
	refs := make([]domain.SwarmContextRef, 0, len(selectors))
	bodies := map[string]ApprovedContext{}
	for id, selector := range selectors {
		ref, approved := contextTestInput(id, "Synthetic instruction "+id, id == "zz-required")
		ref.Selectors = selector
		refs = append(refs, ref)
		bodies[id] = approved
	}
	wantSubject := Subject{ProjectID: "fixture-project", RunID: "fixture-run", TaskID: "fixture-task", AttemptID: "fixture-attempt", TaskType: "fix", Backend: "codex", Paths: []string{"src/deep/file.go"}}
	var resolved []string
	resolver := contextTestResolver(func(_ context.Context, subject Subject, ref domain.SwarmContextRef) (ApprovedContext, error) {
		if !reflect.DeepEqual(subject, wantSubject) {
			t.Fatalf("resolver lost current assignment scope: %#v", subject)
		}
		resolved = append(resolved, ref.ID)
		return bodies[ref.ID], nil
	})
	first, err := SelectContext(context.Background(), refs, wantSubject, resolver, MaxPacketBytes, contextTestMeasure)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"zz-required", "two-path", "two-other", "deep", "literal-a", "literal-b", "alternates", "global"}
	if !reflect.DeepEqual(contextTestIDs(first), want) || !reflect.DeepEqual(resolved, want) {
		t.Fatalf("included %v, resolved %v; want %v", contextTestIDs(first), resolved, want)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID > refs[j].ID })
	second, err := SelectContext(context.Background(), refs, wantSubject, resolver, MaxPacketBytes, contextTestMeasure)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("selection depends on input order: %#v, %#v, %v", first, second, err)
	}
}

func TestSelectContextCompleteByteBudget(t *testing.T) {
	required, requiredBody := contextTestInput("required", "Required instruction with Unicode: é 🧭.", true)
	optional, optionalBody := contextTestInput("optional", strings.Repeat("Optional whole instruction. ", 60), false)
	resolver := contextTestResolver(func(_ context.Context, _ Subject, ref domain.SwarmContextRef) (ApprovedContext, error) {
		if ref.ID == required.ID {
			return requiredBody, nil
		}
		return optionalBody, nil
	})
	mandatory, err := SelectContext(context.Background(), []domain.SwarmContextRef{required}, Subject{}, resolver, MaxPacketBytes, contextTestMeasure)
	if err != nil {
		t.Fatal(err)
	}
	exact, err := SelectContext(context.Background(), []domain.SwarmContextRef{required}, Subject{}, resolver, mandatory.Bytes, contextTestMeasure)
	if err != nil || exact.Bytes != mandatory.Bytes || exact.Included[0].Body != requiredBody.Body {
		t.Fatalf("exact UTF8 byte boundary failed: %#v, %v", exact, err)
	}
	if _, err := SelectContext(context.Background(), []domain.SwarmContextRef{required}, Subject{}, resolver, mandatory.Bytes-1, contextTestMeasure); !errors.Is(err, ErrBudget) {
		t.Fatalf("mandatory one-byte overflow = %v", err)
	}
	omission := []OmittedContext{{ID: optional.ID, Reason: "budget"}}
	withReceipt, _ := contextTestMeasure(mandatory.Included, omission)
	var measurements int
	measure := func(included []IncludedContext, omitted []OmittedContext) (int, error) {
		measurements++
		if len(included)+len(omitted) != 2 {
			t.Fatalf("candidate accounting dropped a receipt: %#v, %#v", included, omitted)
		}
		return contextTestMeasure(included, omitted)
	}
	selection, err := SelectContext(context.Background(), []domain.SwarmContextRef{optional, required}, Subject{}, resolver, withReceipt, measure)
	if err != nil || !reflect.DeepEqual(contextTestIDs(selection), []string{"required"}) || !reflect.DeepEqual(selection.Omitted, omission) || selection.Bytes != withReceipt || measurements != 2 {
		t.Fatalf("whole optional omission/full budget failed: %#v, measurements %d, %v", selection, measurements, err)
	}
	if _, err := SelectContext(context.Background(), []domain.SwarmContextRef{required, optional}, Subject{}, resolver, withReceipt-1, contextTestMeasure); !errors.Is(err, ErrBudget) {
		t.Fatalf("unfittable receipt must fail instead of silently disappearing: %v", err)
	}
	full, err := SelectContext(context.Background(), []domain.SwarmContextRef{required, optional}, Subject{}, resolver, MaxPacketBytes, contextTestMeasure)
	if err != nil || len(full.Included) != 2 {
		t.Fatalf("full packet fixture failed: %#v, %v", full, err)
	}
	exact, err = SelectContext(context.Background(), []domain.SwarmContextRef{required, optional}, Subject{}, resolver, full.Bytes, contextTestMeasure)
	if err != nil || !reflect.DeepEqual(full, exact) {
		t.Fatalf("exact complete packet boundary failed: %#v, %v", exact, err)
	}
	oneLess, err := SelectContext(context.Background(), []domain.SwarmContextRef{required, optional}, Subject{}, resolver, full.Bytes-1, contextTestMeasure)
	if err != nil || len(oneLess.Included) != 1 || !reflect.DeepEqual(oneLess.Omitted, omission) {
		t.Fatalf("one-byte optional overflow must omit the whole item: %#v, %v", oneLess, err)
	}
}

func TestSelectContextSkipsOversizedOptionalAndContinues(t *testing.T) {
	large, largeBody := contextTestInput("a-large", strings.Repeat("large", 200), false)
	small, smallBody := contextTestInput("b-small", "Small whole instruction.", false)
	resolver := contextTestResolver(func(_ context.Context, _ Subject, ref domain.SwarmContextRef) (ApprovedContext, error) {
		if ref.ID == large.ID {
			return largeBody, nil
		}
		return smallBody, nil
	})
	selection, err := SelectContext(context.Background(), []domain.SwarmContextRef{small, large}, Subject{}, resolver, 600, contextTestMeasure)
	if err != nil || !reflect.DeepEqual(contextTestIDs(selection), []string{"b-small"}) || !reflect.DeepEqual(selection.Omitted, []OmittedContext{{"a-large", "budget"}}) {
		t.Fatalf("later fitting optional lost: %#v, %v", selection, err)
	}
	if selection.Included[0].Body != smallBody.Body || selection.Bytes > 600 {
		t.Fatalf("included body changed or packet overflowed: %#v", selection)
	}
}

func TestSelectContextDirectValidation(t *testing.T) {
	ref, approved := contextTestInput("fixture", "Synthetic instruction.", false)
	resolver := contextTestResolver(func(context.Context, Subject, domain.SwarmContextRef) (ApprovedContext, error) { return approved, nil })
	cases := []struct {
		name    string
		refs    []domain.SwarmContextRef
		subject Subject
		limit   int
		measure Measure
	}{
		{"duplicate IDs", []domain.SwarmContextRef{ref, ref}, Subject{}, MaxPacketBytes, contextTestMeasure},
		{"too many refs", make([]domain.SwarmContextRef, 129), Subject{}, MaxPacketBytes, contextTestMeasure},
		{"bad subject path", []domain.SwarmContextRef{ref}, Subject{Paths: []string{"../escape"}}, MaxPacketBytes, contextTestMeasure},
		{"wildcard subject path", []domain.SwarmContextRef{ref}, Subject{Paths: []string{"src/**"}}, MaxPacketBytes, contextTestMeasure},
		{"zero budget", []domain.SwarmContextRef{ref}, Subject{}, 0, contextTestMeasure},
		{"excessive budget", []domain.SwarmContextRef{ref}, Subject{}, MaxPacketBytes + 1, contextTestMeasure},
		{"missing measure", []domain.SwarmContextRef{ref}, Subject{}, MaxPacketBytes, nil},
		{"negative measure", []domain.SwarmContextRef{ref}, Subject{}, MaxPacketBytes, func([]IncludedContext, []OmittedContext) (int, error) { return -1, nil }},
	}
	for _, selector := range []string{"./**", "src/*.go", "src/[ab]", "src/../other", "/absolute", "src\\file", "src/**/file"} {
		bad := ref
		bad.Selectors.Paths = []string{selector}
		cases = append(cases, struct {
			name    string
			refs    []domain.SwarmContextRef
			subject Subject
			limit   int
			measure Measure
		}{"bad selector " + selector, []domain.SwarmContextRef{bad}, Subject{}, MaxPacketBytes, contextTestMeasure})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SelectContext(context.Background(), tc.refs, tc.subject, resolver, tc.limit, tc.measure); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
	if _, err := SelectContext(context.Background(), []domain.SwarmContextRef{ref}, Subject{}, nil, MaxPacketBytes, contextTestMeasure); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing optional resolver = %v", err)
	}
	ref.Required = true
	if _, err := SelectContext(context.Background(), []domain.SwarmContextRef{ref}, Subject{}, nil, MaxPacketBytes, contextTestMeasure); !errors.Is(err, ErrContext) {
		t.Fatalf("missing mandatory resolver = %v", err)
	}
	if selection, err := SelectContext(context.Background(), nil, Subject{}, nil, MaxPacketBytes, contextTestMeasure); err != nil || len(selection.Included) != 0 || len(selection.Omitted) != 0 || selection.Bytes <= 0 {
		t.Fatalf("empty optional context must be legal: %#v, %v", selection, err)
	}
}

func TestSelectContextCancellation(t *testing.T) {
	ref, approved := contextTestInput("fixture", "Synthetic instruction.", true)
	ctx, cancel := context.WithCancel(context.Background())
	resolver := contextTestResolver(func(context.Context, Subject, domain.SwarmContextRef) (ApprovedContext, error) {
		cancel()
		return approved, nil
	})
	if _, err := SelectContext(ctx, []domain.SwarmContextRef{ref}, Subject{}, resolver, MaxPacketBytes, contextTestMeasure); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during resolution = %v", err)
	}
	if _, err := SelectContext(ctx, nil, Subject{}, nil, MaxPacketBytes, contextTestMeasure); !errors.Is(err, context.Canceled) {
		t.Fatalf("initial cancellation = %v", err)
	}
}
