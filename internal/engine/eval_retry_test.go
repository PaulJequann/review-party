package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestRetryDelayHonorsProviderDelayWithinPolicyMaximum(t *testing.T) {
	policy := model.RetryPolicy{MaxAttempts: 3, InitialBackoff: "100ms", MaxBackoff: "1s"}
	if delay := retryDelay(policy, 1, 800*time.Millisecond); delay < 800*time.Millisecond || delay > time.Second {
		t.Fatalf("provider delay = %s", delay)
	}
	if delay := retryDelay(policy, 1, 2*time.Second); delay != time.Second {
		t.Fatalf("capped provider delay = %s", delay)
	}
}

func TestEvalRetryPublishesArtifactsUnderTheRetryAttemptNumber(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "retry"}})
	executor := &scriptedEvalExecutor{executions: []attemptExecution{
		{Outcome: model.AttemptTransientFailure, FailureCategory: model.TerminationTransportFailure, FailurePhase: model.PhaseReviewerExecution, Diagnostic: "temporary transport failure", AssistantText: "first attempt output"},
		{Outcome: model.AttemptCompleted, AssistantText: cleanReview},
	}}
	conductor := testEvalConductor(t, executor)
	conductor.getRunner().publisher = newArtifactPublisher(conductor.artifacts)
	conductor.wait = func(context.Context, time.Duration) error { return nil }
	selection := evalSelection(suite)
	selection.Experiment.RetryPolicy.MaxAttempts = 3
	run, err := conductor.RunEvalSuite(testContext(t), selection)
	if err != nil {
		t.Fatal(err)
	}
	evalRun, err := conductor.InspectEvalRun(context.Background(), run.EvalRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	review, err := conductor.Inspect(context.Background(), evalRun.ReviewID)
	if err != nil {
		t.Fatal(err)
	}

	assertArtifactsLiveUnderTheirAttemptNumber(t, review)
	first := review.Passes[0].Attempts[0].Artifacts[1]
	contents, err := conductor.artifacts.Read(first)
	if err != nil {
		t.Fatalf("attempt 1 assistant text no longer reads back: %v", err)
	}
	if string(contents) != "first attempt output" {
		t.Fatalf("attempt 1 assistant text = %q", contents)
	}
	if err := conductor.getRunner().VerifyArtifacts(review); err != nil {
		t.Fatal(err)
	}
}

func assertArtifactsLiveUnderTheirAttemptNumber(t *testing.T, review model.ReviewRecord) {
	t.Helper()
	attempts := review.Passes[0].Attempts
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want the transient attempt and its retry", len(attempts))
	}
	for index, attempt := range attempts {
		assertAttemptArtifactsUnder(t, attempt, index+1, filepath.Join("artifacts", string(review.ID), fmt.Sprint(index+1)))
	}
}

func assertAttemptArtifactsUnder(t *testing.T, attempt model.AttemptRecord, number int, directory string) {
	t.Helper()
	if attempt.Number != number {
		t.Fatalf("attempt %d records number %d", number, attempt.Number)
	}
	if len(attempt.Artifacts) == 0 {
		t.Fatalf("attempt %d published no artifacts", number)
	}
	for _, reference := range attempt.Artifacts {
		if filepath.Dir(reference.Path) != directory {
			t.Fatalf("attempt %d artifact %q is outside %q", number, reference.Path, directory)
		}
	}
}
