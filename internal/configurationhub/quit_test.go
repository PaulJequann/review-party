package configurationhub

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

func TestQuitWithoutDraftsIsDirect(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		model := New(Snapshot{})
		updated, command := model.Update(key)
		if requireHubModel(t, updated).quit != nil || command == nil {
			t.Fatal("empty Hub did not exit directly")
		}
		if _, ok := command().(tea.QuitMsg); !ok {
			t.Fatal("empty Hub did not quit")
		}
	}
}

func TestQuitRetainsUnderlyingState(t *testing.T) {
	for _, view := range []viewState{viewMenu, viewBrowser, viewForm, viewPlanPreview} {
		for _, answer := range []tea.KeyPressMsg{{Code: 'n', Text: "n"}, {Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}, {Code: tea.KeyEnter}} {
			model := New(Snapshot{})
			model.drafts.copyName = "unfinished"
			model.view, model.browserArea, model.cursor, model.query = view, AreaProfiles, 2, "filter"
			model.pending = func() tea.Msg { t.Fatal("quit authorized publication"); return nil }
			model.pendingKind = planCopy
			if view == viewForm {
				model.openForm(formCopy, []huh.Field{huh.NewInput().Value(&model.session.copyName)})
				model.session.copyName = "unfinished"
			}
			original := model
			updated, _ := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			model = requireHubModel(t, updated)
			if model.quit == nil || model.quit.discard {
				t.Fatal("quit did not default to keeping drafts")
			}
			updated, command := model.Update(answer)
			model, quit := settleQuit(t, requireHubModel(t, updated), command)
			if quit || model.quit != nil || model.view != view || model.form.form != original.form.form || model.session != original.session || model.cursor != 2 || model.query != "filter" || model.pending == nil || !reflect.DeepEqual(model.drafts, original.drafts) {
				t.Fatalf("declining quit lost state in view %v: %#v", view, model)
			}
		}
	}
}

func TestQuitSyncsTypedDraftAndExplicitlyDiscards(t *testing.T) {
	model := New(Snapshot{})
	model.openForm(formParty, []huh.Field{huh.NewInput().Value(&model.session.party.name)})
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = requireHubModel(t, updated)
	if model.session.party.name != "q" || model.quit != nil {
		t.Fatal("q did not type into the active input")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = requireHubModel(t, updated)
	if model.quit == nil || model.drafts.party.name != "q" {
		t.Fatal("typed draft was not synchronized before quit")
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model, quit := settleQuit(t, requireHubModel(t, updated), command)
	if !quit || !model.drafts.empty() {
		t.Fatal("explicit discard did not clear drafts and quit")
	}
}

func TestQuitDoesNotCaptureSearchText(t *testing.T) {
	model := New(Snapshot{})
	model.searching = true
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = requireHubModel(t, updated)
	if model.query != "q" || command != nil || model.quit != nil {
		t.Fatal("q did not type into search")
	}
}

func TestQuitOverlayAllowsBackgroundResultsAndResize(t *testing.T) {
	model := New(Snapshot{})
	model.drafts.copyName = "published"
	model.view = viewPlanPreview
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(planReadyMsg{kind: planCopy, summary: "complete", publish: func() tea.Msg { t.Fatal("overlay published"); return nil }})
	model = requireHubModel(t, updated)
	if model.quit == nil || model.planSummary != "complete" || model.pending == nil {
		t.Fatal("plan was lost behind overlay")
	}
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	model = requireHubModel(t, updated)
	if model.width != 120 || model.height != 30 || model.quit == nil {
		t.Fatal("resize lost confirmation")
	}
	updated, _ = model.Update(publishResultMsg{kind: planCopy, snapshot: Snapshot{Repository: "refreshed"}})
	model = requireHubModel(t, updated)
	if model.quit == nil || !model.drafts.empty() || model.snapshot.Repository != "refreshed" {
		t.Fatal("authorized publication did not finish behind confirmation")
	}
	if !strings.Contains(model.Render(), "Discard unfinished drafts") || !strings.Contains(model.Render(), "Ctrl+C keeps your work") {
		t.Fatal("explicit exit confirmation vanished after publication")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = requireHubModel(t, updated)
	if model.quit != nil || model.view != viewMenu || !model.drafts.empty() {
		t.Fatal("dismissal did not reveal updated Hub")
	}
}

func TestOldFormMessagesCannotAdvanceNewFormOrQuit(t *testing.T) {
	model := New(Snapshot{})
	model.openForm(formParty, []huh.Field{huh.NewInput().Value(&model.session.party.name)})
	message := model.form.command(huh.NextField, false)()
	model.drafts.profile = configuration.ProfileDraft{Name: "retained"}
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = requireHubModel(t, updated)
	model.openForm(formCopy, []huh.Field{huh.NewInput().Value(&model.session.copyName)})
	updated, command := model.Update(message)
	model = requireHubModel(t, updated)
	if command != nil || model.form.State() != huh.StateNormal || model.quit == nil {
		t.Fatal("stale form command changed the active form or confirmation")
	}
}

func settleQuit(t *testing.T, model Model, command tea.Cmd) (Model, bool) {
	t.Helper()
	pending := []tea.Cmd{command}
	for step := 0; len(pending) > 0; step++ {
		if step == 40 {
			t.Fatal("confirmation did not settle")
		}
		next := pending[0]
		pending = pending[1:]
		if next == nil {
			continue
		}
		message := next()
		if _, quit := message.(tea.QuitMsg); quit {
			return model, true
		}
		if batch, ok := message.(tea.BatchMsg); ok {
			pending = append(pending, batch...)
			continue
		}
		updated, follow := model.Update(message)
		model = requireHubModel(t, updated)
		pending = append(pending, follow)
	}
	return model, false
}

func TestOldFormGroupCompletionCannotAdvanceReplacement(t *testing.T) {
	model := New(Snapshot{})
	model.openForm(formParty, []huh.Field{huh.NewInput().Value(&model.session.party.name)})
	updated, completion := model.Update(model.form.command(huh.NextField, false)())
	model = requireHubModel(t, updated)
	model.session.copyName = "retained"
	model.openForm(formCopy, []huh.Field{huh.NewInput().Value(&model.session.copyName)})
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model, quit := settleQuit(t, requireHubModel(t, updated), completion)
	if quit || model.quit == nil || model.form.State() != huh.StateNormal || model.session.copyName != "retained" {
		t.Fatal("stale group completion changed the replacement form or quit overlay")
	}
}

func TestQuitDoesNotPasteIntoUnderlyingDraft(t *testing.T) {
	model := New(Snapshot{})
	model.session.party.name = "retained"
	model.openForm(formParty, []huh.Field{huh.NewInput().Value(&model.session.party.name)})
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = requireHubModel(t, updated)
	updated, command := model.Update(tea.PasteMsg{Content: "unexpected"})
	model = requireHubModel(t, updated)
	if command != nil || model.session.party.name != "retained" || model.quit == nil {
		t.Fatal("paste reached the draft behind quit confirmation")
	}
}
