package engine

import (
	"path/filepath"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func TestSavedProfileReviewerPolicyFailsClosed(t *testing.T) {
	repository := changedTestRepository(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "config.json"), `{"schema_version":1,"reviewers":{"grok":{"enabled":false}}}`)
	manager := configuration.NewManager(configuration.Options{GlobalRoot: root, Reviewers: []string{"grok", "opencode", "copilot", "codex"}})
	publishTestProfile(t, manager, configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "Review bugs.\n",
	})
	executor := successfulExecutor(cleanReview)
	conductor := newConfiguredTestConductor(t, manager, map[string]attemptExecutor{"grok": executor})
	_, err := conductor.Review(testContext(t), model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Profile: "bugs"})
	if err == nil {
		t.Fatal("disabled saved Reviewer was accepted")
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d", executor.attemptCount())
	}
}

func publishTestProfile(t *testing.T, manager *configuration.Manager, draft configuration.ProfileDraft) {
	t.Helper()
	plan, err := manager.PlanProfileCreation("", draft)
	if err != nil || !plan.Valid() {
		t.Fatalf("Profile plan error = %v, reason = %q", err, plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func newConfiguredTestConductor(t *testing.T, manager *configuration.Manager, executors map[string]attemptExecutor) *Conductor {
	t.Helper()
	ledger, err := store.NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	conductor, err := newConductorWithManager(ledger, catalogWithExecutors(executors), manager, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return conductor
}
