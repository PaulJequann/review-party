package configurationhub

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"reviewparty/internal/discovery"
)

func TestHubFormResizeUpdatesAdapterSize(t *testing.T) {
	model := New(Snapshot{})
	model.area = 1
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated, _ = requireHubModel(t, updated).Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = requireHubModel(t, updated)
	budget := model.frameBudget(renderOptions{styled: true})
	if model.form.width != 100 || model.form.height != budget.viewport {
		t.Fatalf("form size = %dx%d, want 100x%d", model.form.width, model.form.height, budget.viewport)
	}
}

func TestProfileFormShowsNavigationGuidance(t *testing.T) {
	model := hubModelAtSize(t, 80, 24)
	model.openArea(AreaProfiles)
	frame := stripANSI(model.View().Content)
	for _, hint := range []string{"tab next", "shift+tab previous", "enter continue", "esc back"} {
		if !strings.Contains(frame, hint) {
			t.Errorf("form omitted %q:\n%s", hint, frame)
		}
	}
	if got := lipgloss.Height(frame); got > 24 {
		t.Fatalf("guidance pushes form to %d rows", got)
	}
}

func TestProfileFormKeepsFocusedInputVisible(t *testing.T) {
	model := hubModelAtSize(t, 120, 30)
	model.openArea(AreaProfiles)
	// The fields form holds identity inputs only; model, effort, and
	// deadline moved to stepped choice forms (see profile_choices.go).
	for _, name := range []string{"name", "reviewer"} {
		spec, _ := profileFieldSpecFor(name)
		updated, _ := model.Update(huh.NextField())
		model = requireHubModel(t, updated)
		assertFocusedSpecVisible(t, model, spec)
	}
	for _, open := range []struct {
		name  string
		form  func() tea.Cmd
		want  string
		setup func()
	}{
		{"model choices", model.openProfileModelForm, "Model", func() {
			model.session.profile.choices = []discovery.ModelChoice{{
				Model:   discovery.Model{ID: "luna", DisplayName: "Luna", ReasoningEfforts: []string{"low", "high"}},
				Sources: []discovery.ChoiceSource{discovery.ChoiceSourceCached},
			}}
		}},
		{"effort choices", model.openProfileEffortForm, "Reasoning effort", func() {
			model.session.profile.draft.Model = "luna"
		}},
		{"manual model", model.openProfileManualModelForm, "Model ID", func() {}},
		{"manual effort", model.openProfileManualEffortForm, "Reasoning effort", func() {}},
		{"deadline", model.openProfileDeadlineForm, "Attempt deadline (for example 8m)", func() {}},
	} {
		open.setup()
		openSteppedForm(t, &model, open.form)
		for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 30}} {
			updated, _ := model.Update(size)
			model = requireHubModel(t, updated)
			frame := stripANSI(model.View().Content)
			if !strings.Contains(frame, "┃ "+open.want) {
				t.Fatalf("stepped %s form hides its focused input:\n%s", open.name, frame)
			}
			assertFrameFits(t, model, open.name, size)
		}
	}
}

// openSteppedForm opens a form the way the runtime does: the open command's
// Init message is delivered before rendering. Note-first forms (choice
// selects) render blank until Init focuses their first editable field.
// The open function is bound to the caller's model, so the model is taken
// by pointer: a by-value copy would clobber the freshly opened form.
func openSteppedForm(t *testing.T, model *Model, open func() tea.Cmd) {
	t.Helper()
	command := open()
	if command != nil {
		if message := command(); message != nil {
			updated, _ := model.Update(message)
			*model = requireHubModel(t, updated)
		}
	}
}

func assertFocusedSpecVisible(t *testing.T, model Model, spec profileFieldSpec) {
	t.Helper()
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 30}} {
		updated, _ := model.Update(size)
		model = requireHubModel(t, updated)
		frame := stripANSI(model.View().Content)
		if !strings.Contains(frame, "┃ "+spec.title) {
			t.Fatalf("focused %s is not visible after resize:\n%s", spec.name, frame)
		}
		assertFrameFits(t, model, spec.name, size)
	}
}

func assertFrameFits(t *testing.T, model Model, what string, size tea.WindowSizeMsg) {
	t.Helper()
	frame := stripANSI(model.View().Content)
	if got := lipgloss.Height(frame); got > size.Height {
		t.Fatalf("focused %s overflows terminal: %d rows", what, got)
	}
	for _, line := range strings.Split(frame, "\n") {
		if got := lipgloss.Width(line); got > size.Width {
			t.Fatalf("focused %s line has %d columns, terminal has %d: %q", what, got, size.Width, line)
		}
	}
}

func TestProfileFormFitsTerminalThroughResize(t *testing.T) {
	model := hubModelAtSize(t, 120, 30)
	model.snapshot.Repository = "/repo/" + strings.Repeat("long-directory/", 10)
	model.openArea(AreaProfiles)
	for _, size := range []tea.WindowSizeMsg{
		{Width: 120, Height: 30},
		{Width: 80, Height: 24},
		{Width: 100, Height: 30},
		{Width: 160, Height: 50},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			updated, _ := model.Update(size)
			model = requireHubModel(t, updated)
			frame := stripANSI(model.View().Content)
			if got := lipgloss.Height(frame); got > size.Height {
				t.Fatalf("form has %d rows, terminal has %d:\n%s", got, size.Height, frame)
			}
			for _, line := range strings.Split(frame, "\n") {
				if got := lipgloss.Width(line); got > size.Width {
					t.Fatalf("form line has %d columns, terminal has %d: %q", got, size.Width, line)
				}
			}
			for _, text := range []string{"Configuration scope", "╰", "esc back", "? help"} {
				if !strings.Contains(frame, text) {
					t.Fatalf("form omitted %q:\n%s", text, frame)
				}
			}
		})
	}
}
