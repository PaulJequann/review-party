package configurationhub

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

func TestHubLabelsEverySearchResultWithItsScope(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{
		{Scope: "global", Kind: "Profile", Name: "quality", Detail: "codex / large"},
		{Scope: "repository", Kind: "Profile", Name: "quality", Detail: "grok / fast"},
	}})
	model.area = 1
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)
	view := model.Render()
	for _, want := range []string{"[global] quality", "[repository] quality"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
}

func TestHubNavigationAndCancellationDoNotMutateSnapshot(t *testing.T) {
	snapshot := Snapshot{Repository: "/repo", Overview: []string{"Repository Reviews: not configured"}}
	model := New(snapshot)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if command != nil {
		t.Fatal("navigation unexpectedly scheduled a command")
	}
	updatedModel, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	if updatedModel.area != 1 {
		t.Fatalf("area = %d, want 1", updatedModel.area)
	}
	if got := updatedModel.snapshot.Overview[0]; got != snapshot.Overview[0] {
		t.Fatalf("snapshot changed to %q", got)
	}
}

func TestHubEnterOpensProfileBrowserAndNewOpensForm(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{
		{Scope: "global", Kind: itemProfile, Name: "quality", Detail: "codex / large"},
	}})
	model.area = 1

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command != nil {
		t.Fatal("opening Profiles unexpectedly scheduled a command")
	}
	updatedModel, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	if updatedModel.view != viewBrowser {
		t.Fatalf("view = %v, want browser view", updatedModel.view)
	}
	if !strings.Contains(updatedModel.Render(), "quality") {
		t.Fatalf("profile browser did not open:\n%s", updatedModel.Render())
	}
	updated, _ = updatedModel.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	updatedModel = requireHubModel(t, updated)
	if updatedModel.view != viewForm {
		t.Fatalf("new Profile view = %v, want form view", updatedModel.view)
	}
}

func TestHubReviewBrowserUsesHighlightedRowForRemoval(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{
		{Scope: "global", Kind: itemReview, Name: "one", Detail: "Profile"},
		{Scope: "global", Kind: itemReview, Name: "two", Detail: "Profile"},
	}})
	model.area = 3
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = requireHubModel(t, updated)
	if model.drafts.reviews.index != "1" || model.drafts.reviews.scope != "global" {
		t.Fatalf("row-driven removal draft = %#v", model.drafts.reviews)
	}
	if model.formKind != formReviewFields {
		t.Fatalf("form kind = %v, want review fields", model.formKind)
	}
}

func TestHubActionBarShowsDraftIndicatorInBrowser(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{{Kind: itemProfile, Scope: "global", Name: "quality"}}})
	model.area = 1
	model.drafts.profile = completeProfileDraft()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)
	if !strings.Contains(model.Render(), "draft") && !strings.Contains(model.Render(), "⏸") {
		t.Fatalf("browser action bar has no draft indicator:\n%s", model.Render())
	}
}

func TestPartyMultiSelectPreservesScopedExpansionOrder(t *testing.T) {
	draft, err := partyDraftFromForm(partyFormDraft{
		scope: "repository", name: "all", limit: "2",
		profileRefs: []string{"global:quality", "repository:fast"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Profiles) != 2 {
		t.Fatalf("profiles = %#v, want two references", draft.Profiles)
	}
	if draft.Profiles[0].Scope != configuration.ScopeGlobal || draft.Profiles[0].Profile != "quality" {
		t.Fatalf("first profile = %#v", draft.Profiles[0])
	}
	if draft.Profiles[1].Scope != configuration.ScopeRepository || draft.Profiles[1].Profile != "fast" {
		t.Fatalf("second profile = %#v", draft.Profiles[1])
	}
}

func TestConcurrencyValidationRequiresPositiveInteger(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "two"} {
		if err := validatePositiveInteger(value); err == nil {
			t.Fatalf("validatePositiveInteger(%q) succeeded", value)
		}
	}
	if err := validatePositiveInteger("3"); err != nil {
		t.Fatalf("positive integer rejected: %v", err)
	}
}

func TestHubUsesOneAltScreenForMenuAndForms(t *testing.T) {
	model := New(Snapshot{})
	view := model.View()
	if !view.AltScreen {
		t.Fatal("menu view did not request the alt screen")
	}
	if view.WindowTitle != "Review Party Configuration Hub" {
		t.Fatalf("menu window title = %q", view.WindowTitle)
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)
	view = model.View()
	if !view.AltScreen || model.view != viewBrowser {
		t.Fatal("browser view did not request the alt screen")
	}
	if view.WindowTitle != "Review Party Configuration Hub" {
		t.Fatalf("browser window title = %q", view.WindowTitle)
	}
}

func TestHubFormEscapeReturnsToMenuWithDrafts(t *testing.T) {
	model := New(Snapshot{Repository: "/repo"})
	model.area = 1
	model.drafts.profile = configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "quality", Instructions: "keep this draft",
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated, _ = requireHubModel(t, updated).Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = requireHubModel(t, updated)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command != nil {
		t.Fatal("form Escape unexpectedly scheduled a command")
	}
	model = requireHubModel(t, updated)
	if model.view != viewMenu {
		t.Fatalf("view = %v, want menu view", model.view)
	}
	if model.drafts.profile.Instructions != "keep this draft" {
		t.Fatalf("draft changed after abort: %#v", model.drafts.profile)
	}
}

func TestHubFormQDoesNotQuit(t *testing.T) {
	model := New(Snapshot{})
	model.area = 1
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated, _ = requireHubModel(t, updated).Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = requireHubModel(t, updated)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if command != nil {
		if _, quit := command().(tea.QuitMsg); quit {
			t.Fatal("form q scheduled tea.Quit")
		}
	}
	model = requireHubModel(t, updated)
	if model.view != viewForm {
		t.Fatalf("view = %v, want form view", model.view)
	}
}

func TestFormAdapterMapsHuhAbortState(t *testing.T) {
	adapter := newFormAdapter(
		[]huh.Field{huh.NewInput().Title("Name")},
		40,
		12,
		huh.ThemeFunc(huh.ThemeCharm),
	)
	adapter.Init()
	updated, command := adapter.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command != nil {
		t.Fatal("adapter Escape unexpectedly returned a command")
	}
	if updated.State() != huh.StateAborted {
		t.Fatalf("adapter state = %v, want %v", updated.State(), huh.StateAborted)
	}
}

func TestGroupedProfileFieldsRefreshDependentInputs(t *testing.T) {
	model := New(Snapshot{})
	model.session = &formSession{profile: profileFormState{
		draft: completeProfileDraft(), target: string(configuration.ScopeGlobal),
	}}
	model.openProfileFieldsForm()
	state := &model.session.profile

	state.accessors["reviewer"].Set("grok")
	requireDependentProfileFieldsEmpty(t, state)
	requireProfileInputsEmpty(t, state)

	state.accessors["model"].Set("new-model")
	requireModelChangeReset(t, state)
}

func TestHubPlansBeforeItPublishesAndPreviewsInProgram(t *testing.T) {
	var planned, published bool
	model := newPlanTestModel(&planned, &published)
	model = enterHubForm(t, model)
	model.session.copyName = "quality"
	model, planCommand := completeHubForm(t, model)
	if !planned {
		t.Fatal("copy form did not start planning")
	}
	model, _ = runHubCommand(t, model, planCommand)
	if model.view != viewPlanPreview {
		t.Fatalf("view = %v, want plan preview", model.view)
	}
	if published {
		t.Fatal("plan command published before confirmation")
	}

	updated, publishCommand := model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model = requireHubModel(t, updated)
	if published {
		t.Fatal("publish command ran while handling preview key")
	}
	model, _ = runHubCommand(t, model, publishCommand)
	if !published || model.view != viewMenu {
		t.Fatalf("publish transition = published %v, view %v", published, model.view)
	}
}

func TestHubPlanPreviewCancelRetainsDraft(t *testing.T) {
	var planned bool
	var published bool
	model := newPlanTestModel(&planned, &published)
	model.drafts.copyName = "quality"
	model = enterHubForm(t, model)
	model.session.copyName = "quality"
	model, command := completeHubForm(t, model)
	model, _ = runHubCommand(t, model, command)
	updated, publishCommand := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = requireHubModel(t, updated)
	if publishCommand != nil {
		t.Fatal("cancel transition scheduled publication")
	}
	if published {
		t.Fatal("cancel transition published the draft")
	}
	if model.drafts.copyName != "quality" {
		t.Fatalf("cancel transition changed draft: %#v", model.drafts)
	}
}

func TestHubPlanPreviewFailureRetainsDraft(t *testing.T) {
	model := New(Snapshot{Repository: "/repo"})
	model.pendingKind = planProfile
	model.drafts.profile = completeProfileDraft()
	model.planSummary = "profile"
	model.planWarnings = nil
	model.pending = func() tea.Msg { return publishResultMsg{kind: planProfile, err: errors.New("disk full")} }
	model.view = viewPlanPreview

	updated, command := model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(command())
	model = requireHubModel(t, updated)
	if model.view != viewForm {
		t.Fatalf("failed publication view = %v, want form", model.view)
	}
	if model.drafts.profile == (configuration.ProfileDraft{}) {
		t.Fatal("failed publication cleared the draft")
	}
	if !strings.Contains(model.outcome, "Publish failed: disk full") {
		t.Fatalf("failure outcome = %q", model.outcome)
	}
}

func TestHubPlanPreviewSuccessClearsOnlyPublishedDraft(t *testing.T) {
	model := New(Snapshot{Repository: "/repo"})
	model.pendingKind = planProfile
	model.drafts.profile = completeProfileDraft()
	model.drafts.party = partyFormDraft{scope: "global", name: "keep"}
	model.pending = func() tea.Msg { return publishResultMsg{kind: planProfile, snapshot: model.snapshot} }
	model.view = viewPlanPreview

	updated, command := model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(command())
	model = requireHubModel(t, updated)
	if model.drafts.profile != (configuration.ProfileDraft{}) {
		t.Fatalf("published profile draft retained: %#v", model.drafts.profile)
	}
	if model.drafts.party.name != "keep" {
		t.Fatalf("unpublished party draft changed: %#v", model.drafts.party)
	}
	if !strings.Contains(model.Render(), "✓ Published profile \"quality\"") {
		t.Fatalf("success banner missing from view:\n%s", model.Render())
	}
}

func requireDependentProfileFieldsEmpty(t *testing.T, state *profileFormState) {
	t.Helper()
	if state.draft.Model != "" {
		t.Fatalf("reviewer change left model: %q", state.draft.Model)
	}
	if state.draft.ReasoningEffort != "" {
		t.Fatalf("reviewer change left reasoning effort: %q", state.draft.ReasoningEffort)
	}
	if state.draft.AttemptDeadline != "" {
		t.Fatalf("reviewer change left dependent draft values: %#v", state.draft)
	}
}

func requireModelChangeReset(t *testing.T, state *profileFormState) {
	t.Helper()
	if state.draft.Model != "new-model" {
		t.Fatalf("model = %q, want new-model", state.draft.Model)
	}
	if state.draft.ReasoningEffort != "" {
		t.Fatalf("model change left reasoning effort: %q", state.draft.ReasoningEffort)
	}
	if state.draft.AttemptDeadline != "" {
		t.Fatalf("model change left attempt deadline: %q", state.draft.AttemptDeadline)
	}
}

func requireProfileInputsEmpty(t *testing.T, state *profileFormState) {
	t.Helper()
	for _, name := range []string{"model", "effort", "deadline"} {
		if value := state.accessors[name].input.GetValue(); value != "" {
			t.Fatalf("%s input = %q after reviewer change, want empty", name, value)
		}
	}
}

func newPlanTestModel(planned, published *bool) Model {
	model := New(Snapshot{Repository: "/repo"})
	model.area = 1
	snapshot := model.snapshot
	model.runtime = &hubRuntime{commands: hubCommands{
		plan: func(request planRequest) tea.Cmd {
			*planned = request.kind == planCopy
			return func() tea.Msg {
				return planReadyMsg{
					kind:    planCopy,
					summary: "copy profile",
					publish: func() tea.Msg {
						*published = true
						return publishResultMsg{kind: planCopy, snapshot: snapshot}
					},
				}
			}
		},
	}}
	return model
}

func enterHubForm(t *testing.T, model Model) Model {
	t.Helper()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	return requireHubModel(t, updated)
}

func completeHubForm(t *testing.T, model Model) (Model, tea.Cmd) {
	t.Helper()
	model.form.form.State = huh.StateCompleted
	updated, command := model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	return requireHubModel(t, updated), command
}

func runHubCommand(t *testing.T, model Model, command tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("Hub transition did not return a command")
	}
	updated, next := model.Update(command())
	return requireHubModel(t, updated), next
}

func requireHubModel(t *testing.T, value tea.Model) Model {
	t.Helper()
	model, ok := value.(Model)
	if !ok {
		t.Fatalf("updated model has type %T", value)
	}
	return model
}

func TestProfilePlanCommandUsesRepositoryForRepositoryProfile(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
	})
	repository := configuration.Repository(t.TempDir())
	request := planRequest{kind: planProfile, profile: configuration.ProfileDraft{
		Target: configuration.ScopeRepository, Name: "quality", Reviewer: "codex", Model: "luna",
		ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review carefully.",
	}}
	plan, err := buildPlan(manager, repository, nil, request)
	if err != nil {
		t.Fatalf("build profile plan: %v", err)
	}
	if !plan.Valid() {
		t.Fatalf("profile plan invalid: %s", plan.Reason())
	}
	wantPrefix := filepath.Join(string(repository), ".reviewparty")
	if len(plan.Paths()) == 0 || !strings.HasPrefix(plan.Paths()[0], wantPrefix) {
		t.Fatalf("profile plan paths = %#v, want repository path under %q", plan.Paths(), wantPrefix)
	}
}

func TestHubRendersConfiguredParties(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{{Scope: "global", Kind: "Party", Name: "baseline", Detail: "2 Profiles"}}})
	model.area = 2
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := requireHubModel(t, updated).Render()
	if !strings.Contains(view, "[global] baseline") {
		t.Fatalf("party missing from view:\n%s", view)
	}
}

func TestHubRendersActionAreasAsActions(t *testing.T) {
	for _, test := range []struct {
		name string
		area int
		want string
	}{
		{name: "changes", area: 4, want: "unfinished drafts"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := New(Snapshot{})
			model.area = test.area
			view := model.Render()
			if !strings.Contains(view, test.want) || strings.Contains(view, "No matching items.") {
				t.Fatalf("action area view = %q", view)
			}
		})
	}
}

func TestHubSearchFromOverviewShowsMatchingScopedItems(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{{Scope: "global", Kind: "Party", Name: "baseline", Detail: "quality"}}})
	model.query = "baseline"
	view := model.Render()
	if !strings.Contains(view, "[global] baseline") {
		t.Fatalf("overview search result missing:\n%s", view)
	}
}

func TestHubSearchStartsFromOverviewRegardlessOfSelectedArea(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{
		{Scope: "global", Kind: "Party", Name: "baseline", Detail: "quality"},
	}})
	model.area = 2
	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	searched, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	if searched.area != 0 || !searched.searching {
		t.Fatalf("search state = %#v", searched)
	}
	searched.query = "baseline"
	if view := searched.Render(); !strings.Contains(view, "[global] baseline") {
		t.Fatalf("global search result missing:\n%s", view)
	}
}

func TestHubSearchCtrlCExits(t *testing.T) {
	model := New(Snapshot{})
	model.searching = true
	updated, command := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if command == nil {
		t.Fatal("search Ctrl-C did not schedule quit")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("search Ctrl-C command returned %T, want tea.QuitMsg", command())
	}
	if _, ok := updated.(Model); !ok {
		t.Fatalf("updated model has type %T", updated)
	}
}

func TestHubSearchTreatsQAsQuery(t *testing.T) {
	model := New(Snapshot{})
	model.searching = true
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if command != nil {
		t.Fatal("search q unexpectedly scheduled quit")
	}
	searched, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	if searched.query != "q" {
		t.Fatalf("search query = %q, want q", searched.query)
	}
}

func TestHubSearchEscapeClearsQuery(t *testing.T) {
	model := New(Snapshot{Overview: []string{"overview restored"}})
	model.searching = true
	model.query = "bugs"
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command != nil {
		t.Fatal("search Escape unexpectedly scheduled a command")
	}
	searched, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	if searched.searching || searched.query != "" {
		t.Fatalf("search state after Escape = %#v", searched)
	}
	if view := searched.Render(); !strings.Contains(view, "overview restored") {
		t.Fatalf("overview was not restored:\n%s", view)
	}
}

func TestOrdinaryExitConfirmationAbortReturnsToHub(t *testing.T) {
	editor := &editor{RunOptions: RunOptions{Context: context.Background()}}
	exit, err := editor.resolveExitConfirmation(false, huh.ErrUserAborted)
	if err != nil {
		t.Fatalf("resolve abort: %v", err)
	}
	if exit {
		t.Fatal("ordinary confirmation abort exited Hub")
	}
}

func TestAccessibleAbortExitsWithoutWaitingForUnsignalledContext(t *testing.T) {
	if !(&editor{RunOptions: RunOptions{Context: context.Background(), Accessible: true}}).abortExits() {
		t.Fatal("accessible abort did not exit")
	}
}

func TestHubSearchAcceptsAndDeletesUnicodeRunes(t *testing.T) {
	model := New(Snapshot{})
	model.searching = true
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'é', Text: "é"})
	var ok bool
	model, ok = updated.(Model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	if model.query != "é" {
		t.Fatalf("query = %q", model.query)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	model, ok = updated.(Model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	if model.query != "" {
		t.Fatalf("query after backspace = %q", model.query)
	}
}

func TestHubSearchMatchesScopeAndDetail(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{
		{Scope: "global", Kind: "Profile", Name: "bugs", Detail: "codex / luna"},
		{Scope: "repository", Kind: "Profile", Name: "docs", Detail: "grok / fast"},
	}})
	model.area = 1
	model.query = "luna"
	view := model.Render()
	if !strings.Contains(view, "[global] bugs") || strings.Contains(view, "[repository] docs") {
		t.Fatalf("unexpected filtered view:\n%s", view)
	}
}
