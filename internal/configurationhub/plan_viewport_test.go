package configurationhub

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestLongPlanKeepsPublicationControlsVisible(t *testing.T) {
	model := New(Snapshot{Repository: "/repository"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(planReadyMsg{summary: strings.Repeat("selected Profile\n", 50) + "final warning"})
	model = requireHubModel(t, updated)
	view := model.Render()
	if lipgloss.Height(view) > 24 || !strings.Contains(view, "publish") || !strings.Contains(view, "more lines") {
		t.Fatalf("plan controls were clipped:\n%s", view)
	}
	for range 60 {
		updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		model = requireHubModel(t, updated)
	}
	if view := model.Render(); !strings.Contains(view, "final warning") || !strings.Contains(view, "publish") {
		t.Fatalf("cannot read complete plan with fixed controls:\n%s", view)
	}
}
