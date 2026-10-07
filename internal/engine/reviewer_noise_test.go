package engine

import (
	"context"
	"errors"
	"os/exec"
	"reviewparty/internal/model"
	"strings"
	"testing"
)

const codexWebsocketNoise = "2026-09-25T23:03:05.817653Z ERROR codex_api::endpoint::responses_websocket: failed to connect to websocket: HTTP error: 405 Method Not Allowed, url: wss://chatgpt.com/backend-api/codex/responses\n" +
	"2026-09-25T23:03:09.847729Z ERROR codex_api::endpoint::responses_websocket: failed to connect to websocket: HTTP error: 405 Method Not Allowed, url: wss://chatgpt.com/backend-api/codex/responses\n"

const codexUnauthorizedMessage = "unexpected status 401 Unauthorized: Missing bearer or basic authentication in header, url: https://api.openai.com/v1/responses"

func codexEventStream(events ...string) []byte {
	return []byte(strings.Join(events, "\n") + "\n")
}

func codexRetryEvent(message string) string {
	return `{"type":"error","message":"` + message + `"}`
}

func codexMessageEvent(text string) string {
	return `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"` + strings.ReplaceAll(text, "\n", `\n`) + `"}}`
}

func executeWithStub(t *testing.T, adapter harnessAdapter, run commandRun) attemptExecution {
	t.Helper()
	executor := directExecutor{adapter: adapter, run: func(context.Context, *exec.Cmd) commandRun { return run }}
	return executor.Execute(context.Background(), attemptSpec{Repository: t.TempDir(), Candidate: reviewerCandidate{ID: adapter.Name(), Model: "model"}})
}

func TestCodexCompletedAttemptKeepsTransportNoiseOutOfDiagnostic(t *testing.T) {
	stdout := codexEventStream(
		`{"type":"thread.started","thread_id":"t"}`,
		codexRetryEvent("Reconnecting... 1/5 (failed to connect to websocket: HTTP error: 405 Method Not Allowed)"),
		codexRetryEvent("Falling back from WebSockets to HTTPS transport. failed to connect to websocket: HTTP error: 405 Method Not Allowed"),
		codexMessageEvent(cleanReview),
		`{"type":"turn.completed","usage":{}}`,
	)

	execution := executeWithStub(t, codexAdapter{}, commandRun{Stdout: stdout, Stderr: codexWebsocketNoise})

	if execution.Outcome != model.AttemptCompleted {
		t.Fatalf("outcome = %q", execution.Outcome)
	}
	if execution.Diagnostic != "" {
		t.Fatalf("diagnostic = %q, want empty", execution.Diagnostic)
	}
	for _, want := range []string{"405 Method Not Allowed", "Reconnecting... 1/5", "Falling back from WebSockets"} {
		if !strings.Contains(execution.ReviewerNoise, want) {
			t.Fatalf("reviewer noise = %q, want %q preserved", execution.ReviewerNoise, want)
		}
	}
}

func TestCodexAuthenticationFailureDiagnosticLeadsWithTheCause(t *testing.T) {
	stdout := codexEventStream(
		`{"type":"thread.started","thread_id":"t"}`,
		codexRetryEvent("Reconnecting... 1/5 (failed to connect to websocket: HTTP error: 405 Method Not Allowed)"),
		codexRetryEvent(codexUnauthorizedMessage),
		`{"type":"turn.failed","error":{"message":"`+codexUnauthorizedMessage+`"}}`,
	)

	execution := executeWithStub(t, codexAdapter{}, commandRun{Stdout: stdout, Stderr: codexWebsocketNoise, WaitErr: errors.New("exit status 1")})

	assertAttemptOutcome(t, execution, model.AttemptReviewerUnavailable)
	assertFailureLocation(t, execution, model.TerminationAuthenticationFailure, model.PhaseReviewerExecution)
	if !strings.HasPrefix(execution.Diagnostic, "unexpected status 401 Unauthorized") {
		t.Fatalf("diagnostic = %q, want it to lead with the 401", execution.Diagnostic)
	}
	if strings.Contains(execution.Diagnostic, "405") {
		t.Fatalf("diagnostic = %q, want no 405 transport noise", execution.Diagnostic)
	}
	if !strings.Contains(execution.ReviewerNoise, "405 Method Not Allowed") {
		t.Fatalf("reviewer noise = %q, want 405 preserved", execution.ReviewerNoise)
	}
}

func TestCodexAuthenticationCauseOnStderrLeadsTheDiagnostic(t *testing.T) {
	stderr := codexWebsocketNoise + "2026-09-25T23:03:10.000000Z ERROR codex_core::codex: " + codexUnauthorizedMessage + "\n"

	execution := executeWithStub(t, codexAdapter{}, commandRun{Stdout: codexEventStream(`{"type":"thread.started","thread_id":"t"}`), Stderr: stderr, WaitErr: errors.New("exit status 1")})

	assertFailureLocation(t, execution, model.TerminationAuthenticationFailure, model.PhaseReviewerExecution)
	if !strings.Contains(execution.Diagnostic, "401 Unauthorized") || strings.Contains(execution.Diagnostic, "405") {
		t.Fatalf("diagnostic = %q, want the 401 cause without the 405 lines", execution.Diagnostic)
	}
}

func TestClaudeCompletedAttemptKeepsStderrOutOfDiagnostic(t *testing.T) {
	stdout := []byte(`{"type":"assistant","message":{"model":"m","content":[{"type":"text","text":"` + strings.ReplaceAll(cleanReview, "\n", `\n`) + `"}]}}` + "\n" +
		`{"type":"result","subtype":"success","is_error":false}` + "\n")

	execution := executeWithStub(t, claudeAdapter{}, commandRun{Stdout: stdout, Stderr: "warning: transport retry\n"})

	if execution.Outcome != model.AttemptCompleted || execution.Diagnostic != "" {
		t.Fatalf("outcome = %q diagnostic = %q, want completed with empty diagnostic", execution.Outcome, execution.Diagnostic)
	}
	if !strings.Contains(execution.ReviewerNoise, "transport retry") {
		t.Fatalf("reviewer noise = %q", execution.ReviewerNoise)
	}
}

func TestClaudeInBandFailureDiagnosticExcludesStderr(t *testing.T) {
	stdout := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Invalid API key"}` + "\n")

	execution := executeWithStub(t, claudeAdapter{}, commandRun{Stdout: stdout, Stderr: "warning: transport retry\n"})

	if execution.Diagnostic != "Invalid API key" {
		t.Fatalf("diagnostic = %q", execution.Diagnostic)
	}
}

func TestStderrOnlyFailureStillExplainsTheAttempt(t *testing.T) {
	execution := executeWithStub(t, stubHarnessAdapter{decode: func([]byte) (decodedHarnessOutput, error) {
		return decodedHarnessOutput{}, nil
	}}, commandRun{Stderr: "provider internal error\n", WaitErr: errors.New("exit status 1")})

	if execution.Diagnostic != "provider internal error" {
		t.Fatalf("diagnostic = %q, want the only failure text available", execution.Diagnostic)
	}
}

func TestFailedAttemptPublishesReviewerNoiseAsAnArtifact(t *testing.T) {
	publisher := newArtifactPublisher(mustNewArtifactStore(t, t.TempDir()))
	runner := &reviewRunner{publisher: publisher}

	attempt, err := runner.buildAttempt(attemptDraft{reviewID: "rp_1723200000000_0123456789abcdef", number: 1, execution: attemptExecution{AssistantText: cleanReview, ReviewerNoise: codexWebsocketNoise}, outcome: model.AttemptInvalidResult, failed: true})
	if err != nil {
		t.Fatal(err)
	}

	var kinds []string
	for _, reference := range attempt.Artifacts {
		kinds = append(kinds, reference.Kind)
	}
	if strings.Join(kinds, ",") != "assistant-text,reviewer-noise" {
		t.Fatalf("artifact kinds = %v", kinds)
	}
	contents, err := publisher.store.Read(attempt.Artifacts[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != codexWebsocketNoise {
		t.Fatalf("noise artifact = %q", contents)
	}
}

func TestQuietFailedAttemptPublishesNoNoiseArtifact(t *testing.T) {
	runner := &reviewRunner{publisher: newArtifactPublisher(mustNewArtifactStore(t, t.TempDir()))}

	attempt, err := runner.buildAttempt(attemptDraft{reviewID: "rp_1723200000000_0123456789abcdef", number: 1, execution: attemptExecution{AssistantText: cleanReview}, outcome: model.AttemptInvalidResult, failed: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(attempt.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want assistant text only", len(attempt.Artifacts))
	}
}

func TestCodexFailureWithStderrCauseKeepsTheInBandDiagnosticInTheNoiseRecord(t *testing.T) {
	stdout := codexEventStream(
		`{"type":"thread.started","thread_id":"t"}`,
		codexRetryEvent("codex sandbox: seccomp filter install failed"),
	)
	stderr := "2026-09-25T23:03:10.000000Z ERROR codex_core::codex: " + codexUnauthorizedMessage + "\n"

	execution := executeWithStub(t, codexAdapter{}, commandRun{Stdout: stdout, Stderr: stderr, WaitErr: errors.New("exit status 1")})

	assertFailureLocation(t, execution, model.TerminationAuthenticationFailure, model.PhaseReviewerExecution)
	if !strings.Contains(execution.Diagnostic, "401 Unauthorized") {
		t.Fatalf("diagnostic = %q, want the stderr 401 cause", execution.Diagnostic)
	}
	if !strings.Contains(execution.ReviewerNoise, "seccomp filter install failed") {
		t.Fatalf("reviewer noise = %q, want the in-band seccomp error preserved", execution.ReviewerNoise)
	}
}

func TestCodexCancelledAttemptKeepsTheInBandDiagnosticInTheNoiseRecord(t *testing.T) {
	stdout := codexEventStream(
		`{"type":"thread.started","thread_id":"t"}`,
		codexRetryEvent("429 Too Many Requests"),
	)

	execution := executeWithStub(t, codexAdapter{}, commandRun{Stdout: stdout, WaitErr: errors.New("signal: killed"), ContextErr: context.DeadlineExceeded})

	assertFailureLocation(t, execution, model.TerminationDeadlineExceeded, model.PhaseReviewerExecution)
	if !strings.Contains(execution.ReviewerNoise, "429 Too Many Requests") {
		t.Fatalf("reviewer noise = %q, want the in-band error preserved", execution.ReviewerNoise)
	}
}

func TestCodexCompletedAttemptSurfacesAnInBandErrorThatIsNotTransportNoise(t *testing.T) {
	stdout := codexEventStream(
		`{"type":"thread.started","thread_id":"t"}`,
		codexRetryEvent("Reconnecting... 1/5 (failed to connect to websocket: HTTP error: 405 Method Not Allowed)"),
		codexMessageEvent(cleanReview),
		`{"type":"turn.failed","error":{"message":"`+codexUnauthorizedMessage+`"}}`,
	)

	execution := executeWithStub(t, codexAdapter{}, commandRun{Stdout: stdout})

	if execution.Outcome != model.AttemptCompleted {
		t.Fatalf("outcome = %q", execution.Outcome)
	}
	if !strings.Contains(execution.Diagnostic, "401 Unauthorized") || strings.Contains(execution.Diagnostic, "405") {
		t.Fatalf("diagnostic = %q, want the 401 without the 405 reconnect", execution.Diagnostic)
	}
	if !strings.Contains(execution.ReviewerNoise, "Reconnecting... 1/5") {
		t.Fatalf("reviewer noise = %q, want the reconnect preserved", execution.ReviewerNoise)
	}
}
