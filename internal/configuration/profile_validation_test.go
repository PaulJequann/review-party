package configuration

import (
	"strings"
	"testing"
)

func TestProfileRequiresEveryExecutionChoice(t *testing.T) {
	manager := testManager(t, t.TempDir())
	assertInvalidProfileDraft(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "Review security boundaries.",
	}, "model and reasoning_effort are required")
}

func TestProfileCreationRejectsInstructionsBeyondReadLimit(t *testing.T) {
	manager := testManager(t, t.TempDir())
	assertInvalidProfileDraft(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: strings.Repeat("x", MaximumDocumentBytes+1),
	}, "instructions exceed")
}

func TestProfileRejectsExcessiveAttemptDeadline(t *testing.T) {
	manager := testManager(t, t.TempDir())
	assertInvalidProfileDraft(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "87600h", Instructions: "Review security.\n",
	}, "must not exceed")
}

func assertInvalidProfileDraft(t *testing.T, manager *Manager, draft ProfileDraft, expected string) {
	t.Helper()
	plan, err := manager.PlanProfileCreation("", draft)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("Profile plan unexpectedly valid")
	}
	if !strings.Contains(plan.Reason(), expected) {
		t.Fatalf("reason = %q", plan.Reason())
	}
}
