package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileOnboardingPublishesOnlyAfterConfirmation(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(Options{
		GlobalRoot:   root,
		Reviewers:    []string{"grok"},
		Templates:    []Template{{ID: "bugs", Revision: "v1", Instructions: "Find bugs.\n"}},
		ValidateName: func(string) error { return nil },
	})
	flow := NewProfileOnboarding(ScopeGlobal)
	requireOnboardingStep(t, flow, OnboardingChooseSource)
	requireOnboardingErrorFree(t, flow.ChooseTemplate(manager, "bugs"))
	requireOnboardingErrorFree(t, flow.SetName("bugs"))
	requireOnboardingErrorFree(t, flow.SetReviewer("grok"))
	requireOnboardingErrorFree(t, flow.SetModel("grok-4.5"))
	requireOnboardingErrorFree(t, flow.SetEffort("high"))
	requireOnboardingErrorFree(t, flow.SetDeadline("1m"))
	plan, err := flow.Validate(manager, "")
	requireValidOnboardingPlan(t, flow, plan, err)
	if _, err := os.Stat(filepath.Join(root, "profiles", "bugs")); !os.IsNotExist(err) {
		t.Fatalf("validation wrote Profile material: %v", err)
	}
	requireOnboardingErrorFree(t, flow.Confirm(manager))
	requireOnboardingStep(t, flow, OnboardingComplete)
	if _, err := os.Stat(filepath.Join(root, "profiles", "bugs", "profile.json")); err != nil {
		t.Fatalf("confirmed Profile missing: %v", err)
	}
}

func TestProfileOnboardingCancellationRetainsDraftWithoutWriting(t *testing.T) {
	flow := NewProfileOnboarding(ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review documentation.\n"))
	requireOnboardingErrorFree(t, flow.SetName("docs"))
	requireOnboardingErrorFree(t, flow.Cancel())
	requireOnboardingStep(t, flow, OnboardingCancelled)
	if draft := flow.Draft(); draft.Name != "docs" || draft.Instructions == "" {
		t.Fatalf("cancelled draft = %#v", draft)
	}
	requireOnboardingErrorFree(t, flow.Resume())
	requireOnboardingStep(t, flow, OnboardingReviewer)
	flow.Discard()
	requireOnboardingStep(t, flow, OnboardingChooseSource)
	if draft := flow.Draft(); draft.Name != "" || draft.Instructions != "" {
		t.Fatalf("discarded draft = %#v", draft)
	}
}

func TestProfileOnboardingCannotCancelAfterConfirmation(t *testing.T) {
	manager := NewManager(Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"grok"},
		ValidateName: func(string) error { return nil },
	})
	flow := NewProfileOnboarding(ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.SetName("bugs"))
	requireOnboardingErrorFree(t, flow.SetReviewer("grok"))
	requireOnboardingErrorFree(t, flow.SetModel("grok-4.6"))
	requireOnboardingErrorFree(t, flow.SetEffort("high"))
	requireOnboardingErrorFree(t, flow.SetDeadline("1m"))
	plan, err := flow.Validate(manager, "")
	requireValidOnboardingPlan(t, flow, plan, err)
	requireOnboardingErrorFree(t, flow.Confirm(manager))
	if err := flow.Cancel(); err == nil {
		t.Fatal("completed onboarding accepted cancellation")
	}
	requireOnboardingStep(t, flow, OnboardingComplete)
}

func TestProfileOnboardingRejectsTargetCreatedAfterValidation(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"grok"}, ValidateName: func(string) error { return nil }})
	flow := NewProfileOnboarding(ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.SetName("bugs"))
	requireOnboardingErrorFree(t, flow.SetReviewer("grok"))
	requireOnboardingErrorFree(t, flow.SetModel("grok-4.6"))
	requireOnboardingErrorFree(t, flow.SetEffort("high"))
	requireOnboardingErrorFree(t, flow.SetDeadline("1m"))
	plan, err := flow.Validate(manager, "")
	requireValidOnboardingPlan(t, flow, plan, err)

	otherManager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"grok"}, ValidateName: func(string) error { return nil }})
	otherPlan, err := otherManager.PlanProfileCreation("", ProfileDraft{
		Target: ScopeGlobal, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "Newer review.\n",
	})
	if err != nil {
		t.Fatalf("other plan error = %v", err)
	}
	if !otherPlan.Valid() {
		t.Fatalf("other plan = %#v, err = %v", otherPlan, err)
	}
	requireOnboardingErrorFree(t, otherManager.Publish(otherPlan))
	if err := flow.Confirm(manager); err == nil {
		t.Fatal("stale onboarding plan overwrote a Profile created after validation")
	}
	profile, found, err := manager.LoadProfile(ScopeGlobal, "", "bugs")
	if err != nil {
		t.Fatalf("load newer Profile: %v", err)
	}
	if !found {
		t.Fatal("newer Profile was not found")
	}
	if profile.Model != "grok-4.5" {
		t.Fatalf("newer Profile model = %q", profile.Model)
	}
	if profile.Instructions != "Newer review.\n" {
		t.Fatalf("newer Profile = %#v, found = %v, err = %v", profile, found, err)
	}
}

func TestProfileOnboardingCannotConfirmThroughAnotherManager(t *testing.T) {
	manager := NewManager(Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"grok"},
		ValidateName: func(string) error { return nil },
	})
	otherManager := NewManager(Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"grok"},
		ValidateName: func(string) error { return nil },
	})
	flow := NewProfileOnboarding(ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.SetName("bugs"))
	requireOnboardingErrorFree(t, flow.SetReviewer("grok"))
	requireOnboardingErrorFree(t, flow.SetModel("grok-4.6"))
	requireOnboardingErrorFree(t, flow.SetEffort("high"))
	requireOnboardingErrorFree(t, flow.SetDeadline("1m"))
	plan, err := flow.Validate(manager, "")
	requireValidOnboardingPlan(t, flow, plan, err)
	if err := flow.Confirm(otherManager); err == nil {
		t.Fatal("onboarding confirmed through a different Configuration Manager")
	}
	requireOnboardingStep(t, flow, OnboardingReview)
}

func TestProfileOnboardingTemplateEditBecomesBlankInstructions(t *testing.T) {
	manager := NewManager(Options{
		GlobalRoot: t.TempDir(), Templates: []Template{{ID: "bugs", Revision: "v1", Instructions: "Packaged.\n"}},
		ValidateName: func(string) error { return nil },
	})
	flow := NewProfileOnboarding(ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseTemplate(manager, "bugs"))
	requireOnboardingErrorFree(t, flow.SetInstructions("Customized.\n"))
	draft := flow.Draft()
	if draft.TemplateID != "" {
		t.Fatalf("edited Template retained an ID: %#v", draft)
	}
	if draft.TemplateRevision != "" {
		t.Fatalf("edited Template retained a revision: %#v", draft)
	}
	if draft.Instructions != "Customized.\n" {
		t.Fatalf("edited Template instructions = %q", draft.Instructions)
	}
}

func TestProfileOnboardingEmptyFieldsDoNotAdvance(t *testing.T) {
	flow := NewProfileOnboarding(ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank(""))
	requireOnboardingStep(t, flow, OnboardingChooseSource)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.SetInstructions(""))
	requireOnboardingStep(t, flow, OnboardingChooseSource)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))

	requireOnboardingErrorFree(t, flow.SetName(""))
	requireOnboardingStep(t, flow, OnboardingName)
	requireOnboardingErrorFree(t, flow.SetName("bugs"))
	requireOnboardingErrorFree(t, flow.SetReviewer(""))
	requireOnboardingStep(t, flow, OnboardingReviewer)
	requireOnboardingErrorFree(t, flow.SetReviewer("grok"))
	requireOnboardingErrorFree(t, flow.SetModel(""))
	requireOnboardingStep(t, flow, OnboardingModel)
	requireOnboardingErrorFree(t, flow.SetModel("grok-4.6"))
	requireOnboardingErrorFree(t, flow.SetEffort(""))
	requireOnboardingStep(t, flow, OnboardingEffort)
	requireOnboardingErrorFree(t, flow.SetEffort("high"))
	requireOnboardingErrorFree(t, flow.SetDeadline(""))
	requireOnboardingStep(t, flow, OnboardingDeadline)
	requireOnboardingErrorFree(t, flow.Cancel())
	requireOnboardingErrorFree(t, flow.Resume())
	requireOnboardingStep(t, flow, OnboardingDeadline)
}

func TestProfileOnboardingClearsDependentFieldsWhenReviewerChanges(t *testing.T) {
	manager := NewManager(Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"grok", "codex"},
		ValidateName: func(string) error { return nil },
	})
	flow := NewProfileOnboarding(ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.SetName("bugs"))
	requireOnboardingErrorFree(t, flow.SetReviewer("grok"))
	requireOnboardingErrorFree(t, flow.SetModel("grok-4.6"))
	requireOnboardingErrorFree(t, flow.SetEffort("high"))
	requireOnboardingErrorFree(t, flow.SetDeadline("1m"))
	requireOnboardingErrorFree(t, flow.SetReviewer("codex"))
	draft := flow.Draft()
	if draft.Model != "" {
		t.Fatalf("stale model = %q", draft.Model)
	}
	if draft.ReasoningEffort != "" {
		t.Fatalf("stale reasoning effort = %q", draft.ReasoningEffort)
	}
	if draft.AttemptDeadline != "" {
		t.Fatalf("stale deadline = %q", draft.AttemptDeadline)
	}
	if _, err := flow.Validate(manager, ""); err == nil {
		t.Fatal("validation accepted an incomplete draft")
	}
}

func requireOnboardingErrorFree(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func requireOnboardingStep(t *testing.T, flow *ProfileOnboarding, want OnboardingStep) {
	t.Helper()
	if flow.Step() != want {
		t.Fatalf("step = %s, want %s", flow.Step(), want)
	}
}

func requireValidOnboardingPlan(t *testing.T, flow *ProfileOnboarding, plan Plan, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("validate error = %v", err)
	}
	if !plan.Valid() {
		t.Fatalf("invalid plan = %#v", plan)
	}
	requireOnboardingStep(t, flow, OnboardingReview)
}
