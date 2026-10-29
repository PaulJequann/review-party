package configurationhub

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reviewparty/internal/configuration"
)

func reviewTestSnapshot(repository, global *configuration.ReviewSelection) Snapshot {
	snapshot := Snapshot{Repository: "/repo", Targets: hubTargets("/repo")}
	snapshot.Reviews = ReviewsView{Files: []ReviewFile{
		{Target: configuration.ScopeRepository, Path: "/repo/.reviewparty/config.json", Present: repository != nil, Selection: repository},
		{Target: configuration.ScopeGlobal, Path: "/home/user/.config/reviewparty/config.json", Present: global != nil, Selection: global},
	}}
	switch {
	case repository != nil:
		snapshot.Reviews.InForce = configuration.ScopeRepository
	case global != nil:
		snapshot.Reviews.InForce = configuration.ScopeGlobal
	}
	for _, file := range snapshot.Reviews.Files {
		appendHubReviewFile(&snapshot, file)
	}
	return snapshot
}

var nightlyParty = configuration.PartyDraft{
	Name: "nightly", ConcurrencyLimit: 2,
	Profiles: []configuration.ProfileReference{{Scope: configuration.ScopeGlobal, Profile: "shared"}},
}

func reviewRows(snapshot Snapshot, target configuration.Scope) []Item {
	var rows []Item
	for _, item := range snapshot.Items {
		if item.Target == target && (item.Kind == itemReview || item.Kind == itemReviewHeader) {
			rows = append(rows, item)
		}
	}
	return rows
}

func TestSnapshotReviewsViewReflectsEachRepositoryState(t *testing.T) {
	global := configuration.ReviewSelection{ConcurrencyLimit: 3, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{}}
	own := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{dailyParty}}
	tests := []struct {
		name       string
		prepare    func(t *testing.T, manager *configuration.Manager, repository configuration.Repository)
		inForce    configuration.Scope
		repository string
		globalLine string
		rows       int
	}{
		{
			name:       "no repository file",
			prepare:    func(*testing.T, *configuration.Manager, configuration.Repository) {},
			inForce:    configuration.ScopeGlobal,
			repository: "no file; inherits Global Configuration",
			globalLine: "● in force · 1 selected · concurrency 3",
			rows:       1,
		},
		{
			name: "repository file without Reviews",
			prepare: func(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
				plan, err := manager.Plan(repository, []configuration.Intent{configuration.SetDefaultReviewer{Target: configuration.ScopeRepository, Reviewer: "codex"}})
				if err != nil {
					t.Fatal(err)
				}
				if err := manager.Publish(plan); err != nil {
					t.Fatal(err)
				}
			},
			inForce:    configuration.ScopeGlobal,
			repository: "inherits Global Configuration",
			globalLine: "● in force · 1 selected · concurrency 3",
			rows:       1,
		},
		{
			name: "repository declares its own",
			prepare: func(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
				publishScopedSelection(t, manager, repository, configuration.ScopeRepository, own)
			},
			inForce:    configuration.ScopeRepository,
			repository: "● in force · 1 selected · concurrency 1",
			globalLine: "replaced here by Repository Configuration · 1 selected · concurrency 3",
			rows:       2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, repository := reviewFixture(t)
			publishScopedSelection(t, manager, repository, configuration.ScopeGlobal, global)
			test.prepare(t, manager, repository)
			snapshot, err := buildSnapshot(manager, repository)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snapshot.Targets, []configuration.Scope{configuration.ScopeRepository, configuration.ScopeGlobal}) || !snapshot.writes(configuration.ScopeGlobal) {
				t.Fatalf("targets = %v", snapshot.Targets)
			}
			if snapshot.Reviews.InForce != test.inForce {
				t.Fatalf("in force = %q, want %q", snapshot.Reviews.InForce, test.inForce)
			}
			for _, want := range []string{"Repository Configuration Reviews: " + test.repository, "Global Configuration Reviews: " + test.globalLine} {
				if !containsLine(snapshot.Overview, want) {
					t.Fatalf("overview lacks %q:\n%s", want, strings.Join(snapshot.Overview, "\n"))
				}
			}
			rows := reviewRows(snapshot, configuration.ScopeRepository)
			if len(rows) != test.rows || rows[0].Kind != itemReviewHeader || rows[0].Detail != test.repository {
				t.Fatalf("repository rows = %#v", rows)
			}
			globalRows := reviewRows(snapshot, configuration.ScopeGlobal)
			if len(globalRows) != 2 || globalRows[1].Index != 0 || globalRows[1].Scope != "global" || globalRows[1].Selection != sharedProfile {
				t.Fatalf("global rows = %#v", globalRows)
			}
		})
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func TestReviewHeaderStateDescribesEachFile(t *testing.T) {
	selection := &configuration.ReviewSelection{ConcurrencyLimit: 4, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{dailyParty}}
	tests := []struct {
		name    string
		inForce configuration.Scope
		file    ReviewFile
		want    string
	}{
		{name: "absent repository file", file: ReviewFile{Target: configuration.ScopeRepository}, want: "no file; inherits Global Configuration"},
		{name: "absent global file", file: ReviewFile{Target: configuration.ScopeGlobal}, want: "no file"},
		{name: "repository file without Reviews", file: ReviewFile{Target: configuration.ScopeRepository, Present: true}, want: "inherits Global Configuration"},
		{name: "global file without Reviews", file: ReviewFile{Target: configuration.ScopeGlobal, Present: true}, want: "no Review selection"},
		{name: "in force", inForce: configuration.ScopeGlobal, file: ReviewFile{Target: configuration.ScopeGlobal, Present: true, Selection: selection}, want: "● in force · 2 selected · concurrency 4"},
		{name: "replaced", inForce: configuration.ScopeRepository, file: ReviewFile{Target: configuration.ScopeGlobal, Present: true, Selection: selection}, want: "replaced here by Repository Configuration · 2 selected · concurrency 4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := (ReviewsView{InForce: test.inForce}).headerState(test.file); got != test.want {
				t.Fatalf("header state = %q, want %q", got, test.want)
			}
		})
	}
}

func TestChoicesForGlobalTargetOmitsRepositoryDefinitions(t *testing.T) {
	manager, repository := reviewFixture(t)
	snapshot, err := buildSnapshot(manager, repository)
	if err != nil {
		t.Fatal(err)
	}
	global := snapshot.ChoicesFor(configuration.ScopeGlobal)
	want := []configuration.SelectedDefinition{{Kind: configuration.ItemProfile, Name: "shared", Scope: configuration.ScopeGlobal}}
	if !reflect.DeepEqual(global, want) {
		t.Fatalf("global choices = %#v, want %#v", global, want)
	}
	if !reflect.DeepEqual(snapshot.ChoicesFor(configuration.ScopeRepository), snapshot.ReviewChoices) {
		t.Fatalf("repository choices = %#v, want every choice", snapshot.ChoicesFor(configuration.ScopeRepository))
	}
}

func TestFilteredReviewRemovalHitsHighlightedItem(t *testing.T) {
	model := New(reviewTestSnapshot(&configuration.ReviewSelection{
		ConcurrencyLimit: 2, Global: []configuration.SelectionItem{{Profile: "one"}, {Profile: "two"}, {Profile: "three"}},
	}, nil))
	model.area = 3
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, letter := range "three" {
		model = pressHubKeys(t, model, tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	view := model.Render()
	if strings.Contains(view, "one") || !strings.Contains(view, "Repository Configuration") || !strings.Contains(view, "three") {
		t.Fatalf("filter hid the file headers or kept unmatched rows:\n%s", view)
	}
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'r', Text: "r"})
	want := reviewEntry{group: configuration.ScopeGlobal, index: 2, expect: configuration.SelectionItem{Profile: "three"}}
	if model.drafts.reviews.reviewEntry != want {
		t.Fatalf("filtered removal draft = %#v, want %#v", model.drafts.reviews.reviewEntry, want)
	}
}

func TestReviewDraftKeepsItsTargetWhenCursorMoves(t *testing.T) {
	snapshot := reviewTestSnapshot(&configuration.ReviewSelection{ConcurrencyLimit: 2, Global: []configuration.SelectionItem{sharedProfile}}, nil)
	snapshot.ReviewChoices = []configuration.SelectedDefinition{{Kind: configuration.ItemProfile, Name: "shared", Scope: configuration.ScopeGlobal}}
	model := New(snapshot)
	var planned planRequest
	model.runtime = &hubRuntime{commands: hubCommands{plan: func(request planRequest) tea.Cmd {
		planned = request
		return nil
	}}}
	model.area = 3
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if row, _ := model.cursorRow(); row.Target != configuration.ScopeGlobal {
		t.Fatalf("cursor row = %#v, want the Global header", row)
	}
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if model.formKind != formReviewFields || model.session.reviews.target != configuration.ScopeGlobal {
		t.Fatalf("add on the Global header opened %v for %q", model.formKind, model.session.reviews.target)
	}
	model.cursor = 0
	model.session.reviews.choice = snapshot.ReviewChoices[0]
	model, _ = completeHubForm(t, model)
	if planned.kind != planReviews || planned.review.draft.target != configuration.ScopeGlobal || planned.review.draft.choice != snapshot.ReviewChoices[0] {
		t.Fatalf("planned draft = %#v, want a Global add", planned.review.draft)
	}
}

func TestGlobalReviewEditsThroughTheInteractiveHost(t *testing.T) {
	manager, repository := reviewFixture(t)
	publishGlobalParty(t, manager, nightlyParty)
	global := configuration.SelectedDefinition{Kind: configuration.ItemProfile, Name: "shared", Scope: configuration.ScopeGlobal}
	model := hubModelFor(t, manager, repository)

	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if model.formKind != formReviewFields {
		t.Fatalf("form kind = %v, want review fields", model.formKind)
	}
	model.session.reviews.choice = global
	model = publishHubForm(t, model)
	if got := globalReviews(t, manager); !reflect.DeepEqual(got.Global, []configuration.SelectionItem{sharedProfile}) {
		t.Fatalf("Global file after add = %#v", got)
	}
	if !strings.Contains(model.outcome, "Published Global Configuration Reviews") {
		t.Fatalf("outcome = %q", model.outcome)
	}

	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'a', Text: "a"})
	model.session.reviews.choice = configuration.SelectedDefinition{Kind: configuration.ItemParty, Name: "nightly", Scope: configuration.ScopeGlobal}
	model = publishHubForm(t, model)

	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'm', Text: "m"}, tea.KeyPressMsg{Code: 'k', Text: "k"})
	model = publishHubPlan(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := globalReviews(t, manager); !reflect.DeepEqual(got.Global, []configuration.SelectionItem{{Party: "nightly"}, sharedProfile}) {
		t.Fatalf("Global file after move = %#v", got)
	}

	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'c', Text: "c"})
	model.session.reviews.concurrency = "5"
	model = publishHubForm(t, model)
	if got := globalReviews(t, manager); got.ConcurrencyLimit != 5 {
		t.Fatalf("Global file after concurrency = %#v", got)
	}

	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = publishHubPlan(t, model, tea.KeyPressMsg{Code: 'r', Text: "r"})
	if got := globalReviews(t, manager); !reflect.DeepEqual(got.Global, []configuration.SelectionItem{sharedProfile}) {
		t.Fatalf("Global file after remove = %#v", got)
	}
	if loaded, err := manager.Load(repository); err != nil || loaded.Repository.Document.Reviews != nil {
		t.Fatalf("Global edits touched the Repository file: %#v, %v", loaded.Repository.Document.Reviews, err)
	}
}

func TestGlobalReviewEditsThroughTheAccessibleHost(t *testing.T) {
	manager, repository := reviewFixture(t)
	publishGlobalParty(t, manager, nightlyParty)
	steps := []struct {
		input string
		want  configuration.ReviewSelection
	}{
		{input: "2\n1\n1\ny\n", want: configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{}}},
		{input: "2\n1\n2\ny\n", want: configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{sharedProfile, {Party: "nightly"}}, Repository: []configuration.SelectionItem{}}},
		{input: "2\n3\n2\n1\ny\n", want: configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Party: "nightly"}, sharedProfile}, Repository: []configuration.SelectionItem{}}},
		{input: "2\n4\n6\ny\n", want: configuration.ReviewSelection{ConcurrencyLimit: 6, Global: []configuration.SelectionItem{{Party: "nightly"}, sharedProfile}, Repository: []configuration.SelectionItem{}}},
		{input: "2\n2\n1\ny\n", want: configuration.ReviewSelection{ConcurrencyLimit: 6, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{}}},
	}
	for index, step := range steps {
		output := runAccessibleReviews(t, manager, repository, step.input)
		if !strings.Contains(output, "Configuration file") || !strings.Contains(output, "Global Configuration") {
			t.Fatalf("step %d did not offer the file prompt:\n%s", index, output)
		}
		if got := globalReviews(t, manager); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("step %d (%q) Global file = %#v, want %#v", index, step.input, got, step.want)
		}
	}
	output := runAccessibleReviews(t, manager, repository, "2\n1\n2\nn\n")
	if strings.Contains(output, "[repository] profile shared") || !strings.Contains(output, "[global] party nightly") {
		t.Fatalf("Global add offered a Repository definition:\n%s", output)
	}
}

func TestRepositoryInheritsGlobalThroughBothHosts(t *testing.T) {
	own := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{dailyParty}}
	t.Run("interactive", func(t *testing.T) {
		manager, repository := reviewFixture(t)
		publishScopedSelection(t, manager, repository, configuration.ScopeRepository, own)
		model := hubModelFor(t, manager, repository)
		model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
		if hints := model.cursorReviewKeys(); hints[len(hints)-1].key != "i" {
			t.Fatalf("Repository header hints = %#v, want inherit last", hints)
		}
		model = publishHubPlan(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
		if loaded, err := manager.Load(repository); err != nil || loaded.Repository.Document.Reviews != nil {
			t.Fatalf("Repository file still declares Reviews: %#v, %v", loaded.Repository.Document.Reviews, err)
		}
		if !strings.Contains(model.outcome, "Published Repository Configuration Reviews") {
			t.Fatalf("outcome = %q", model.outcome)
		}
		model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
		for _, hint := range model.cursorReviewKeys() {
			if hint.key == "i" {
				t.Fatalf("inheriting header still offers inherit: %#v", model.cursorReviewKeys())
			}
		}
		model, command := pressHubKey(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
		if command != nil || model.view != viewBrowser || model.status != "Inherit Global Configuration does not apply to this row." {
			t.Fatalf("repeated inherit: view %v, status %q, command %v", model.view, model.status, command != nil)
		}
	})
	t.Run("accessible", func(t *testing.T) {
		manager, repository := reviewFixture(t)
		publishScopedSelection(t, manager, repository, configuration.ScopeRepository, own)
		output := runAccessibleReviews(t, manager, repository, "1\n5\ny\n")
		if !strings.Contains(output, "Inherit Global Configuration") {
			t.Fatalf("accessible operations omit inherit:\n%s", output)
		}
		if loaded, err := manager.Load(repository); err != nil || loaded.Repository.Document.Reviews != nil {
			t.Fatalf("Repository file still declares Reviews: %#v, %v", loaded.Repository.Document.Reviews, err)
		}
		output = runAccessibleReviews(t, manager, repository, "1\n1\n1\nn\n")
		if strings.Contains(output, "Inherit Global Configuration") || strings.Contains(output, "Remove item") {
			t.Fatalf("inheriting file offers inherit or remove:\n%s", output)
		}
	})
}

func TestGlobalEditWarnsWhenRepositoryReplacesIt(t *testing.T) {
	manager, repository := reviewFixture(t)
	publishScopedSelection(t, manager, repository, configuration.ScopeRepository, configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{dailyParty}})
	draft := reviewFormDraft{target: configuration.ScopeGlobal, operation: reviewAdd, choice: configuration.SelectedDefinition{Kind: configuration.ItemProfile, Name: "shared", Scope: configuration.ScopeGlobal}}
	const warning = "Repository Configuration replaces the Global Review selection here, so this change to it applies only to repositories without their own."
	message := planRequestCommand(context.Background(), manager, repository, nil, planRequest{kind: planReviews, review: reviewPlanRequest{draft: draft}})()
	ready, ok := message.(planReadyMsg)
	if !ok || len(ready.warnings) != 1 || ready.warnings[0] != warning {
		t.Fatalf("interactive plan = %#v", message)
	}
	output := runAccessibleReviews(t, manager, repository, "2\n1\n1\nn\n")
	if !strings.Contains(output, warning) {
		t.Fatalf("accessible plan lacks the precedence warning:\n%s", output)
	}
}

func TestUnchangedReviewEditIsRefusedWithAFact(t *testing.T) {
	manager, repository := reviewFixture(t)
	publishScopedSelection(t, manager, repository, configuration.ScopeRepository, configuration.ReviewSelection{ConcurrencyLimit: 3, Global: []configuration.SelectionItem{}, Repository: []configuration.SelectionItem{dailyParty}})
	const want = "Repository Configuration already holds this Review selection."
	draft := reviewFormDraft{target: configuration.ScopeRepository, operation: reviewConcurrency, concurrency: "3"}
	message := planRequestCommand(context.Background(), manager, repository, nil, planRequest{kind: planReviews, review: reviewPlanRequest{draft: draft}})()
	if failed, ok := message.(planFailedMsg); !ok || failed.err.Error() != want {
		t.Fatalf("interactive plan = %#v", message)
	}
	snapshot, err := buildSnapshot(manager, repository)
	if err != nil {
		t.Fatal(err)
	}
	e := editor{manager: manager, snapshot: snapshot, RunOptions: RunOptions{Repository: repository, Accessible: true, Input: newLineInput("1\n4\n3\n"), Output: &bytes.Buffer{}}}
	if err := e.editReviews(); err == nil || err.Error() != want {
		t.Fatalf("accessible plan = %v, want %q", err, want)
	}
}

func TestReviewsAreaRendersStackedFileSections(t *testing.T) {
	manager, repository := reviewFixture(t)
	publishScopedSelection(t, manager, repository, configuration.ScopeGlobal, configuration.ReviewSelection{ConcurrencyLimit: 3, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{}})
	inherits := hubModelFor(t, manager, repository)
	inherits = pressHubKeys(t, inherits, tea.KeyPressMsg{Code: tea.KeyEnter})
	list, _ := renderReviewList(inherits.reviewRows(), 0, 200, renderOptions{})
	view := inherits.Render()
	for _, want := range []string{
		"› Repository Configuration\n    no file; inherits Global Configuration\n",
		"  Global Configuration\n    ● in force · 1 selected · concurrency 3\n",
		"    [global] shared — Profile\n",
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("inheriting list lacks %q:\n%s", want, list)
		}
	}
	if strings.Contains(list, "Profiles and Parties") || !strings.Contains(view, "a add  c concurrency  / filter") || strings.Contains(view, "i inherit") {
		t.Fatalf("inheriting view shows a group label or inherit, or lacks the row hints:\n%s", view)
	}
	t.Logf("inheriting repository:\n%s", view)

	publishScopedSelection(t, manager, repository, configuration.ScopeRepository, configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{sharedProfile}, Repository: []configuration.SelectionItem{dailyParty}})
	declares := hubModelFor(t, manager, repository)
	declares = pressHubKeys(t, declares, tea.KeyPressMsg{Code: tea.KeyEnter})
	list, _ = renderReviewList(declares.reviewRows(), 0, 200, renderOptions{})
	view = declares.Render()
	want := strings.Join([]string{
		"› Repository Configuration",
		"    ● in force · 2 selected · concurrency 1",
		"    Global Profiles and Parties",
		"    [global] shared — Profile",
		"    Repository Profiles and Parties",
		"    [repository] daily — Party",
		"  Global Configuration",
		"    replaced here by Repository Configuration · 1 selected · concurrency 3",
		"    [global] shared — Profile",
		"",
	}, "\n")
	if list != want {
		t.Fatalf("declaring list =\n%s\nwant\n%s", list, want)
	}
	if !strings.Contains(view, "a add  c concurrency  i inherit Global  / filter") {
		t.Fatalf("declaring header lacks the inherit hint:\n%s", view)
	}
	t.Logf("declaring repository:\n%s", view)
}

func globalReviews(t *testing.T, manager *configuration.Manager) configuration.ReviewSelection {
	t.Helper()
	loaded, err := manager.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Global.Document.Reviews == nil {
		t.Fatal("Global file declares no Reviews")
	}
	return *loaded.Global.Document.Reviews
}

func hubModelFor(t *testing.T, manager *configuration.Manager, repository configuration.Repository) Model {
	t.Helper()
	snapshot, err := buildSnapshot(manager, repository)
	if err != nil {
		t.Fatal(err)
	}
	model := New(snapshot)
	model.runtime = newHubRuntime(RunOptions{Context: context.Background(), Repository: repository}, manager)
	model.width, model.height = 120, 30
	model.area = 3
	return model
}

func pressHubKey(t *testing.T, model Model, key tea.KeyPressMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, command := model.Update(key)
	return requireHubModel(t, updated), command
}

func publishHubForm(t *testing.T, model Model) Model {
	t.Helper()
	model, command := completeHubForm(t, model)
	return publishHubPlanCommand(t, model, command)
}

func publishHubPlan(t *testing.T, model Model, key tea.KeyPressMsg) Model {
	t.Helper()
	model, command := pressHubKey(t, model, key)
	return publishHubPlanCommand(t, model, command)
}

func publishHubPlanCommand(t *testing.T, model Model, command tea.Cmd) Model {
	t.Helper()
	model, _ = runHubCommand(t, model, command)
	if model.view != viewPlanPreview {
		t.Fatalf("view = %v after planning, status %q", model.view, model.status)
	}
	updated, publish := model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model, _ = runHubCommand(t, requireHubModel(t, updated), publish)
	if !model.outcomeGood {
		t.Fatalf("publish outcome = %q, status %q", model.outcome, model.status)
	}
	return model
}

func runAccessibleReviews(t *testing.T, manager *configuration.Manager, repository configuration.Repository, input string) string {
	t.Helper()
	snapshot, err := buildSnapshot(manager, repository)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	e := editor{manager: manager, snapshot: snapshot, RunOptions: RunOptions{Repository: repository, Accessible: true, Input: newLineInput(input), Output: &output}}
	if err := e.editReviews(); err != nil {
		t.Fatalf("editReviews(%q): %v\n%s", input, err, output.String())
	}
	return output.String()
}
