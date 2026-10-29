package configurationhub

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reviewparty/internal/configuration"
)

func globalOnlyManager(t *testing.T) *configuration.Manager {
	t.Helper()
	return configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
}

func globalOnlySnapshot(t *testing.T, manager *configuration.Manager) Snapshot {
	t.Helper()
	snapshot, err := buildSnapshot(manager, "")
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func globalOnlyModel(t *testing.T, manager *configuration.Manager) Model {
	t.Helper()
	model := New(globalOnlySnapshot(t, manager))
	model.runtime = newHubRuntime(RunOptions{Context: t.Context()}, manager)
	model.width, model.height = 120, 30
	return model
}

func TestGlobalOnlyHubWritesOnlyGlobalConfigurationAndSaysWhy(t *testing.T) {
	manager := globalOnlyManager(t)
	publishPreviewProfile(t, manager, configuration.ScopeGlobal, "")
	snapshot := globalOnlySnapshot(t, manager)
	if !slices.Equal(snapshot.Targets, []configuration.Scope{configuration.ScopeGlobal}) {
		t.Fatalf("targets = %v, want Global only", snapshot.Targets)
	}
	for _, file := range snapshot.Reviews.Files {
		if file.Target != configuration.ScopeGlobal {
			t.Fatalf("Reviews offers %s outside a repository", file.Target)
		}
	}
	for _, file := range snapshot.Checkpoints.Files {
		if file.Target != configuration.ScopeGlobal {
			t.Fatalf("Checkpoints offers %s outside a repository", file.Target)
		}
	}
	path, err := manager.ConfigPath(configuration.ScopeGlobal, "")
	if err != nil {
		t.Fatal(err)
	}
	model := New(snapshot)
	model.width = 400
	render := model.Render()
	for _, want := range []string{"[global only] " + path, "Not in a git repository, so Repository Configuration is not available.", "Global: 1 Profile, 0 Parties", runPreviewAbsence} {
		if !strings.Contains(render, want) {
			t.Errorf("Global-only Hub missing %q:\n%s", want, render)
		}
	}
	for _, unwanted := range []string{"[repository]", "Repository: ", "Repository Configuration Reviews", "Repository Configuration Checkpoints"} {
		if strings.Contains(render, unwanted) {
			t.Errorf("Global-only Hub shows %q:\n%s", unwanted, render)
		}
	}
}

func TestGlobalOnlyHubOffersNoProfileCopyInEitherHost(t *testing.T) {
	manager := globalOnlyManager(t)
	publishPreviewProfile(t, manager, configuration.ScopeGlobal, "")

	model := globalOnlyModel(t, manager)
	model.area = 1
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if strings.Contains(model.Render(), "p copy") {
		t.Fatalf("Profiles browser offers copy:\n%s", model.Render())
	}
	model, command := pressHubKey(t, model, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if command != nil || model.view != viewBrowser {
		t.Fatalf("p left the browser: view %v, command %v", model.view, command != nil)
	}
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: '?'})
	if help := stripANSI(model.View().Content); strings.Contains(help, "copy profile") {
		t.Fatalf("help lists copy profile:\n%s", help)
	}

	var output bytes.Buffer
	editor := &editor{manager: manager, RunOptions: RunOptions{Accessible: true, Input: newLineInput("8\n"), Output: &output}}
	action, err := editor.nextAction()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Copy a Repository Profile") || !action.exit {
		t.Fatalf("accessible menu offers copy or misroutes Exit (%#v):\n%s", action, output.String())
	}
}

func TestGlobalOnlyCreationAsksNoConfigurationScope(t *testing.T) {
	manager := globalOnlyManager(t)
	var output bytes.Buffer
	input := "bugs\ncodex\nluna\nhigh\n8m\n2\nReview bugs.\nn\ny\n"
	editor := editor{manager: manager, snapshot: globalOnlySnapshot(t, manager), RunOptions: RunOptions{Input: newLineInput(input), Output: &output, Accessible: true}}
	if err := editor.createProfile(); err != nil {
		t.Fatalf("createProfile: %v\n%s", err, output.String())
	}
	if strings.Contains(output.String(), "Configuration scope") {
		t.Fatalf("accessible Profile creation asked for a scope:\n%s", output.String())
	}
	if _, found, err := manager.LoadProfile(configuration.ScopeGlobal, "", "bugs"); err != nil || !found {
		t.Fatalf("Global Profile found=%v, err=%v", found, err)
	}

	model := globalOnlyModel(t, manager)
	model.area = 1
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if model.view != viewForm || strings.Contains(model.Render(), "Configuration scope") || model.session.profile.target != string(configuration.ScopeGlobal) {
		t.Fatalf("Profile form: view %v, target %q:\n%s", model.view, model.session.profile.target, model.Render())
	}
	model = globalOnlyModel(t, manager)
	model.area = 2
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if model.view != viewForm || strings.Contains(model.Render(), "Configuration scope") || model.session.party.scope != string(configuration.ScopeGlobal) {
		t.Fatalf("Party form: view %v, scope %q:\n%s", model.view, model.session.party.scope, model.Render())
	}
}

func TestGlobalOnlyReviewPreviewNeedsARepository(t *testing.T) {
	manager := globalOnlyManager(t)
	publishPreviewProfile(t, manager, configuration.ScopeGlobal, "")

	output := runAccessibleReviews(t, manager, "", "1\n1\ny\n")
	if strings.Contains(output, "Configuration file") || !strings.Contains(output, runPreviewAbsence) {
		t.Fatalf("accessible Global add:\n%s", output)
	}
	if got := globalReviews(t, manager); !slices.Equal(got.Global, []configuration.SelectionItem{sharedProfile}) {
		t.Fatalf("Global selection = %#v", got)
	}

	model := globalOnlyModel(t, manager)
	model.area = 3
	model = pressHubKeys(t, model, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 'c', Text: "c"})
	model.session.reviews.concurrency = "2"
	model, command := completeHubForm(t, model)
	model, _ = runHubCommand(t, model, command)
	if model.view != viewPlanPreview || !strings.Contains(model.Render(), runPreviewAbsence) {
		t.Fatalf("interactive Global plan: view %v, status %q:\n%s", model.view, model.status, model.Render())
	}
}
