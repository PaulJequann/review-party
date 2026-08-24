package engine

import (
	"context"
	"errors"
	"os/exec"
	"reviewparty/internal/model"
	"testing"
	"time"
)

type stubHarnessAdapter struct {
	decode func([]byte) (decodedHarnessOutput, error)
}

func (stubHarnessAdapter) Name() string { return "stub" }

func (stubHarnessAdapter) Check(context.Context, reviewerCandidate) availability {
	return availability{Available: true}
}

func (stubHarnessAdapter) Prepare(attemptSpec) (preparedAttempt, error) {
	return preparedAttempt{command: exec.Command("stub")}, nil
}

func (adapter stubHarnessAdapter) Decode(output []byte) (decodedHarnessOutput, error) {
	return adapter.decode(output)
}

func TestDirectExecutorHandlesStartFailureBeforeDecoding(t *testing.T) {
	decoded := false
	executor := directExecutor{
		adapter: stubHarnessAdapter{decode: func([]byte) (decodedHarnessOutput, error) {
			decoded = true
			return decodedHarnessOutput{}, errors.New("decode should not run")
		}},
		run: func(context.Context, *exec.Cmd) commandRun {
			return commandRun{Stdout: []byte("not-json"), StartErr: errors.New("executable not found")}
		},
	}

	execution := executor.Execute(context.Background(), attemptSpec{})

	assertAttemptOutcome(t, execution, model.AttemptReviewerUnavailable)
	assertFailureLocation(t, execution, model.TerminationReviewerUnavailable, model.PhaseHarnessLaunch)
	if decoded {
		t.Fatal("decoder ran after command start failure")
	}
}

func TestDirectExecutorRejectsOverflowBeforeDecoding(t *testing.T) {
	decoded := false
	executor := directExecutor{
		adapter: stubHarnessAdapter{decode: func([]byte) (decodedHarnessOutput, error) {
			decoded = true
			return decodedHarnessOutput{assistantText: cleanReview}, nil
		}},
		run: func(context.Context, *exec.Cmd) commandRun {
			return commandRun{Stdout: []byte(cleanReview), OutputOverflow: true}
		},
	}

	execution := executor.Execute(context.Background(), attemptSpec{})

	assertAttemptOutcome(t, execution, model.AttemptInvalidResult)
	assertFailureLocation(t, execution, model.TerminationMalformedOutput, model.PhaseOutputCapture)
	if execution.Diagnostic != "stub output exceeded the capture limit" {
		t.Fatalf("diagnostic = %q", execution.Diagnostic)
	}
	if decoded {
		t.Fatal("decoder ran after output overflow")
	}
}

func TestNonzeroHarnessExitCannotCompleteValidPayload(t *testing.T) {
	run := commandRun{WaitErr: errors.New("exit status 1")}
	execution := finalizeHarnessRun(run, decodedHarnessOutput{assistantText: cleanReview, diagnostic: "authentication failed", model: "model", effort: "high"}, "reviewer")
	assertAttemptOutcome(t, execution, model.AttemptReviewerUnavailable)
	assertFailureLocation(t, execution, model.TerminationAuthenticationFailure, model.PhaseReviewerExecution)
	if execution.AssistantText != cleanReview {
		t.Fatalf("assistant text = %q", execution.AssistantText)
	}
}

func TestEmptyAssistantTextPreservesUnknownDiagnostic(t *testing.T) {
	execution := finalizeHarnessRun(commandRun{}, decodedHarnessOutput{diagnostic: "provider internal error"}, "reviewer")

	assertAttemptOutcome(t, execution, model.AttemptUnknownFailure)
	assertFailureLocation(t, execution, model.TerminationUnknownFailure, model.PhaseReviewerExecution)
	if execution.Diagnostic != "provider internal error" {
		t.Fatalf("diagnostic = %q", execution.Diagnostic)
	}
}

func TestContextTerminationClassificationSurvivesRecordTranslation(t *testing.T) {
	tests := map[string]struct {
		err      error
		outcome  model.AttemptOutcome
		category model.TerminationCategory
	}{
		"deadline": {
			err: context.DeadlineExceeded, outcome: model.AttemptTransientFailure, category: model.TerminationDeadlineExceeded,
		},
		"caller cancellation": {
			err: context.Canceled, outcome: model.AttemptCancelled, category: model.TerminationCancelled,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			execution := finalizeHarnessRun(commandRun{ContextErr: test.err}, decodedHarnessOutput{}, "reviewer")
			assertAttemptOutcome(t, execution, test.outcome)
			assertFailureLocation(t, execution, test.category, model.PhaseReviewerExecution)

			termination := terminationForAttempt(execution, execution.Outcome, nil)
			if termination.Category != test.category || termination.Phase != model.PhaseReviewerExecution {
				t.Fatalf("termination = %#v", termination)
			}
		})
	}
}

func TestDiagnosticFailureCategoryRecognizesTransportFailures(t *testing.T) {
	for _, diagnostic := range []string{
		"dial tcp: connection refused",
		"read: connection reset by peer",
		"network is unreachable",
		"upstream request timed out",
		"HTTP 503 Service Unavailable",
	} {
		if got := diagnosticFailureCategory(diagnostic); got != model.TerminationTransportFailure {
			t.Errorf("diagnostic %q classified as %q", diagnostic, got)
		}
	}
	if got := diagnosticFailureCategory("provider internal bookkeeping failed"); got != model.TerminationUnknownFailure {
		t.Fatalf("unknown diagnostic classified as %q", got)
	}
}

func assertAttemptOutcome(t *testing.T, execution attemptExecution, want model.AttemptOutcome) {
	t.Helper()
	if execution.Outcome != want {
		t.Fatalf("outcome = %q, want %q", execution.Outcome, want)
	}
}

func assertFailureLocation(t *testing.T, execution attemptExecution, category model.TerminationCategory, phase model.ExecutionPhase) {
	t.Helper()
	if execution.FailureCategory != category {
		t.Fatalf("failure category = %q, want %q", execution.FailureCategory, category)
	}
	if execution.FailurePhase != phase {
		t.Fatalf("failure phase = %q, want %q", execution.FailurePhase, phase)
	}
}

func TestProcessGroupCanBeTerminated(t *testing.T) {
	command := exec.Command("sh", "-c", "sleep 30")
	configureProcessGroup(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	terminateProcessGroup(command)

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		killProcessGroup(command)
		t.Fatal("process did not terminate")
	}
}

func TestCommandRunnerReturnsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	run := runCommand(ctx, exec.Command("sh", "-c", "sleep 30"))
	if !errors.Is(run.ContextErr, context.DeadlineExceeded) {
		t.Fatalf("context error = %v, want deadline exceeded", run.ContextErr)
	}
}

func TestBoundedBufferReportsOverflowWithoutGrowing(t *testing.T) {
	buffer := newBoundedBuffer(4)
	written, err := buffer.Write([]byte("123456"))
	if err != nil {
		t.Fatal(err)
	}
	if written != 6 {
		t.Fatalf("written = %d", written)
	}
	if !buffer.overflow {
		t.Fatal("buffer did not report overflow")
	}
	if string(buffer.Bytes()) != "1234" {
		t.Fatalf("buffer = %q", buffer.Bytes())
	}
}

func TestReviewerEnvironmentExcludesUnrelatedCallerSecret(t *testing.T) {
	t.Setenv("REVIEW_PARTY_UNRELATED_SECRET", "must-not-leak")
	t.Setenv("GITHUB_TOKEN", "allowed-token")
	command := exec.Command("sh", "-c", `test "$GITHUB_TOKEN" = allowed-token && test -z "$REVIEW_PARTY_UNRELATED_SECRET"`)
	command.Env = reviewerEnvironment("copilot")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("environment policy: %v\n%s", err, output)
	}
}
