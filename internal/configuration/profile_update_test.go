package configuration

import (
	"strings"
	"testing"
)

func publishExecutionProfile(t *testing.T, manager *Manager, draft ProfileDraft) {
	t.Helper()
	plan, err := manager.PlanProfileCreation("", draft)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("creation plan invalid: %s", plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func TestPlanProfileUpdateRewritesExecutionOnly(t *testing.T) {
	manager := testManager(t, t.TempDir())
	publishExecutionProfile(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review bugs.\n",
	})
	plan := requireExecutionUpdate(t, manager, ProfileExecutionUpdate{Model: "grok-4.6"})
	requireSingleModelChange(t, plan)
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	requireStoredExecution(t, manager)
}

func requireExecutionUpdate(t *testing.T, manager *Manager, update ProfileExecutionUpdate) Plan {
	t.Helper()
	plan, err := manager.PlanProfileUpdate("", ScopeGlobal, "bugs", update)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("update plan invalid: %s", plan.Reason())
	}
	return plan
}

func requireSingleModelChange(t *testing.T, plan Plan) {
	t.Helper()
	if len(plan.Changes()) != 1 {
		t.Fatalf("changes = %#v", plan.Changes())
	}
	if plan.Changes()[0].Field != "profiles.bugs.model" {
		t.Fatalf("changes = %#v", plan.Changes())
	}
}

func requireStoredExecution(t *testing.T, manager *Manager) {
	t.Helper()
	profile, found, err := manager.LoadProfile(ScopeGlobal, "", "bugs")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("updated Profile was not found")
	}
	if profile.Model != "grok-4.6" {
		t.Fatalf("model = %q, want grok-4.6", profile.Model)
	}
	if profile.Reviewer != "grok" {
		t.Fatalf("reviewer = %q, want grok", profile.Reviewer)
	}
	if profile.Instructions != "Review bugs.\n" {
		t.Fatalf("instructions = %q", profile.Instructions)
	}
}

func TestPlanProfileUpdateReportsNoChange(t *testing.T) {
	manager := publishInvalidUpdateFixture(t)
	plan, err := manager.PlanProfileUpdate("", ScopeGlobal, "bugs", ProfileExecutionUpdate{Model: "grok-4.5"})
	if err != nil {
		t.Fatal(err)
	}
	requireInvalidPlan(t, plan, "already uses")
}

func TestPlanProfileUpdateRejectsUnknownProfile(t *testing.T) {
	manager := testManager(t, t.TempDir())
	if _, err := manager.PlanProfileUpdate("", ScopeGlobal, "missing", ProfileExecutionUpdate{Model: "x"}); err == nil {
		t.Fatal("missing profile update succeeded")
	}
}

func TestPlanProfileUpdateRequiresAField(t *testing.T) {
	manager := testManager(t, t.TempDir())
	if _, err := manager.PlanProfileUpdate("", ScopeGlobal, "bugs", ProfileExecutionUpdate{}); err == nil {
		t.Fatal("empty update succeeded")
	}
}

func TestPlanProfileUpdateValidatesExecution(t *testing.T) {
	manager := publishInvalidUpdateFixture(t)
	plan, err := manager.PlanProfileUpdate("", ScopeGlobal, "bugs", ProfileExecutionUpdate{AttemptDeadline: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	requireInvalidPlan(t, plan, "attempt_deadline")
}

func publishInvalidUpdateFixture(t *testing.T) *Manager {
	t.Helper()
	manager := testManager(t, t.TempDir())
	publishExecutionProfile(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review.\n",
	})
	return manager
}

func requireInvalidPlan(t *testing.T, plan Plan, want string) {
	t.Helper()
	if plan.Valid() {
		t.Fatal("invalid update unexpectedly valid")
	}
	if !strings.Contains(plan.Reason(), want) {
		t.Fatalf("reason = %q, want %q", plan.Reason(), want)
	}
}
