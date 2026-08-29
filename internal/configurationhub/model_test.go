package configurationhub

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

func TestHubLabelsEverySearchResultWithItsScope(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{
		{Scope: "global", Kind: "Profile", Name: "quality", Detail: "codex / large"},
		{Scope: "repository", Kind: "Profile", Name: "quality", Detail: "grok / fast"},
	}})
	model.area = 1
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

func TestHubRendersConfiguredParties(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{{Scope: "global", Kind: "Party", Name: "baseline", Detail: "2 Profiles"}}})
	model.area = 2
	view := model.Render()
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
		{name: "advanced", area: 4, want: "Copy a Repository Profile"},
		{name: "changes", area: 5, want: "unfinished drafts"},
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
