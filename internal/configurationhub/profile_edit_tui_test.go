package configurationhub

import (
	"strings"
	"testing"
)

// The Profiles browser lists Templates before Profiles, and the cursor indexes
// that mixed list. Editing must follow the highlighted row, never re-index a
// profiles-only slice.
func newProfileBrowserModel() Model {
	model := New(Snapshot{Repository: "/repo", Items: []Item{
		{Scope: "template", Kind: itemTemplate, Name: "bugs", Detail: "Review Profile Template v1"},
		{Scope: "global", Kind: itemProfile, Name: "real", Detail: "grok / grok-4.5"},
	}})
	model.browserArea = AreaProfiles
	model.view = viewBrowser
	return model
}

func TestBrowserEditOnTemplateRowNeverStartsProfileEdit(t *testing.T) {
	model := newProfileBrowserModel()
	model.cursor = 0
	updated, _ := model.browserEdit()
	model = requireHubModel(t, updated)
	if model.session.editProfile != "" {
		t.Fatalf("editProfile = %q, want empty on a Template row", model.session.editProfile)
	}
	if !strings.Contains(model.status, "Template") {
		t.Fatalf("status = %q, want a Template hint", model.status)
	}
}

func TestBrowserEditUsesHighlightedProfileRow(t *testing.T) {
	model := newProfileBrowserModel()
	model.cursor = 1
	updated, _ := model.browserEdit()
	model = requireHubModel(t, updated)
	if model.session.editProfile != "global:real" {
		t.Fatalf("editProfile = %q, want global:real", model.session.editProfile)
	}
	if model.view != viewForm {
		t.Fatalf("view = %v, want form view", model.view)
	}
}

func TestBrowserEditBeyondVisibleRowsReportsNoSelection(t *testing.T) {
	model := newProfileBrowserModel()
	model.cursor = 5
	updated, _ := model.browserEdit()
	model = requireHubModel(t, updated)
	if model.session.editProfile != "" {
		t.Fatalf("editProfile = %q, want empty beyond the list", model.session.editProfile)
	}
	if !strings.Contains(model.status, "No Profile row is selected.") {
		t.Fatalf("status = %q, want no-selection message", model.status)
	}
}
