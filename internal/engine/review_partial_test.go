package engine

import (
	"context"
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

	record, err := conductor.Review(context.Background(), testSelection(repository))
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

	record, err := conductor.Review(context.Background(), testSelection(repository))
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
