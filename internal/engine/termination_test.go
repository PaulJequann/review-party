package engine

import (
	"errors"
	"testing"
)

func TestAttemptTerminationClassification(t *testing.T) {
	tests := map[string]struct {
		execution attemptExecution
		outcome   AttemptOutcome
		parseErr  error
		category  TerminationCategory
		phase     ExecutionPhase
	}{
		"deadline": {
			execution: attemptExecution{FailureCategory: TerminationDeadlineExceeded, FailurePhase: PhaseReviewerExecution},
			outcome:   AttemptTransientFailure,
			category:  TerminationDeadlineExceeded, phase: PhaseReviewerExecution,
		},
		"caller cancellation": {
			execution: attemptExecution{FailureCategory: TerminationCancelled, FailurePhase: PhaseReviewerExecution},
			outcome:   AttemptCancelled,
			category:  TerminationCancelled, phase: PhaseReviewerExecution,
		},
		"transport": {
			execution: failedExecution(AttemptTransientFailure, TerminationTransportFailure, PhaseReviewerExecution, ""),
			outcome:   AttemptTransientFailure,
			category:  TerminationTransportFailure, phase: PhaseReviewerExecution,
		},
		"malformed harness output": {
			execution: attemptExecution{
				FailureCategory: TerminationMalformedOutput,
				FailurePhase:    PhaseOutputDecode,
			},
			outcome:  AttemptInvalidResult,
			category: TerminationMalformedOutput, phase: PhaseOutputDecode,
		},
		"canonical validation": {
			execution: attemptExecution{
				Outcome:         AttemptCompleted,
				FailureCategory: TerminationResultValidationFailure,
				FailurePhase:    PhaseResultValidation,
			},
			outcome:  AttemptInvalidResult,
			parseErr: errors.New("missing END_REVIEW"),
			category: TerminationResultValidationFailure, phase: PhaseResultValidation,
		},
		"reviewer unavailable": {
			execution: failedExecution(AttemptReviewerUnavailable, TerminationReviewerUnavailable, PhaseHarnessLaunch, ""),
			outcome:   AttemptReviewerUnavailable,
			category:  TerminationReviewerUnavailable, phase: PhaseHarnessLaunch,
		},
		"unknown": {
			execution: failedExecution(AttemptUnknownFailure, TerminationUnknownFailure, PhaseReviewerExecution, ""),
			outcome:   AttemptUnknownFailure,
			category:  TerminationUnknownFailure, phase: PhaseReviewerExecution,
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
