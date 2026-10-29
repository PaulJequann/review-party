package configurationhub

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reviewparty/internal/configuration"
)

var (
	sharedProfile = configuration.SelectionItem{Profile: "shared"}
	dailyParty    = configuration.SelectionItem{Party: "daily"}
)

func reviewFixture(t *testing.T) (*configuration.Manager, configuration.Repository) {
	t.Helper()
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}})
	repository := configuration.Repository(t.TempDir())
	publishPreviewProfile(t, manager, configuration.ScopeGlobal, "")
	publishPreviewProfile(t, manager, configuration.ScopeRepository, repository)
	plan, err := manager.PlanPartyCreation(repository, configuration.PartyDraft{
		Target: configuration.ScopeRepository, Name: "daily", ConcurrencyLimit: 2,
		Profiles: []configuration.ProfileReference{{Scope: configuration.ScopeRepository, Profile: "shared"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	return manager, repository
}

func publishScopedSelection(t *testing.T, manager *configuration.Manager, repository configuration.Repository, target configuration.Scope, selection configuration.ReviewSelection) {
	t.Helper()
	plan, err := manager.Plan(repository, []configuration.Intent{configuration.SetReviewSelection{Target: target, Selection: selection}})
	if err != nil {
		t.Fatalf("plan %s selection: %v", target, err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatalf("publish %s selection: %v", target, err)
	}
}

func publishReviewEdit(t *testing.T, manager *configuration.Manager, repository configuration.Repository, draft reviewFormDraft) configuration.ReviewSelection {
	t.Helper()
	plan, selection, err := planReviewEdit(manager, repository, draft)
	if err != nil {
		t.Fatalf("plan %s: %v", draft.operation, err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatalf("publish %s: %v", draft.operation, err)
	}
	return selection
}

func TestPlanReviewEditAppliesEachOperationToTheRepositorySelection(t *testing.T) {
	seed := configuration.ReviewSelection{ConcurrencyLimit: 2, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{sharedProfile, dailyParty}}
	tests := []struct {
		name  string
		draft reviewFormDraft
		want  configuration.ReviewSelection
	}{
		{
			name:  "add",
			draft: reviewFormDraft{operation: reviewAdd, choice: configuration.SelectedDefinition{Kind: configuration.ItemProfile, Name: "shared", Scope: configuration.ScopeGlobal}},
			want:  configuration.ReviewSelection{ConcurrencyLimit: 2, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{sharedProfile, dailyParty}},
		},
		{
			name:  "remove",
			draft: reviewFormDraft{operation: reviewRemove, reviewEntry: reviewEntry{group: configuration.ScopeRepository, index: 1, expect: dailyParty}},
			want:  configuration.ReviewSelection{ConcurrencyLimit: 2, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{sharedProfile}},
		},
		{
			name:  "move",
			draft: reviewFormDraft{operation: reviewMove, reviewEntry: reviewEntry{group: configuration.ScopeRepository, index: 0, expect: sharedProfile}, to: 1},
			want:  configuration.ReviewSelection{ConcurrencyLimit: 2, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{dailyParty, sharedProfile}},
		},
		{
			name:  "concurrency",
			draft: reviewFormDraft{operation: reviewConcurrency, concurrency: " 5 "},
			want:  configuration.ReviewSelection{ConcurrencyLimit: 5, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{sharedProfile, dailyParty}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, repository := reviewFixture(t)
			publishScopedSelection(t, manager, repository, configuration.ScopeRepository, seed)
			test.draft.target = configuration.ScopeRepository
			staged := publishReviewEdit(t, manager, repository, test.draft)
			if !reflect.DeepEqual(staged, test.want) {
				t.Fatalf("staged selection = %#v, want %#v", staged, test.want)
			}
			published, _, err := manager.EffectiveReviewSelection(repository)
			if err != nil || !reflect.DeepEqual(published, test.want) {
				t.Fatalf("published selection = %#v, %v, want %#v", published, err, test.want)
			}
		})
	}
}

func TestPlanReviewEditRefusesAnEntryTheSelectionNoLongerHolds(t *testing.T) {
	manager, repository := reviewFixture(t)
	seed := configuration.ReviewSelection{ConcurrencyLimit: 2, Repository: []configuration.SelectionItem{sharedProfile, dailyParty}}
	publishScopedSelection(t, manager, repository, configuration.ScopeRepository, seed)
	for _, draft := range []reviewFormDraft{
		{operation: reviewRemove, reviewEntry: reviewEntry{group: configuration.ScopeRepository, index: 1, expect: sharedProfile}},
		{operation: reviewMove, reviewEntry: reviewEntry{group: configuration.ScopeRepository, index: 2, expect: dailyParty}},
		{operation: reviewRemove, reviewEntry: reviewEntry{group: configuration.ScopeGlobal, index: 0, expect: sharedProfile}},
	} {
		draft.target = configuration.ScopeRepository
		if _, _, err := planReviewEdit(manager, repository, draft); !errors.Is(err, errStaleReviewSelection) || err.Error() != "The selection changed; reopen Reviews." {
			t.Fatalf("%s %#v error = %v, want stale refusal", draft.operation, draft.reviewEntry, err)
		}
	}
	if current, _, err := manager.EffectiveReviewSelection(repository); err != nil || !reflect.DeepEqual(current, configuration.ReviewSelection{ConcurrencyLimit: 2, Global: []configuration.SelectionItem{}, Repository: seed.Repository}) {
		t.Fatalf("refused edit changed the selection: %#v, %v", current, err)
	}
}

func TestPlanReviewEditStartsARepositoryEditFromACopyOfTheGlobalSelection(t *testing.T) {
	manager, repository := reviewFixture(t)
	global := configuration.ReviewSelection{ConcurrencyLimit: 3, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{}}
	publishScopedSelection(t, manager, "", configuration.ScopeGlobal, global)
	draft := reviewFormDraft{target: configuration.ScopeRepository, operation: reviewAdd, choice: configuration.SelectedDefinition{Kind: configuration.ItemParty, Name: "daily", Scope: configuration.ScopeRepository}}
	staged := publishReviewEdit(t, manager, repository, draft)
	want := configuration.ReviewSelection{ConcurrencyLimit: 3, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{dailyParty}}
	if !reflect.DeepEqual(staged, want) {
		t.Fatalf("repository edit = %#v, want %#v", staged, want)
	}
	if kept, err := manager.ReviewSelectionToEdit(configuration.ScopeGlobal, repository); err != nil || !reflect.DeepEqual(kept, global) {
		t.Fatalf("Global selection changed: %#v, %v", kept, err)
	}
	if _, source, err := manager.EffectiveReviewSelection(repository); err != nil || source.Source != configuration.SourceRepository {
		t.Fatalf("repository did not gain its own selection: %#v, %v", source, err)
	}
}

func TestPlanReviewEditInheritsTheGlobalSelection(t *testing.T) {
	manager, repository := reviewFixture(t)
	global := configuration.ReviewSelection{ConcurrencyLimit: 3, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{}}
	publishScopedSelection(t, manager, "", configuration.ScopeGlobal, global)
	publishScopedSelection(t, manager, repository, configuration.ScopeRepository, configuration.ReviewSelection{ConcurrencyLimit: 2, Repository: []configuration.SelectionItem{dailyParty}})
	draft := reviewFormDraft{target: configuration.ScopeRepository, operation: reviewInherit}
	if staged := publishReviewEdit(t, manager, repository, draft); !reflect.DeepEqual(staged, global) {
		t.Fatalf("inherit preview = %#v, want Global %#v", staged, global)
	}
	if current, source, err := manager.EffectiveReviewSelection(repository); err != nil || source.Source != configuration.SourceGlobal || !reflect.DeepEqual(current, global) {
		t.Fatalf("inherited selection = %#v from %#v, %v", current, source, err)
	}
	if _, _, err := planReviewEdit(manager, repository, draft); err == nil || !strings.Contains(err.Error(), "nothing to inherit") {
		t.Fatalf("repeated inherit error = %v", err)
	}
}

func TestReviewEditorsPlanTheSameChangesForTheSameDraft(t *testing.T) {
	daily := configuration.SelectedDefinition{Kind: configuration.ItemParty, Name: "daily", Scope: configuration.ScopeRepository}
	tests := []struct {
		name  string
		input func(Snapshot) string
		draft reviewFormDraft
	}{
		{
			name: "add",
			input: func(snapshot Snapshot) string {
				for index, choice := range snapshot.ReviewChoices {
					if choice == daily {
						return fmt.Sprintf("1\n1\n%d\nn\n", index+1)
					}
				}
				return ""
			},
			draft: reviewFormDraft{operation: reviewAdd, choice: daily},
		},
		{
			name:  "remove",
			input: func(Snapshot) string { return "1\n2\n2\nn\n" },
			draft: reviewFormDraft{operation: reviewRemove, reviewEntry: reviewEntry{group: configuration.ScopeRepository, index: 0, expect: sharedProfile}},
		},
		{
			name:  "move",
			input: func(Snapshot) string { return "1\n3\n3\n1\nn\n" },
			draft: reviewFormDraft{operation: reviewMove, reviewEntry: reviewEntry{group: configuration.ScopeRepository, index: 1, expect: dailyParty}, to: 0},
		},
		{
			name:  "concurrency",
			input: func(Snapshot) string { return "1\n4\n4\nn\n" },
			draft: reviewFormDraft{operation: reviewConcurrency, concurrency: "4"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, repository := reviewFixture(t)
			selection := configuration.ReviewSelection{ConcurrencyLimit: 2, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{sharedProfile}}
			if test.name != "add" {
				selection.Repository = append(selection.Repository, dailyParty)
			}
			publishScopedSelection(t, manager, repository, configuration.ScopeRepository, selection)
			snapshot, err := buildSnapshot(manager, repository)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			e := editor{manager: manager, snapshot: snapshot, RunOptions: RunOptions{
				Repository: repository, Accessible: true, Input: newLineInput(test.input(snapshot)), Output: &output,
			}}
			if err := e.editReviews(); err != nil {
				t.Fatal(err)
			}
			test.draft.target = configuration.ScopeRepository
			if accessible := e.draftSet().reviews; accessible != test.draft {
				t.Fatalf("accessible draft = %#v, want %#v", accessible, test.draft)
			}
			plan, preview, err := buildPlan(manager, repository, nil, planRequest{kind: planReviews, review: reviewPlanRequest{draft: test.draft}})
			if err != nil || !plan.Valid() || len(plan.Changes()) == 0 {
				t.Fatalf("plan = valid %v, %d changes, error %v", plan.Valid(), len(plan.Changes()), err)
			}
			var interactive bytes.Buffer
			if err := configuration.RenderPlanHuman(&interactive, plan); err != nil {
				t.Fatal(err)
			}
			if err := preview(&interactive); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), interactive.String()) {
				t.Fatalf("accessible plan differs from interactive plan.\ninteractive:\n%s\naccessible:\n%s", interactive.String(), output.String())
			}
		})
	}
}

func TestHubReviewBrowserMovesHighlightedRowWithinItsGroup(t *testing.T) {
	model := New(reviewTestSnapshot(&configuration.ReviewSelection{
		ConcurrencyLimit: 2,
		Global:           []configuration.SelectionItem{{Profile: "one"}, {Profile: "two"}},
		Repository:       []configuration.SelectionItem{{Party: "three"}},
	}, nil))
	model.area = 3
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'm', Text: "m"}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: tea.KeyEnter})
	want := reviewFormDraft{
		target: configuration.ScopeRepository, operation: reviewMove, to: 1,
		reviewEntry: reviewEntry{group: configuration.ScopeGlobal, index: 0, expect: configuration.SelectionItem{Profile: "one"}},
	}
	if model.drafts.reviews != want || model.formKind != formReviewFields {
		t.Fatalf("row-driven move draft = %#v in %v, want %#v", model.drafts.reviews, model.formKind, want)
	}

	model.toMenu()
	model.drafts.reviews = reviewFormDraft{}
	model.area = 3
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'm', Text: "m"}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.drafts.reviews != (reviewFormDraft{}) || model.status != "Move within one group." {
		t.Fatalf("cross-group move = %#v, status %q", model.drafts.reviews, model.status)
	}
}

func TestHubReviewBrowserEditsTheHighlightedDuplicateEntry(t *testing.T) {
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	key := func(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: string(code)} }
	tests := []struct {
		name string
		keys []tea.KeyPressMsg
		want []configuration.SelectionItem
	}{
		{name: "remove", keys: []tea.KeyPressMsg{enter, key('j'), key('j'), key('j'), key('r')}, want: []configuration.SelectionItem{sharedProfile, dailyParty}},
		{name: "move", keys: []tea.KeyPressMsg{enter, key('j'), key('j'), key('j'), key('m'), key('k'), enter}, want: []configuration.SelectionItem{sharedProfile, sharedProfile, dailyParty}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, repository := reviewFixture(t)
			publishScopedSelection(t, manager, repository, configuration.ScopeRepository, configuration.ReviewSelection{
				ConcurrencyLimit: 2, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{sharedProfile, dailyParty, sharedProfile},
			})
			snapshot, err := buildSnapshot(manager, repository)
			if err != nil {
				t.Fatal(err)
			}
			model := New(snapshot)
			model.area = 3
			model = pressHubKeys(t, model, test.keys...)
			if selection := publishReviewEdit(t, manager, repository, model.drafts.reviews); !reflect.DeepEqual(selection.Repository, test.want) {
				t.Fatalf("Repository selection = %#v, want %#v", selection.Repository, test.want)
			}
		})
	}
}

func pressHubKeys(t *testing.T, model Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, key := range keys {
		updated, _ := model.Update(key)
		model = requireHubModel(t, updated)
	}
	return model
}
