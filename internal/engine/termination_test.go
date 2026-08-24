package engine

import (
	"errors"
	"reviewparty/internal/model"
	"testing"
)

func TestAttemptTerminationClassification(t *testing.T) {
	tests := map[string]struct {
		execution attemptExecution
		outcome   model.AttemptOutcome
		parseErr  error
		category  model.TerminationCategory
		phase     model.ExecutionPhase
	}{
		"deadline": {
			execution: attemptExecution{FailureCategory: model.TerminationDeadlineExceeded, FailurePhase: model.PhaseReviewerExecution},
			outcome:   model.AttemptTransientFailure,
			category:  model.TerminationDeadlineExceeded, phase: model.PhaseReviewerExecution,
		},
		"caller cancellation": {
			execution: attemptExecution{FailureCategory: model.TerminationCancelled, FailurePhase: model.PhaseReviewerExecution},
			outcome:   model.AttemptCancelled,
			category:  model.TerminationCancelled, phase: model.PhaseReviewerExecution,
		},
		"transport": {
			execution: failedExecution(model.AttemptTransientFailure, model.TerminationTransportFailure, model.PhaseReviewerExecution, ""),
			outcome:   model.AttemptTransientFailure,
			category:  model.TerminationTransportFailure, phase: model.PhaseReviewerExecution,
		},
		"malformed harness output": {
			execution: attemptExecution{
				FailureCategory: model.TerminationMalformedOutput,
				FailurePhase:    model.PhaseOutputDecode,
			},
			outcome:  model.AttemptInvalidResult,
			category: model.TerminationMalformedOutput, phase: model.PhaseOutputDecode,
		},
		"canonical validation": {
			execution: attemptExecution{
				Outcome:         model.AttemptCompleted,
				FailureCategory: model.TerminationResultValidationFailure,
				FailurePhase:    model.PhaseResultValidation,
			},
			outcome:  model.AttemptInvalidResult,
			parseErr: errors.New("missing END_REVIEW"),
			category: model.TerminationResultValidationFailure, phase: model.PhaseResultValidation,
		},
		"reviewer unavailable": {
			execution: failedExecution(model.AttemptReviewerUnavailable, model.TerminationReviewerUnavailable, model.PhaseHarnessLaunch, ""),
			outcome:   model.AttemptReviewerUnavailable,
			category:  model.TerminationReviewerUnavailable, phase: model.PhaseHarnessLaunch,
		},
		"unknown": {
			execution: failedExecution(model.AttemptUnknownFailure, model.TerminationUnknownFailure, model.PhaseReviewerExecution, ""),
			outcome:   model.AttemptUnknownFailure,
			category:  model.TerminationUnknownFailure, phase: model.PhaseReviewerExecution,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			termination := terminationForAttempt(test.execution, test.outcome, test.parseErr)
			if termination.Category != test.category || termination.Phase != test.phase {
				t.Fatalf("termination = %#v, want category %q phase %q", termination, test.category, test.phase)
			}
		})
	}
}
