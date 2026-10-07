package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
)

// partialFindingsReview is a findings review whose second section is missing
// its Evidence field, so the first finding survives and the second is dropped
// by the salvage tier.
const partialFindingsReview = `BEGIN_REVIEW
status: findings

1. HIGH | correctness | review.go:3
Failure: The changed state is not handled.
Evidence: The Subject changes the state without updating its caller.
Fix: Update the caller with the state change.
Test: Exercise the caller with the changed state.

2. MEDIUM | maintainability | parse.go:8
Failure: Second failure without evidence.
Fix: Second correction.
Test: Second regression.
END_REVIEW`

func TestPartialResultPreservesEvidenceButNeverCompletes(t *testing.T) {
	repository := changedTestRepository(t)
	conductor := testConductor(t, successfulExecutor(partialFindingsReview), time.Second)

	record, err := conductor.Review(testContext(t), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("lifecycle = %q, want %q", record.Lifecycle, model.LifecycleIncomplete)
	}
	if record.Result == nil || record.Result.Status != model.ResultFindingsPartial {
		t.Fatalf("result = %#v, want a partial result", record.Result)
	}
	if record.Result.FindingCount() != 1 {
		t.Fatalf("findings = %#v, want the salvaged finding", record.Result.Findings)
	}
	if record.Passes[0].Attempts[0].Outcome != model.AttemptCompleted {
		t.Fatalf("outcome = %q, want %q", record.Passes[0].Attempts[0].Outcome, model.AttemptCompleted)
	}
	if record.Termination == nil || record.Termination.Category != model.TerminationResultValidationFailure || record.Termination.Phase != model.PhaseResultValidation {
		t.Fatalf("termination = %#v, want result validation failure at result validation", record.Termination)
	}
	if !strings.Contains(record.Termination.Message, "incomplete") {
		t.Fatalf("termination message = %q, want the incompleteness report", record.Termination.Message)
	}
}

func TestPartialResultDoesNotAffectCleanLifecycle(t *testing.T) {
	repository := changedTestRepository(t)
	conductor := testConductor(t, successfulExecutor(findingsReview), time.Second)

	record, err := conductor.Review(testContext(t), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != model.LifecycleCompleted || record.Termination != nil {
		t.Fatalf("record = %#v, want a fully completed review", record)
	}
	if record.Result.Status != model.ResultFindings {
		t.Fatalf("status = %q, want %q", record.Result.Status, model.ResultFindings)
	}
}

func TestEvalRetryRetainsSalvagedPartialResultWhenRetryFails(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "partial-retry"}})
	base := filepath.Join(suite, "cases", "partial-retry", "base")
	head := filepath.Join(suite, "cases", "partial-retry", "head")
	executions := 0
	executor := &scriptedExecutor{availability: availability{Available: true}}
	outcomes := []attemptExecution{
		{AssistantText: partialFindingsReview, Outcome: model.AttemptCompleted},
		{AssistantText: "garbage without any review block", Outcome: model.AttemptCompleted},
	}
	executor.execute = func(context.Context, attemptSpec) attemptExecution {
		executions++
		removePreparationDirectory(t, base)
		return outcomes[executions-1]
	}
	conductor := testEvalConductor(t, executor)
	conductor.wait = func(context.Context, time.Duration) error { return nil }
	selection := model.ReviewSelection{
		Repository: suite, Subject: model.CapturedChange(base, head), Profile: "bugs",
		Reviewer: defaultReviewer, Model: "grok-code-fast-1", Effort: "high",
	}
	record, err := conductor.reviewEvalCase(testContext(t), selection, model.RetryPolicy{
		MaxAttempts: 2, InitialBackoff: "1ms", MaxBackoff: "1ms",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("lifecycle = %q, want %q", record.Lifecycle, model.LifecycleIncomplete)
	}
	if record.Result == nil || record.Result.Status != model.ResultFindingsPartial {
		t.Fatalf("result = %#v, want the retained partial result", record.Result)
	}
	if record.Result.FindingCount() != 1 {
		t.Fatalf("findings = %#v, want the salvaged finding from the first attempt", record.Result.Findings)
	}
	if record.Termination == nil || record.Termination.Category != model.TerminationResultValidationFailure {
		t.Fatalf("termination = %#v, want the last attempt's validation failure", record.Termination)
	}
	reloaded, err := conductor.Inspect(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Result == nil || reloaded.Result.Status != model.ResultFindingsPartial || reloaded.Result.FindingCount() != 1 {
		t.Fatalf("persisted result = %#v, want the retained partial result to survive the retry", reloaded.Result)
	}
}

func TestEvalRetryCompletingAfterPartialReplacesPartialWithCompleteResult(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "partial-replace"}})
	base := filepath.Join(suite, "cases", "partial-replace", "base")
	head := filepath.Join(suite, "cases", "partial-replace", "head")
	executions := 0
	executor := &scriptedExecutor{availability: availability{Available: true}}
	outcomes := []attemptExecution{
		{AssistantText: partialFindingsReview, Outcome: model.AttemptCompleted},
		{AssistantText: findingsReview, Outcome: model.AttemptCompleted},
	}
	executor.execute = func(context.Context, attemptSpec) attemptExecution {
		executions++
		removePreparationDirectory(t, base)
		return outcomes[executions-1]
	}
	conductor := testEvalConductor(t, executor)
	conductor.wait = func(context.Context, time.Duration) error { return nil }
	selection := model.ReviewSelection{
		Repository: suite, Subject: model.CapturedChange(base, head), Profile: "bugs",
		Reviewer: defaultReviewer, Model: "grok-code-fast-1", Effort: "high",
	}
	record, err := conductor.reviewEvalCase(testContext(t), selection, model.RetryPolicy{
		MaxAttempts: 2, InitialBackoff: "1ms", MaxBackoff: "1ms",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("lifecycle = %q, want %q", record.Lifecycle, model.LifecycleCompleted)
	}
	if record.Result == nil || record.Result.Status != model.ResultFindings {
		t.Fatalf("result = %#v, want the complete replacement result", record.Result)
	}
}
