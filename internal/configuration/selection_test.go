package configuration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type selectionFixture struct {
	manager    *Manager
	repository Repository
}

func newSelectionFixture(t *testing.T) selectionFixture {
	t.Helper()
	return selectionFixture{manager: testManager(t, t.TempDir()), repository: Repository(t.TempDir())}
}

func (fixture selectionFixture) profile(t *testing.T, scope Scope, name string) {
	t.Helper()
	draft := ProfileDraft{
		Target: scope, Name: name,
		Reviewer: "grok", Model: "grok-4.5", ReasoningEffort: "high",
		AttemptDeadline: "1m", Instructions: "Review " + name + ".\n",
	}
	repository := fixture.repository
	if scope == ScopeGlobal {
		repository = ""
	}
	plan, planningErr := fixture.manager.PlanProfileCreation(repository, draft)
	fixture.publish(t, plan, planningErr)
}

type partySpec struct {
	scope   Scope
	name    string
	limit   int
	members []ProfileReference
}

func (fixture selectionFixture) party(t *testing.T, spec partySpec) {
	t.Helper()
	draft := PartyDraft{Target: spec.scope, Name: spec.name, ConcurrencyLimit: spec.limit, Profiles: spec.members}
	repository := fixture.repository
	if spec.scope == ScopeGlobal {
		repository = ""
	}
	plan, planningErr := fixture.manager.PlanPartyCreation(repository, draft)
	fixture.publish(t, plan, planningErr)
}

func (fixture selectionFixture) reviews(t *testing.T, selection ReviewSelection) {
	t.Helper()
	payload := struct {
		SchemaVersion int             `json:"schema_version"`
		Reviews       ReviewSelection `json:"reviews"`
	}{SchemaVersion: SchemaVersion, Reviews: selection}
	path := filepath.Join(string(fixture.repository), ".reviewparty", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (fixture selectionFixture) resolve(t *testing.T, request RunRequest) ResolvedReviews {
	t.Helper()
	resolved, err := fixture.manager.ResolveRun(request)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// publish stages nothing until confirmed; every fixture definition is valid.
func (fixture selectionFixture) publish(t *testing.T, plan Plan, planningErr error) {
	t.Helper()
	if planningErr != nil || !plan.Valid() {
		t.Fatalf("plan error = %v reason %q", planningErr, plan.Reason())
	}
	if err := fixture.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func scopedSlots(resolved ResolvedReviews) []string {
	slots := make([]string, 0, len(resolved.Expanded))
	for _, slot := range resolved.Expanded {
		slots = append(slots, string(slot.Scope)+":"+slot.Profile+"@"+slot.Origin)
	}
	return slots
}

func expectSlots(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("expanded slots = %#v, want %#v", got, want)
	}
}

func TestParseScopedReference(t *testing.T) {
	tests := []struct {
		input string
		scope Scope
		name  string
	}{
		{input: "global:bugs", scope: ScopeGlobal, name: "bugs"},
		{input: "repository:bugs", scope: ScopeRepository, name: "bugs"},
		{input: "bugs", name: "bugs"},
		{input: "other:bugs", name: "other:bugs"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			scope, name := ParseScopedReference(test.input)
			if scope != test.scope || name != test.name {
				t.Fatalf("parseScopedReference(%q) = (%q, %q), want (%q, %q)", test.input, scope, name, test.scope, test.name)
			}
		})
	}
}

func TestDefaultSelectionExpandsGlobalBeforeRepositoryInAuthoredOrder(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.profile(t, ScopeGlobal, "code-quality")
	fixture.profile(t, ScopeGlobal, "documentation")
	fixture.profile(t, ScopeRepository, "security")
	fixture.reviews(t, ReviewSelection{
		ConcurrencyLimit: 2,
		Global:           []SelectionItem{{Profile: "documentation"}, {Profile: "bugs"}},
		Repository:       []SelectionItem{{Profile: "security"}},
	})
	resolved := fixture.resolve(t, RunRequest{Repository: fixture.repository})
	expectSlots(t, scopedSlots(resolved),
		"global:documentation@reviews.global[0]",
		"global:bugs@reviews.global[1]",
		"repository:security@reviews.repository[0]",
	)
	requireResolvedFacts(t, resolved, facts{kind: SelectionRepositoryDefault, limit: 2, source: LimitRepositorySelection})
}

type facts struct {
	kind   SelectionKind
	limit  int
	source LimitSource
}

func requireResolvedFacts(t *testing.T, resolved ResolvedReviews, want facts) {
	t.Helper()
	requireKind(t, resolved.Kind, want.kind)
	if resolved.ConcurrencyLimit != want.limit || resolved.LimitSource != want.source {
		t.Fatalf("limit facts = (%d, %q), want (%d, %q)", resolved.ConcurrencyLimit, resolved.LimitSource, want.limit, want.source)
	}
}

func requireKind(t *testing.T, got SelectionKind, want SelectionKind) {
	t.Helper()
	if got != want {
		t.Fatalf("kind = %q, want %q", got, want)
	}
}

func TestExactOverlapDeduplicatesAtFirstOccurrence(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.profile(t, ScopeGlobal, "code-quality")
	fixture.party(t, partySpec{scope: ScopeGlobal, name: "extra", limit: 3, members: []ProfileReference{
		{Scope: ScopeGlobal, Profile: "bugs"},
		{Scope: ScopeGlobal, Profile: "code-quality"},
	}})
	fixture.reviews(t, ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []SelectionItem{{Profile: "bugs"}, {Party: "extra"}},
		Repository:       []SelectionItem{},
	})
	resolved := fixture.resolve(t, RunRequest{Repository: fixture.repository})
	expectSlots(t, scopedSlots(resolved),
		"global:bugs@reviews.global[0]",
		"global:code-quality@reviews.global[1]#1",
	)
	wantSkipped := SkippedProfile{
		Scope: ScopeGlobal, Profile: "bugs",
		Origin: "reviews.global[1]#0", KeptOrigin: "reviews.global[0]",
	}
	if len(resolved.Deduplicated) != 1 || resolved.Deduplicated[0] != wantSkipped {
		t.Fatalf("deduplicated = %#v, want exactly %#v", resolved.Deduplicated, wantSkipped)
	}
}

func TestCrossScopeSameNameRunsTwiceWithStrongWarning(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "code-quality")
	fixture.profile(t, ScopeRepository, "code-quality")
	fixture.reviews(t, ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []SelectionItem{{Profile: "code-quality"}},
		Repository:       []SelectionItem{{Profile: "code-quality"}},
	})
	resolved := fixture.resolve(t, RunRequest{Repository: fixture.repository})
	if len(resolved.Expanded) != 2 {
		t.Fatalf("expanded = %#v, want both scoped identities", resolved.Expanded)
	}
	requireNoDeduplication(t, resolved.Deduplicated)
	warning := singleWarning(t, resolved.Warnings)
	if warning.Category != WarningSameNameCrossScope || warning.Name != "code-quality" {
		t.Fatalf("warning = %#v, want a same-name cross-scope warning for code-quality", warning)
	}
}

func requireNoDeduplication(t *testing.T, duplicates []SkippedProfile) {
	t.Helper()
	if len(duplicates) != 0 {
		t.Fatalf("cross-scope identities must not deduplicate: %#v", duplicates)
	}
}

func singleWarning(t *testing.T, warnings []ResolverWarning) ResolverWarning {
	t.Helper()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want exactly one", warnings)
	}
	return warnings[0]
}

func TestUnqualifiedExplicitNameResolvesRepositoryBeforeGlobal(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "shared")
	fixture.profile(t, ScopeRepository, "shared")

	unqualified := fixture.resolve(t, RunRequest{Repository: fixture.repository, Profile: "shared"})
	if unqualified.Kind != SelectionExplicitProfile {
		t.Fatalf("kind = %q", unqualified.Kind)
	}
	requireAuthoredScope(t, unqualified.Authored, ScopeRepository)

	exact := fixture.resolve(t, RunRequest{Repository: fixture.repository, Profile: "global:shared"})
	requireAuthoredScope(t, exact.Authored, ScopeGlobal)
	requireResolvedFacts(t, unqualified, facts{kind: SelectionExplicitProfile, limit: 1, source: LimitExplicitProfile})
}

func requireAuthoredScope(t *testing.T, authored []SelectedDefinition, scope Scope) {
	t.Helper()
	if len(authored) != 1 || authored[0].Scope != scope {
		t.Fatalf("authored = %#v, want one %s definition", authored, scope)
	}
}

func TestExplicitPartyKeepsMemberOrderAndOwnsConcurrency(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.profile(t, ScopeGlobal, "documentation")
	fixture.party(t, partySpec{scope: ScopeGlobal, name: "baseline", limit: 4, members: baselineMembers()})
	resolved := fixture.resolve(t, RunRequest{Repository: fixture.repository, Party: "global:baseline"})
	expectSlots(t, scopedSlots(resolved),
		"global:documentation@party baseline#0",
		"global:bugs@party baseline#1",
	)
	requireResolvedFacts(t, resolved, facts{kind: SelectionExplicitParty, limit: 4, source: LimitParty})
	requireAuthoredName(t, resolved.Authored, "baseline")
}

func baselineMembers() []ProfileReference {
	return []ProfileReference{
		{Scope: ScopeGlobal, Profile: "documentation"},
		{Scope: ScopeGlobal, Profile: "bugs"},
	}
}

func requireAuthoredName(t *testing.T, authored []SelectedDefinition, want string) {
	t.Helper()
	entry := singleAuthoredDefinition(t, authored)
	if entry.Name != want {
		t.Fatalf("authored name = %q, want %q", entry.Name, want)
	}
}

func singleAuthoredDefinition(t *testing.T, authored []SelectedDefinition) SelectedDefinition {
	t.Helper()
	if len(authored) != 1 {
		t.Fatalf("authored = %#v, want exactly one entry", authored)
	}
	return authored[0]
}

func TestResolveRunRejectsConflictingExplicitChoices(t *testing.T) {
	fixture := newSelectionFixture(t)
	conflicted := RunRequest{Repository: fixture.repository, Profile: "a", Party: "b"}
	_, err := fixture.manager.ResolveRun(conflicted)
	if err == nil || !strings.Contains(err.Error(), "cannot be selected together") {
		t.Fatalf("error = %v", err)
	}
}

func TestMissingSavedSelectionFailsClosed(t *testing.T) {
	fixture := newSelectionFixture(t)
	_, err := fixture.manager.ResolveRun(RunRequest{Repository: fixture.repository})
	if !errors.Is(err, ErrNoRepositorySelection) {
		t.Fatalf("error = %v, want ErrNoRepositorySelection", err)
	}
	fixture.reviews(t, ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []SelectionItem{},
		Repository:       []SelectionItem{},
	})
	_, err = fixture.manager.ResolveRun(RunRequest{Repository: fixture.repository})
	if !errors.Is(err, ErrNoRepositorySelection) || !strings.Contains(err.Error(), "selects nothing") {
		t.Fatalf("empty roll-up error = %v", err)
	}
}

func TestDuplicatedSavedSelectionRunsOnceAndRecordsTheSkip(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.reviews(t, ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []SelectionItem{{Profile: "bugs"}, {Profile: "bugs"}},
		Repository:       []SelectionItem{},
	})
	resolved := fixture.resolve(t, RunRequest{Repository: fixture.repository})
	expectSlots(t, scopedSlots(resolved), "global:bugs@reviews.global[0]")
	wantSkipped := SkippedProfile{
		Scope: ScopeGlobal, Profile: "bugs",
		Origin: "reviews.global[1]", KeptOrigin: "reviews.global[0]",
	}
	if len(resolved.Deduplicated) != 1 || resolved.Deduplicated[0] != wantSkipped {
		t.Fatalf("deduplicated = %#v, want exactly %#v", resolved.Deduplicated, wantSkipped)
	}
}

func TestMissingReferenceFailsClosedWithAvailableNames(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.reviews(t, ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []SelectionItem{{Profile: "ghosts"}},
		Repository:       []SelectionItem{},
	})
	_, err := fixture.manager.ResolveRun(RunRequest{Repository: fixture.repository})
	var unresolved UnresolvedReferenceError
	if !errors.As(err, &unresolved) {
		t.Fatalf("error = %v, want UnresolvedReferenceError", err)
	}
	requireUnresolvedDetails(t, unresolved, "ghosts", "reviews.global[0]")
}

func requireUnresolvedDetails(t *testing.T, failure UnresolvedReferenceError, name, selectedBy string) {
	t.Helper()
	if failure.Name != name {
		t.Fatalf("name = %q, want %q", failure.Name, name)
	}
	if failure.SelectedBy != selectedBy {
		t.Fatalf("selectedBy = %q, want %q", failure.SelectedBy, selectedBy)
	}
	for _, available := range failure.Available {
		if available == "bugs" {
			return
		}
	}
	t.Fatalf("available names omit bugs: %#v", failure.Available)
}
