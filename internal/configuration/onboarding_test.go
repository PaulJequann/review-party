package configuration

import (
	"os"
	"path/filepath"
	"strings"
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
	flow := NewProfileOnboarding(manager, ScopeGlobal)
	requireOnboardingStep(t, flow, OnboardingChooseSource)
	requireOnboardingErrorFree(t, flow.ChooseTemplate("bugs"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, "bugs"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldReviewer, "grok"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldModel, "grok-4.5"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldEffort, "high"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldDeadline, "1m"))
	plan, err := flow.Validate("")
	requireValidOnboardingPlan(t, flow, plan, err)
	if _, err := os.Stat(filepath.Join(root, "profiles", "bugs")); !os.IsNotExist(err) {
		t.Fatalf("validation wrote Profile material: %v", err)
	}
	requireOnboardingErrorFree(t, flow.Confirm())
	requireOnboardingStep(t, flow, OnboardingComplete)
	if _, err := os.Stat(filepath.Join(root, "profiles", "bugs", "profile.json")); err != nil {
		t.Fatalf("confirmed Profile missing: %v", err)
	}
}

func TestProfileOnboardingCarriesModelChoiceWarningOnPlan(t *testing.T) {
	manager := NewManager(Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"grok"},
		ValidateName: func(string) error { return nil },
	})
	flow := NewProfileOnboarding(manager, ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, "bugs"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldReviewer, "grok"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldModel, "grok-custom"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldEffort, "high"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldDeadline, "1m"))
	requireOnboardingErrorFree(t, flow.SetModelChoiceCheck(ModelChoiceCheck{Status: ModelChoicesUnknown}))
	plan, err := flow.Validate("")
	requireValidOnboardingPlan(t, flow, plan, err)
	warnings := plan.Warnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "grok-custom") {
		t.Fatalf("plan warnings = %#v", warnings)
	}
	if err := flow.SetModelChoiceCheck(ModelChoiceCheck{Status: ModelChoicesKnown}); err == nil {
		t.Fatal("onboarding accepted a choice check change after validation")
	}
}

func TestProfileOnboardingInstructionEditPreservesExecutableFields(t *testing.T) {
	flow := completeOnboarding(t, nil)
	requireOnboardingErrorFree(t, flow.SetInstructions("Updated instructions.\n"))
	draft := flow.Draft()
	for _, field := range []struct {
		name string
		got  string
		want string
	}{
		{name: "name", got: draft.Name, want: "bugs"},
		{name: "reviewer", got: draft.Reviewer, want: "grok"},
		{name: "model", got: draft.Model, want: "grok-4.6"},
		{name: "effort", got: draft.ReasoningEffort, want: "high"},
		{name: "deadline", got: draft.AttemptDeadline, want: "1m"},
	} {
		if field.got != field.want {
			t.Fatalf("instruction edit %s = %q, want %q", field.name, field.got, field.want)
		}
	}
	if draft.Instructions != "Updated instructions.\n" || draft.TemplateID != "" {
		t.Fatalf("instruction edit draft = %#v", draft)
	}
}

func TestProfileOnboardingSourceEditInvalidatesReviewedPlan(t *testing.T) {
	manager := NewManager(Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"grok"},
		ValidateName: func(string) error { return nil },
	})
	flow := completeOnboarding(t, manager)
	plan, err := flow.Validate("")
	requireValidOnboardingPlan(t, flow, plan, err)
	requireOnboardingErrorFree(t, flow.SetInstructions("Changed instructions.\n"))
	if flow.Plan().Valid() {
		t.Fatal("instruction edit retained a reviewed plan")
	}
	if err := flow.Confirm(); err == nil {
		t.Fatal("onboarding confirmed a plan after source edit")
	}
}

func TestProfileOnboardingRevisionKeepsUnchangedLaterFields(t *testing.T) {
	manager := NewManager(Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"grok"},
		ValidateName: func(string) error { return nil },
	})
	flow := completeOnboarding(t, manager)
	plan, err := flow.Validate("")
	requireValidOnboardingPlan(t, flow, plan, err)
	requireOnboardingErrorFree(t, flow.Revise())
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, "bugs"))
	draft := flow.Draft()
	for _, field := range []struct {
		name string
		got  string
		want string
	}{
		{name: "reviewer", got: draft.Reviewer, want: "grok"},
		{name: "model", got: draft.Model, want: "grok-4.6"},
		{name: "reasoning effort", got: draft.ReasoningEffort, want: "high"},
		{name: "deadline", got: draft.AttemptDeadline, want: "1m"},
	} {
		if field.got != field.want {
			t.Fatalf("revised draft %s = %q, want %q", field.name, field.got, field.want)
		}
	}
	requireOnboardingStep(t, flow, OnboardingValidation)
}

func TestProfileOnboardingCancellationRetainsDraftWithoutWriting(t *testing.T) {
	flow := NewProfileOnboarding(nil, ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review documentation.\n"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, "docs"))
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
	flow := completeOnboarding(t, manager)
	plan, err := flow.Validate("")
	requireValidOnboardingPlan(t, flow, plan, err)
	requireOnboardingErrorFree(t, flow.Confirm())
	if err := flow.Cancel(); err == nil {
		t.Fatal("completed onboarding accepted cancellation")
	}
	requireOnboardingStep(t, flow, OnboardingComplete)
}

func TestProfileOnboardingRejectsTargetCreatedAfterValidation(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"grok"}, ValidateName: func(string) error { return nil }})
	flow := NewProfileOnboarding(manager, ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, "bugs"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldReviewer, "grok"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldModel, "grok-4.6"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldEffort, "high"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldDeadline, "1m"))
	plan, err := flow.Validate("")
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
	if err := flow.Confirm(); err == nil {
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

func TestProfileOnboardingRequiresBoundManager(t *testing.T) {
	flow := NewProfileOnboarding(nil, ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, "bugs"))
	if _, err := flow.Validate(""); err == nil {
		t.Fatal("onboarding validated without a bound Configuration Manager")
	}
}

func TestProfileOnboardingTemplateEditBecomesBlankInstructions(t *testing.T) {
	manager := NewManager(Options{
		GlobalRoot: t.TempDir(), Templates: []Template{{ID: "bugs", Revision: "v1", Instructions: "Packaged.\n"}},
		ValidateName: func(string) error { return nil },
	})
	flow := NewProfileOnboarding(manager, ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseTemplate("bugs"))
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
	flow := NewProfileOnboarding(nil, ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank(""))
	requireOnboardingStep(t, flow, OnboardingChooseSource)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.SetInstructions(""))
	requireOnboardingStep(t, flow, OnboardingChooseSource)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))

	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, ""))
	requireOnboardingStep(t, flow, OnboardingName)
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, "bugs"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldReviewer, ""))
	requireOnboardingStep(t, flow, OnboardingReviewer)
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldReviewer, "grok"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldModel, ""))
	requireOnboardingStep(t, flow, OnboardingModel)
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldModel, "grok-4.6"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldEffort, ""))
	requireOnboardingStep(t, flow, OnboardingEffort)
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldEffort, "high"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldDeadline, ""))
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
	flow := NewProfileOnboarding(manager, ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldName, "bugs"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldReviewer, "grok"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldModel, "grok-4.6"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldEffort, "high"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldDeadline, "1m"))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldReviewer, "codex"))
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
	if _, err := flow.Validate(""); err == nil {
		t.Fatal("validation accepted an incomplete draft")
	}
}

func TestProfileOnboardingClearsModelChoiceCheckWhenModelChanges(t *testing.T) {
	flow := completeOnboarding(t, nil)
	requireOnboardingErrorFree(t, flow.SetModelChoiceCheck(ModelChoiceCheck{Status: ModelChoicesKnown}))
	requireOnboardingErrorFree(t, flow.Set(OnboardingFieldModel, "grok-custom"))
	if flow.modelChoiceCheck != (ModelChoiceCheck{}) {
		t.Fatalf("model choice check = %#v, want cleared", flow.modelChoiceCheck)
	}
}

func requireOnboardingErrorFree(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func completeOnboarding(t *testing.T, manager *Manager) *ProfileOnboarding {
	t.Helper()
	flow := NewProfileOnboarding(manager, ScopeGlobal)
	requireOnboardingErrorFree(t, flow.ChooseBlank("Review bugs.\n"))
	for _, field := range []struct {
		name  OnboardingField
		value OnboardingText
	}{
		{name: OnboardingFieldName, value: "bugs"},
		{name: OnboardingFieldReviewer, value: "grok"},
		{name: OnboardingFieldModel, value: "grok-4.6"},
		{name: OnboardingFieldEffort, value: "high"},
		{name: OnboardingFieldDeadline, value: "1m"},
	} {
		requireOnboardingErrorFree(t, flow.Set(field.name, field.value))
	}
	return flow
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
