package configurationhub

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"reviewparty/internal/configuration"
)

func TestProfileConfirmationShowsSettingsAndDestinationBeforePublication(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
	})
	draft := configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "test-audit", Reviewer: "codex",
		Model: "gpt-6-luna", ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Audit tests.",
	}
	plan, err := manager.PlanProfileCreation("", draft)
	if err != nil || !plan.Valid() {
		t.Fatalf("plan: %v, %s", err, plan.Reason())
	}
	summary := renderPlanConfirmation(plan, draft)
	for _, want := range []string{
		"Create profile \"test-audit\"", "Scope      Global", "Reviewer   codex",
		"Model      gpt-6-luna", "Effort     high", "Deadline   8m",
		"Save location", plan.Changes()[0].Path, "Nothing has been saved yet.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("missing %q:\n%s", want, summary)
		}
	}
	for _, width := range []int{80, 160} {
		model := New(Snapshot{Repository: "/repo"})
		updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		model = requireHubModel(t, updated)
		updated, _ = model.Update(planReadyMsg{kind: planProfile, summary: summary})
		model = requireHubModel(t, updated)
		view := model.Render()
		if lipgloss.Width(view) > width || lipgloss.Height(view) > 24 {
			t.Fatalf("confirmation exceeds %dx24:\n%s", width, view)
		}
		if !strings.Contains(view, "Reviewer   codex") || !strings.Contains(view, "publish") || !strings.Contains(view, "revise") || !strings.Contains(view, "cancel") {
			t.Fatalf("confirmation lost settings or controls:\n%s", view)
		}
	}
}
