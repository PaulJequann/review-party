package configuration

import (
	"errors"
	"slices"
	"testing"
)

func TestAddReviewSelectionIgnoresAnItemItsGroupAlreadySelects(t *testing.T) {
	current := ReviewSelection{ConcurrencyLimit: 2, Global: []SelectionItem{{Profile: "bugs"}}}
	repeated := AddReviewSelection(current, ScopeGlobal, SelectionItem{Profile: "bugs"}).Selection
	if !slices.Equal(repeated.Global, current.Global) || len(repeated.Repository) != 0 {
		t.Fatalf("repeated add changed the selection: %#v", repeated)
	}
	crossScope := AddReviewSelection(repeated, ScopeRepository, SelectionItem{Profile: "bugs"}).Selection
	want := []SelectionItem{{Profile: "bugs"}}
	if !slices.Equal(crossScope.Repository, want) || !slices.Equal(crossScope.Global, want) {
		t.Fatalf("a different group must still receive the item: %#v", crossScope)
	}
	party := AddReviewSelection(crossScope, ScopeGlobal, SelectionItem{Party: "bugs"}).Selection
	if !slices.Equal(party.Global, []SelectionItem{{Profile: "bugs"}, {Party: "bugs"}}) {
		t.Fatalf("a Party must not be mistaken for a same-named Profile: %#v", party.Global)
	}
}

type resolvedTarget struct {
	group Scope
	item  SelectionItem
}

func TestResolveSelectionTargetPrefersRepositoryBeforeGlobal(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.profile(t, ScopeRepository, "bugs")
	fixture.profile(t, ScopeGlobal, "docs")
	fixture.party(t, partySpec{scope: ScopeGlobal, name: "crew", limit: 1, members: []ProfileReference{{Scope: ScopeGlobal, Profile: "docs"}}})
	cases := []struct {
		kind      AuthoredItemKind
		value     string
		wantGroup Scope
		wantItem  SelectionItem
	}{
		{ItemProfile, "bugs", ScopeRepository, SelectionItem{Profile: "bugs"}},
		{ItemProfile, "global:bugs", ScopeGlobal, SelectionItem{Profile: "bugs"}},
		{ItemProfile, "docs", ScopeGlobal, SelectionItem{Profile: "docs"}},
		{ItemParty, "crew", ScopeGlobal, SelectionItem{Party: "crew"}},
	}
	for _, testCase := range cases {
		group, item, err := fixture.manager.ResolveSelectionTarget(fixture.repository, testCase.kind, testCase.value)
		if err != nil {
			t.Fatalf("%s %q: %v", testCase.kind, testCase.value, err)
		}
		if got, want := (resolvedTarget{group, item}), (resolvedTarget{testCase.wantGroup, testCase.wantItem}); got != want {
			t.Fatalf("%s %q = %#v, want %#v", testCase.kind, testCase.value, got, want)
		}
	}
}

func TestResolveSelectionTargetReportsAvailableNamesForAMissingName(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	_, _, err := fixture.manager.ResolveSelectionTarget(fixture.repository, ItemProfile, "ghosts")
	var unresolved UnresolvedReferenceError
	if !errors.As(err, &unresolved) {
		t.Fatalf("error = %v, want UnresolvedReferenceError", err)
	}
	requireUnresolvedDetails(t, unresolved, "ghosts", "")
	_, _, err = fixture.manager.ResolveSelectionTarget(fixture.repository, ItemProfile, "repository:bugs")
	if !errors.As(err, &unresolved) || unresolved.Scope != ScopeRepository {
		t.Fatalf("qualified miss = %v, want a Repository UnresolvedReferenceError", err)
	}
}

func TestSelectionBindingListsEveryMissingNameIncludingPartyMembers(t *testing.T) {
	author := newSelectionFixture(t)
	for _, name := range []string{"bugs", "docs"} {
		author.profile(t, ScopeGlobal, name)
	}
	author.party(t, partySpec{scope: ScopeRepository, name: "team", limit: 2, members: []ProfileReference{
		{Scope: ScopeGlobal, Profile: "bugs"}, {Scope: ScopeGlobal, Profile: "docs"},
	}})
	author.reviews(t, ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []SelectionItem{{Profile: "style"}, {Party: "crew"}, {Profile: "bugs"}},
		Repository:       []SelectionItem{{Party: "team"}, {Profile: "local"}, {Profile: "style"}},
	})
	teammate := selectionFixture{manager: testManager(t, t.TempDir()), repository: author.repository}
	teammate.profile(t, ScopeGlobal, "bugs")

	binding, err := teammate.manager.SelectionBinding(teammate.repository)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(binding.Unresolved))
	for _, missing := range binding.Unresolved {
		got = append(got, string(missing.Kind)+" "+string(missing.Scope)+":"+missing.Name+"@"+missing.SelectedBy)
	}
	want := []string{
		"profile global:style@reviews.global[0]",
		"party global:crew@reviews.global[1]",
		"profile global:docs@reviews.repository[0]#1",
		"profile repository:local@reviews.repository[1]",
		"profile repository:style@reviews.repository[2]",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unresolved = %#v, want %#v", got, want)
	}
	if !binding.Declared || binding.Ready() {
		t.Fatalf("binding = %#v, want declared and not ready", binding)
	}
	if !slices.Contains(binding.Unresolved[0].Available, "bugs") {
		t.Fatalf("available = %#v, want the teammate's Global Profiles", binding.Unresolved[0].Available)
	}
}

func TestSelectionBindingDistinguishesUndeclaredFromReady(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	if binding := requireSelectionBinding(t, fixture); binding.Declared || binding.Ready() {
		t.Fatalf("binding without a selection = %#v, want undeclared", binding)
	}
	fixture.reviews(t, ReviewSelection{ConcurrencyLimit: 1, Global: []SelectionItem{{Profile: "bugs"}}, Repository: []SelectionItem{}})
	if binding := requireSelectionBinding(t, fixture); !binding.Ready() {
		t.Fatalf("binding with a resolvable selection = %#v, want ready", binding)
	}
}

func requireSelectionBinding(t *testing.T, fixture selectionFixture) SelectionBinding {
	t.Helper()
	binding, err := fixture.manager.SelectionBinding(fixture.repository)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}
