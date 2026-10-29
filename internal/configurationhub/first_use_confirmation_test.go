package configurationhub

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestFirstUseFormsRequestTheAlternateScreen(t *testing.T) {
	view := firstUseFormView(tea.NewView("Choose a review"))
	if !view.AltScreen || view.Content != "Choose a review" {
		t.Fatalf("setup view = %#v", view)
	}
}

func TestFirstUseConfirmationKeepsPlanAndControlsInTheAlternateScreen(t *testing.T) {
	model := firstUseConfirmation{Model: New(Snapshot{Repository: "/repo"})}
	model.view = viewPlanPreview
	model.planSummary = strings.Repeat("Profile details\n", 40) + "Last setting"
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, ok := updated.(firstUseConfirmation)
	if !ok {
		t.Fatalf("setup returned %T", updated)
	}
	view := model.View()
	if !view.AltScreen || lipgloss.Height(view.Content) > 24 || !strings.Contains(view.Content, "publish") {
		t.Fatalf("setup confirmation clipped controls:\n%s", view.Content)
	}
	for range 50 {
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		var ok bool
		model, ok = updated.(firstUseConfirmation)
		if !ok {
			t.Fatalf("setup returned %T", updated)
		}
	}
	if !strings.Contains(model.View().Content, "Last setting") {
		t.Fatal("cannot scroll through the complete setup Plan")
	}
	for _, test := range []struct {
		key                string
		confirmed, aborted bool
	}{
		{"p", true, false}, {"e", false, false}, {"esc", false, true}, {"ctrl+c", false, true},
	} {
		var key tea.KeyPressMsg
		switch test.key {
		case "esc":
			key = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "ctrl+c":
			key = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
		default:
			key = tea.KeyPressMsg{Code: rune(test.key[0]), Text: test.key}
		}
		updated, command := model.Update(key)
		result, ok := updated.(firstUseConfirmation)
		if !ok {
			t.Fatalf("setup returned %T", updated)
		}
		if command == nil || result.confirmed != test.confirmed || result.aborted != test.aborted {
			t.Fatalf("%s: confirmed=%v aborted=%v", test.key, result.confirmed, result.aborted)
		}
	}
}
