package configurationhub

import (
	"bytes"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func publishGlobalEditProfile(t *testing.T, manager *configuration.Manager, name string) {
	t.Helper()
	plan, err := manager.PlanProfileCreation("", configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: name, Reviewer: "codex",
		Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func TestAccessibleEditProfileUpdatesModel(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	publishGlobalEditProfile(t, manager, "bugs")
	input := "2\n1\n2\nluna-2\n5\ny\n"
	editor := editor{manager: manager, RunOptions: RunOptions{
		Input: newLineInput(input), Output: &bytes.Buffer{}, Accessible: true,
	}}
	if err := editor.refresh(); err != nil {
		t.Fatal(err)
	}
	if err := editor.manageProfiles(); err != nil {
		t.Fatal(err)
	}
	profile, found, err := manager.LoadProfile(configuration.ScopeGlobal, "", "bugs")
	if err != nil || !found {
		t.Fatalf("reloaded found=%v err=%v", found, err)
	}
	if profile.Model != "luna-2" {
		t.Fatalf("model = %q, want luna-2", profile.Model)
	}
}

func TestPlanProfileEditAttachesModelWarning(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	publishGlobalEditProfile(t, manager, "bugs")
	editor := editor{manager: manager, RunOptions: RunOptions{
		ModelChoiceCheck: func(reviewer, model string) configuration.ModelChoiceCheck {
			return configuration.ModelChoiceCheck{Status: configuration.ModelChoicesUnknown}
		},
	}}
	plan, err := editor.planProfileEdit(configuration.ScopeGlobal, "bugs", configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "bugs", Reviewer: "codex",
		Model: "custom", ReasoningEffort: "high", AttemptDeadline: "8m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings()) != 1 || !strings.Contains(plan.Warnings()[0], "custom") {
		t.Fatalf("warnings = %#v", plan.Warnings())
	}
}
