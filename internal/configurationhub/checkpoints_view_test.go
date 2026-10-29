package configurationhub

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reviewparty/internal/configuration"
)

const (
	checkpointsArea   = 4
	enrollmentWarning = "Creating Repository Configuration enrolls this repository, so the Global pre-push Checkpoint now applies here."
	undeclaredNotice  = "Installed Checkpoint Integrations serve no declared Checkpoint; remove them with: review-party checkpoint uninstall --undeclared"
)

func checkpointsHubFixture(t *testing.T) (*configuration.Manager, configuration.Repository) {
	t.Helper()
	return configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}}), configuration.Repository(t.TempDir())
}

func judgedCheckpoint() configuration.Checkpoint {
	checkpoint := configuration.NewCheckpoint()
	checkpoint.Requirement, checkpoint.ReviewBudget = configuration.RequirementJudged, 4
	checkpoint.Integrations = []configuration.IntegrationName{configuration.IntegrationGit}
	return checkpoint
}

func publishIntents(t *testing.T, manager *configuration.Manager, repository configuration.Repository, intents ...configuration.Intent) {
	t.Helper()
	plan, err := manager.Plan(repository, intents)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func declareGlobalPrePush(t *testing.T, manager *configuration.Manager) {
	t.Helper()
	publishIntents(t, manager, "", configuration.SetCheckpoint{Target: configuration.ScopeGlobal, Name: configuration.CheckpointPrePush, Checkpoint: judgedCheckpoint()})
}

func enroll(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
	t.Helper()
	plan, err := manager.PlanEnrollment(repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func loadedFiles(t *testing.T, manager *configuration.Manager, repository configuration.Repository) configuration.Loaded {
	t.Helper()
	loaded, err := manager.Load(repository)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func checkpointsModel(t *testing.T, manager *configuration.Manager, repository configuration.Repository, check func() (string, error)) Model {
	t.Helper()
	model := hubModelFor(t, manager, repository)
	model.runtime = newHubRuntime(RunOptions{Context: context.Background(), Repository: repository, UndeclaredIntegrations: check}, manager)
	model.area = checkpointsArea
	return model
}

func runAccessibleCheckpoints(t *testing.T, manager *configuration.Manager, repository configuration.Repository, input string, check func() (string, error)) string {
	t.Helper()
	snapshot, err := buildSnapshot(manager, repository)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	e := editor{manager: manager, snapshot: snapshot, RunOptions: RunOptions{
		Repository: repository, Accessible: true, Input: newLineInput(input), Output: &output, UndeclaredIntegrations: check,
	}}
	if err := e.editCheckpoints(); err != nil {
		t.Fatalf("editCheckpoints(%q): %v\n%s", input, err, output.String())
	}
	return output.String()
}

func key(letter rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: letter, Text: string(letter)}
}

func TestCheckpointsViewDescribesEachFile(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, manager *configuration.Manager, repository configuration.Repository)
		repository string
		global     string
		inForce    int
	}{
		{
			name:       "nothing declared",
			prepare:    func(*testing.T, *configuration.Manager, configuration.Repository) {},
			repository: "no file: not enrolled",
			global:     "declares no Checkpoints",
		},
		{
			name: "Global declares, repository not enrolled",
			prepare: func(t *testing.T, manager *configuration.Manager, _ configuration.Repository) {
				declareGlobalPrePush(t, manager)
			},
			repository: "no file: not enrolled; the Global Checkpoints apply once enrolled",
			global:     "not in force here: this repository is not enrolled · pre-push",
		},
		{
			name: "enrolled repository inherits",
			prepare: func(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
				declareGlobalPrePush(t, manager)
				enroll(t, manager, repository)
			},
			repository: "inherits the Global Checkpoints",
			global:     "● in force · pre-push",
			inForce:    1,
		},
		{
			name: "repository opts out",
			prepare: func(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
				declareGlobalPrePush(t, manager)
				enroll(t, manager, repository)
				publishIntents(t, manager, repository, configuration.DeclareNoCheckpoints{})
			},
			repository: "opted out: declares no Checkpoints, so the Global Checkpoints do not apply",
			global:     "replaced here by Repository Configuration · pre-push",
		},
		{
			name: "repository declares its own",
			prepare: func(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
				declareGlobalPrePush(t, manager)
				publishIntents(t, manager, repository,
					configuration.SetCheckpoint{Target: configuration.ScopeRepository, Name: configuration.CheckpointPreCommit, Checkpoint: configuration.NewCheckpoint()},
					configuration.SetCheckpoint{Target: configuration.ScopeRepository, Name: configuration.CheckpointPrePush, Checkpoint: configuration.NewCheckpoint()})
			},
			repository: "● in force · pre-push, pre-commit; replaces the Global Checkpoints",
			global:     "replaced here by Repository Configuration · pre-push",
			inForce:    2,
		},
		{
			name: "repository declares its own with no Global Checkpoints",
			prepare: func(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
				publishIntents(t, manager, repository,
					configuration.SetCheckpoint{Target: configuration.ScopeRepository, Name: configuration.CheckpointPrePush, Checkpoint: configuration.NewCheckpoint()})
			},
			repository: "● in force · pre-push",
			global:     "declares no Checkpoints",
			inForce:    1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, repository := checkpointsHubFixture(t)
			test.prepare(t, manager, repository)
			model := checkpointsModel(t, manager, repository, nil)
			for _, want := range []string{"Repository Configuration Checkpoints: " + test.repository, "Global Configuration Checkpoints: " + test.global} {
				if !containsLine(model.snapshot.Overview, want) {
					t.Fatalf("overview lacks %q:\n%s", want, strings.Join(model.snapshot.Overview, "\n"))
				}
			}
			spec, _ := areaSpecFor(AreaCheckpoints)
			if got := model.menuAreaCount(spec); got != test.inForce {
				t.Fatalf("menu count = %d, want %d", got, test.inForce)
			}
			model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			rows := model.browserRows()
			if len(rows) != 6 || rows[0].Kind != itemCheckpointHeader || rows[0].Detail != test.repository ||
				rows[3].Target != configuration.ScopeGlobal || rows[3].Detail != test.global || rows[4].Name != string(configuration.CheckpointPrePush) {
				t.Fatalf("rows = %#v", rows)
			}
		})
	}
}

func TestCheckpointHeaderStateNamesGlobalReachWithoutARepository(t *testing.T) {
	view := CheckpointsView{Files: []CheckpointFile{{Target: configuration.ScopeGlobal, Present: true, Field: CheckpointsDeclared, Declared: map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPrePush: judgedCheckpoint()}}}}
	if got := view.headerState(view.Files[0]); got != "applies to every enrolled repository that declares no Checkpoints · pre-push" {
		t.Fatalf("Global-only header = %q", got)
	}
}

func TestCheckpointRowsWrapUnderTheirFileInANarrowPane(t *testing.T) {
	manager, repository := checkpointsHubFixture(t)
	model := pressHubKeys(t, checkpointsModel(t, manager, repository, nil), tea.KeyPressMsg{Code: tea.KeyEnter})
	list, _ := renderFileSections(model.browserRows(), 0, 30, renderOptions{})
	for _, want := range []string{
		"    [repository] pre-push —\n    not declared\n",
		"    [global] pre-commit — not\n    declared\n",
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("narrow list lacks %q:\n%s", want, list)
		}
	}
}

func TestCheckpointRowsSurviveTheFilterUnderTheirFileHeaders(t *testing.T) {
	manager, repository := checkpointsHubFixture(t)
	declareGlobalPrePush(t, manager)
	model := pressHubKeys(t, checkpointsModel(t, manager, repository, nil), tea.KeyPressMsg{Code: tea.KeyEnter}, key('/'))
	for _, letter := range "commit" {
		model = pressHubKeys(t, model, key(letter))
	}
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	var kinds []string
	for _, row := range model.browserRows() {
		kinds = append(kinds, string(row.Target)+" "+string(row.Kind)+" "+row.Name)
	}
	want := []string{
		"repository Checkpoint file Repository Configuration", "repository Checkpoint pre-commit",
		"global Checkpoint file Global Configuration", "global Checkpoint pre-commit",
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("filtered rows = %q, want %q", kinds, want)
	}
}

func TestUnofferedCheckpointKeySetsAStatusAndPlansNothing(t *testing.T) {
	manager, repository := checkpointsHubFixture(t)
	model := checkpointsModel(t, manager, repository, nil)
	planned := false
	model.runtime.commands.plan = func(planRequest) tea.Cmd {
		planned = true
		return nil
	}
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, test := range []struct {
		name   string
		cursor int
		key    rune
		want   string
	}{
		{name: "enroll or inherit on an unenrollable header", cursor: 0, key: 'i', want: "Enroll this repository or Inherit Global Checkpoints does not apply to this row."},
		{name: "opt out without a Repository file", cursor: 0, key: 'o', want: "Opt out of Global Checkpoints does not apply to this row."},
		{name: "remove an undeclared Checkpoint", cursor: 1, key: 'r', want: "Remove Checkpoint does not apply to this row."},
		{name: "opt out on the Global header", cursor: 3, key: 'o', want: "Opt out of Global Checkpoints does not apply to this row."},
	} {
		t.Run(test.name, func(t *testing.T) {
			model.cursor = test.cursor
			got, command := pressHubKey(t, model, key(test.key))
			if command != nil || planned || got.view != viewBrowser || got.status != test.want {
				t.Fatalf("view %v, status %q, command %v, planned %v", got.view, got.status, command != nil, planned)
			}
			if !strings.Contains(got.Render(), test.want) {
				t.Fatalf("view does not show the refusal:\n%s", got.Render())
			}
		})
	}
	model.cursor = 0
	var hints []string
	for _, binding := range model.cursorCheckpointKeys() {
		hints = append(hints, binding.help+" "+binding.label)
	}
	if !reflect.DeepEqual(hints, []string{"e/⏎ set"}) || !strings.Contains(model.Render(), "e/⏎ set") {
		t.Fatalf("unenrolled header hints = %q\n%s", hints, model.Render())
	}
}

func TestGlobalCheckpointDraftKeepsItsTargetWhenCursorMoves(t *testing.T) {
	manager, repository := checkpointsHubFixture(t)
	model := checkpointsModel(t, manager, repository, nil)
	var planned planRequest
	model.runtime.commands.plan = func(request planRequest) tea.Cmd {
		planned = request
		return nil
	}
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, key('j'), key('j'), key('j'), key('j'), key('e'))
	if model.formKind != formCheckpointFields || model.session.checkpoints.target != configuration.ScopeGlobal || model.session.checkpoints.name != configuration.CheckpointPrePush {
		t.Fatalf("e on the Global pre-push row opened %v for %#v", model.formKind, model.session.checkpoints)
	}
	if breadcrumb := model.renderBreadcrumb(renderOptions{}); breadcrumb != "Hub › Checkpoints › Global Configuration" {
		t.Fatalf("form breadcrumb = %q, want it to name the Global file", breadcrumb)
	}
	model.cursor = 0
	model.session.checkpoints.reviewBudget = "3"
	model, _ = completeHubForm(t, model)
	if planned.kind != planCheckpoints || planned.checkpoint.target != configuration.ScopeGlobal || planned.checkpoint.reviewBudget != "3" {
		t.Fatalf("planned = %#v, want a Global set", planned.checkpoint)
	}
}

func TestGlobalCheckpointSetNeverWritesRepositoryThroughEitherHost(t *testing.T) {
	want := map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPrePush: configuration.NewCheckpoint()}
	t.Run("interactive", func(t *testing.T) {
		manager, repository := checkpointsHubFixture(t)
		model := pressHubKeys(t, checkpointsModel(t, manager, repository, nil), tea.KeyPressMsg{Code: tea.KeyEnter}, key('j'), key('j'), key('j'), tea.KeyPressMsg{Code: tea.KeyEnter})
		if model.formKind != formCheckpointName || model.session.checkpoints.target != configuration.ScopeGlobal {
			t.Fatalf("enter on the Global header opened %v for %#v", model.formKind, model.session.checkpoints)
		}
		model, _ = completeHubForm(t, model)
		if model.formKind != formCheckpointFields {
			t.Fatalf("name form led to %v", model.formKind)
		}
		model = publishHubForm(t, model)
		requireGlobalOnly(t, manager, repository, want)
		if !strings.Contains(model.outcome, "Published Global Configuration Checkpoints") {
			t.Fatalf("outcome = %q", model.outcome)
		}
	})
	t.Run("accessible", func(t *testing.T) {
		manager, repository := checkpointsHubFixture(t)
		output := runAccessibleCheckpoints(t, manager, repository, "2\n1\n\n\n\n0\n3\n\n0\ny\n", nil)
		if !strings.Contains(output, "Global Configuration — declares no Checkpoints") {
			t.Fatalf("file prompt lacks the header state:\n%s", output)
		}
		requireGlobalOnly(t, manager, repository, want)
	})
}

func requireGlobalOnly(t *testing.T, manager *configuration.Manager, repository configuration.Repository, want map[configuration.CheckpointName]configuration.Checkpoint) {
	t.Helper()
	loaded := loadedFiles(t, manager, repository)
	if !reflect.DeepEqual(loaded.Global.Document.Checkpoints, want) {
		t.Fatalf("Global Checkpoints = %#v, want %#v", loaded.Global.Document.Checkpoints, want)
	}
	if loaded.Repository.Present {
		t.Fatalf("a Global set wrote the Repository file: %#v", loaded.Repository.Document)
	}
}

func TestEnrollCreatesTheRepositoryFileAndWarnsThroughBothHosts(t *testing.T) {
	t.Run("interactive", func(t *testing.T) {
		manager, repository := checkpointsHubFixture(t)
		declareGlobalPrePush(t, manager)
		model := pressHubKeys(t, checkpointsModel(t, manager, repository, nil), tea.KeyPressMsg{Code: tea.KeyEnter})
		if hints := model.cursorCheckpointKeys(); len(hints) != 2 || hints[1].label != "enroll" {
			t.Fatalf("unenrolled header hints = %#v", hints)
		}
		model, command := pressHubKey(t, model, key('i'))
		model, _ = runHubCommand(t, model, command)
		if model.view != viewPlanPreview || !reflect.DeepEqual(model.planWarnings, []string{enrollmentWarning}) {
			t.Fatalf("enroll preview: view %v, warnings %q, status %q", model.view, model.planWarnings, model.status)
		}
		model = publishHubPlanCommand(t, model, command)
		requireEnrolledAndInheriting(t, manager, repository)
	})
	t.Run("accessible", func(t *testing.T) {
		manager, repository := checkpointsHubFixture(t)
		declareGlobalPrePush(t, manager)
		output := runAccessibleCheckpoints(t, manager, repository, "1\n2\ny\n", nil)
		if !strings.Contains(output, "Enroll this repository") || !strings.Contains(output, enrollmentWarning) {
			t.Fatalf("accessible enroll lacks the operation or warning:\n%s", output)
		}
		requireEnrolledAndInheriting(t, manager, repository)
	})
}

func requireEnrolledAndInheriting(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(string(repository), ".reviewparty", "config.json"))
	if err != nil || string(content) != "{\n  \"schema_version\": 1\n}\n" {
		t.Fatalf("Repository file = %q, %v", content, err)
	}
	if checkpoints := loadedFiles(t, manager, repository).Checkpoints(); checkpoints.Source != configuration.SourceGlobal {
		t.Fatalf("Checkpoints in force come from %q, want Global", checkpoints.Source)
	}
}

func TestOptOutAndInheritThroughBothHosts(t *testing.T) {
	prepare := func(t *testing.T) (*configuration.Manager, configuration.Repository) {
		t.Helper()
		manager, repository := checkpointsHubFixture(t)
		declareGlobalPrePush(t, manager)
		enroll(t, manager, repository)
		return manager, repository
	}
	t.Run("interactive", func(t *testing.T) {
		manager, repository := prepare(t)
		model := pressHubKeys(t, checkpointsModel(t, manager, repository, nil), tea.KeyPressMsg{Code: tea.KeyEnter})
		model = publishHubPlan(t, model, key('o'))
		requireOptedOut(t, repository)
		model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
		if hints := model.cursorCheckpointKeys(); len(hints) != 2 || hints[1].label != "inherit Global" {
			t.Fatalf("opted-out header hints = %#v", hints)
		}
		model = publishHubPlan(t, model, key('i'))
		requireInheriting(t, manager, repository)
		if !strings.Contains(model.outcome, "Published Repository Configuration Checkpoints") {
			t.Fatalf("outcome = %q", model.outcome)
		}
	})
	t.Run("accessible", func(t *testing.T) {
		manager, repository := prepare(t)
		runAccessibleCheckpoints(t, manager, repository, "1\n2\ny\n", nil)
		requireOptedOut(t, repository)
		output := runAccessibleCheckpoints(t, manager, repository, "1\n2\ny\n", nil)
		if !strings.Contains(output, "Inherit Global Checkpoints") || strings.Contains(output, "Opt out of Global Checkpoints") {
			t.Fatalf("opted-out operations:\n%s", output)
		}
		requireInheriting(t, manager, repository)
	})
}

func requireOptedOut(t *testing.T, repository configuration.Repository) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(string(repository), ".reviewparty", "config.json"))
	if err != nil || !strings.Contains(string(content), "\"checkpoints\": {}") {
		t.Fatalf("Repository file after opt out = %q, %v", content, err)
	}
}

func requireInheriting(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
	t.Helper()
	if loaded := loadedFiles(t, manager, repository); loaded.Repository.Document.Checkpoints != nil || !loaded.Repository.Present {
		t.Fatalf("Repository file after inherit: present %v, checkpoints %#v", loaded.Repository.Present, loaded.Repository.Document.Checkpoints)
	}
}

func TestCheckpointSetPrefillsTheFileThenGlobalThenDefaults(t *testing.T) {
	manager, repository := checkpointsHubFixture(t)
	declareGlobalPrePush(t, manager)
	own := configuration.NewCheckpoint()
	own.UnreviewedLines = 7
	publishIntents(t, manager, repository, configuration.SetCheckpoint{Target: configuration.ScopeRepository, Name: configuration.CheckpointPreCommit, Checkpoint: own})
	model := checkpointsModel(t, manager, repository, nil)
	view := model.snapshot.Checkpoints
	fallback := configuration.NewCheckpoint()
	fallback.ReviewBudget = 9
	for _, test := range []struct {
		target configuration.Scope
		name   configuration.CheckpointName
		want   configuration.Checkpoint
	}{
		{target: configuration.ScopeRepository, name: configuration.CheckpointPreCommit, want: own},
		{target: configuration.ScopeRepository, name: configuration.CheckpointPrePush, want: judgedCheckpoint()},
		{target: configuration.ScopeGlobal, name: configuration.CheckpointPreCommit, want: fallback},
	} {
		draft := view.setDraft(test.target, test.name, fallback)
		got, err := draft.checkpoint()
		if err != nil || got.Summary(test.name) != test.want.Summary(test.name) || draft.target != test.target || draft.operation != checkpointSet {
			t.Fatalf("%s %s prefill = %#v, %v; want %#v", test.target, test.name, got, err, test.want)
		}
	}
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, key('j'), key('e'))
	got, err := model.session.checkpoints.checkpoint()
	if err != nil || model.formKind != formCheckpointFields || got.Summary(configuration.CheckpointPrePush) != judgedCheckpoint().Summary(configuration.CheckpointPrePush) {
		t.Fatalf("Repository pre-push form opened %v with %#v, %v; want the Global rules", model.formKind, got, err)
	}
}

func TestRepositoryCheckpointEditsMatchThroughBothHosts(t *testing.T) {
	prepare := func(t *testing.T) (*configuration.Manager, configuration.Repository) {
		t.Helper()
		manager, repository := checkpointsHubFixture(t)
		declareGlobalPrePush(t, manager)
		enroll(t, manager, repository)
		return manager, repository
	}
	interactiveManager, interactiveRepository := prepare(t)
	model := pressHubKeys(t, checkpointsModel(t, interactiveManager, interactiveRepository, nil), tea.KeyPressMsg{Code: tea.KeyEnter}, key('j'), key('e'))
	model.session.checkpoints.exemptions = "*.md"
	model = publishHubForm(t, model)
	set := repositoryFile(t, interactiveRepository)

	accessibleManager, accessibleRepository := prepare(t)
	output := runAccessibleCheckpoints(t, accessibleManager, accessibleRepository, "1\n1\n\n\n*.md\n0\n4\n\n0\ny\n", nil)
	if got := repositoryFile(t, accessibleRepository); got != set {
		t.Fatalf("hosts disagree after set:\ninteractive %s\naccessible %s\n%s", set, got, output)
	}
	want := judgedCheckpoint()
	want.ExemptPaths = []string{"*.md"}
	if got := loadedFiles(t, accessibleManager, accessibleRepository).Repository.Document.Checkpoints; !reflect.DeepEqual(got, map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPrePush: want}) {
		t.Fatalf("Repository Checkpoints = %#v", got)
	}

	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, key('j'))
	model = publishHubPlan(t, model, key('r'))
	runAccessibleCheckpoints(t, accessibleManager, accessibleRepository, "1\n2\n1\ny\n", nil)
	removed := repositoryFile(t, interactiveRepository)
	if got := repositoryFile(t, accessibleRepository); got != removed || strings.Contains(removed, "pre-push") {
		t.Fatalf("hosts disagree after remove:\ninteractive %s\naccessible %s", removed, got)
	}
}

func TestCheckpointEditKeepsExemptPatternsThatHoldASpace(t *testing.T) {
	manager, repository := checkpointsHubFixture(t)
	checkpoint := judgedCheckpoint()
	checkpoint.ExemptPaths = []string{"design docs/**", "*.md"}
	publishIntents(t, manager, "", configuration.SetCheckpoint{Target: configuration.ScopeGlobal, Name: configuration.CheckpointPrePush, Checkpoint: checkpoint})
	enroll(t, manager, repository)

	model := pressHubKeys(t, checkpointsModel(t, manager, repository, nil), tea.KeyPressMsg{Code: tea.KeyEnter}, key('j'), key('e'))
	model.session.checkpoints.reviewBudget = "5"
	publishHubForm(t, model)
	checkpoint.ReviewBudget = 5
	if got := loadedFiles(t, manager, repository).Repository.Document.Checkpoints; !reflect.DeepEqual(got, map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPrePush: checkpoint}) {
		t.Fatalf("Repository Checkpoints = %#v", got)
	}
}

func repositoryFile(t *testing.T, repository configuration.Repository) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(string(repository), ".reviewparty", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestCheckpointsPublishReportsUndeclaredIntegrationsThroughBothHosts(t *testing.T) {
	notice := func() (string, error) { return undeclaredNotice, nil }
	t.Run("interactive", func(t *testing.T) {
		manager, repository := checkpointsHubFixture(t)
		model := pressHubKeys(t, checkpointsModel(t, manager, repository, notice), tea.KeyPressMsg{Code: tea.KeyEnter}, key('j'), key('e'))
		model = publishHubForm(t, model)
		if model.notice != undeclaredNotice || !strings.Contains(model.Render(), "remove them with:") {
			t.Fatalf("notice = %q\n%s", model.notice, model.Render())
		}
	})
	t.Run("accessible", func(t *testing.T) {
		manager, repository := checkpointsHubFixture(t)
		output := runAccessibleCheckpoints(t, manager, repository, "1\n1\n\n\n\n0\n3\n\n0\ny\n", notice)
		if !strings.HasSuffix(output, undeclaredNotice+"\n") {
			t.Fatalf("accessible output does not end with the notice:\n%s", output)
		}
	})
	t.Run("failed check", func(t *testing.T) {
		got := integrationsNotice(func() (string, error) { return "", errors.New("no git") })
		if got != "Checking installed Checkpoint Integrations failed: no git" || integrationsNotice(nil) != "" {
			t.Fatalf("failed check notice = %q", got)
		}
	})
}

func TestUnchangedCheckpointEditIsRefusedWithAFact(t *testing.T) {
	manager, repository := checkpointsHubFixture(t)
	declareGlobalPrePush(t, manager)
	enroll(t, manager, repository)
	for _, test := range []struct {
		draft checkpointFormDraft
		want  string
	}{
		{draft: checkpointFormDraft{target: configuration.ScopeGlobal, operation: checkpointSet, checkpointDraft: newCheckpointDraft(configuration.CheckpointPrePush, judgedCheckpoint())}, want: "Global Configuration already declares this pre-push Checkpoint."},
		{draft: checkpointFormDraft{target: configuration.ScopeGlobal, operation: checkpointRemove, checkpointDraft: checkpointDraft{name: configuration.CheckpointPreCommit}}, want: "Global Configuration does not declare the pre-commit Checkpoint."},
		{draft: checkpointFormDraft{target: configuration.ScopeRepository, operation: checkpointEnroll}, want: "This repository is already enrolled: it has a Repository Configuration file."},
	} {
		message := planRequestCommand(context.Background(), manager, repository, nil, planRequest{kind: planCheckpoints, checkpoint: test.draft})()
		if failed, ok := message.(planFailedMsg); !ok || failed.err.Error() != test.want {
			t.Fatalf("%s plan = %#v, want %q", test.draft.operation, message, test.want)
		}
	}
}

func TestFirstUsePrefillsAnOptedOutRepositoryFromGlobal(t *testing.T) {
	journey := newCheckpointJourney(t, "bugs")
	declareGlobalPrePush(t, journey.manager)
	publishIntents(t, journey.manager, journey.repository, configuration.DeclareNoCheckpoints{})

	output := journey.run(t, "", "", "", "0", "4", "", "0", "y", "", "0")

	declared, err := journey.manager.Checkpoints(journey.repository)
	if err != nil || !reflect.DeepEqual(declared, map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPrePush: judgedCheckpoint()}) {
		t.Fatalf("declared = %#v, %v\noutput:\n%s", declared, err, output)
	}
	if !reflect.DeepEqual(journey.installer.installs, []string{"git team true"}) {
		t.Fatalf("installs = %q\noutput:\n%s", journey.installer.installs, output)
	}
}
