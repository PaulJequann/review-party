package configurationhub

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reviewparty/internal/configuration"
)

func TestAccessibleCopyProfileWithoutRepositoryProfilesReportsError(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	var output bytes.Buffer
	editor := editor{manager: manager, RunOptions: RunOptions{
		Repository: configuration.Repository(t.TempDir()), Accessible: true,
		Input: newLineInput("\n"), Output: &output,
	}}
	if err := editor.refresh(); err != nil {
		t.Fatal(err)
	}
	err := editor.copyProfile()
	if err == nil || err.Error() != "no Repository Profiles exist to copy" {
		t.Fatalf("copyProfile error = %v, want no Repository Profiles exist to copy", err)
	}
	if output.Len() != 0 {
		t.Fatalf("copy form ran without Repository Profiles:\n%s", output.String())
	}
}

func TestHubCopyWithoutRepositoryProfilesStaysInBrowser(t *testing.T) {
	model := New(Snapshot{Repository: "/repo", Items: []Item{
		{Scope: "global", Kind: itemProfile, Name: "quality", Detail: "codex / large"},
	}})
	model.area = 1
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)

	updated, command := model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model = requireHubModel(t, updated)
	if command != nil {
		t.Fatal("copy without Repository Profiles opened a form")
	}
	if model.view != viewBrowser {
		t.Fatalf("view = %v, want browser", model.view)
	}
	if model.status != "No Repository Profiles exist to copy." {
		t.Fatalf("status = %q", model.status)
	}
}
