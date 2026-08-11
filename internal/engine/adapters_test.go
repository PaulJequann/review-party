package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCopilotCommandRestrictsVisibleTools(t *testing.T) {
	candidate := reviewerCandidate{Model: "model", Effort: "high"}
	got := copilotCommand(candidate)
	want := []string{"copilot", "--available-tools=view,grep,glob", "--deny-tool=shell", "--deny-tool=write", "--deny-tool=url", "--disable-builtin-mcps", "--no-ask-user", "--allow-all-tools", "--no-color", "--output-format=json", "--model=model", "--effort=high"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestCopilotAutoModelOmitsEffortAndCapturesResolution(t *testing.T) {
	command := copilotCommand(reviewerCandidate{Model: "auto", Effort: "auto"})
	if slicesContainPrefix(command, "--effort=") {
		t.Fatalf("auto model command contains incompatible effort flag: %#v", command)
	}
	output := []byte("{\"type\":\"session.auto_mode_resolved\",\"data\":{\"chosenModel\":\"gpt-5-mini\",\"reasoningBucket\":\"low\"}}\n" +
		"{\"type\":\"assistant.message\",\"data\":{\"model\":\"gpt-5-mini\",\"content\":\"" + strings.ReplaceAll(cleanReview, "\n", "\\n") + "\"}}\n")
	decoded, err := decodeCopilotOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.model != "gpt-5-mini" {
		t.Fatalf("model = %q", decoded.model)
	}
	if decoded.effort != "low" {
		t.Fatalf("effort = %q", decoded.effort)
	}
	if decoded.assistantText != cleanReview {
		t.Fatalf("assistant text = %q", decoded.assistantText)
	}
}

func TestCopilotAutoModelRejectsExplicitEffort(t *testing.T) {
	check := (copilotAdapter{}).Check(context.Background(), reviewerCandidate{Model: "auto", Effort: "high"})
	if check.Available || !strings.Contains(check.Diagnostic, "does not support explicit effort") {
		t.Fatalf("availability = %#v, want explicit unsupported-effort diagnostic", check)
	}
}

func TestCopilotDecoderPreservesAssistantMessageBoundaries(t *testing.T) {
	output := []byte("{\"type\":\"assistant.message\",\"data\":{\"content\":\"Inspecting files.\"}}\n" +
		"{\"type\":\"assistant.message\",\"data\":{\"content\":\"" + strings.ReplaceAll(cleanReview, "\n", "\\n") + "\"}}\n")
	decoded, err := decodeCopilotOutput(output)
	assertReviewWithPreamble(t, decoded.assistantText, err)
}

func TestCopilotUnavailableModelIsClassifiedWithoutRetry(t *testing.T) {
	execution := classifyCopilotFailure(`Error: Model "gpt-5.6-luna" from --model flag is not available.`, context.Canceled)
	if execution.Outcome != AttemptReviewerUnavailable {
		t.Fatalf("outcome = %q", execution.Outcome)
	}
}

func TestReviewerSelectionChangesProfileRevision(t *testing.T) {
	executor := successfulExecutor(cleanReview)
	capabilities := restrictedReviewCapabilities()
	catalog := newReviewerCatalog([]reviewerRegistration{
		{candidate: reviewerCandidate{ID: "reviewer-a", Model: "same-model", Effort: "same-effort", Harness: "same-harness", Transport: "same-transport"}, capabilities: capabilities, executor: executor},
		{candidate: reviewerCandidate{ID: "reviewer-b", Model: "same-model", Effort: "same-effort", Harness: "same-harness", Transport: "same-transport"}, capabilities: capabilities, executor: executor},
	})
	first, err := compileSelectedTestProfile(catalog, ProfileSelection{Profile: "bugs", Reviewer: "reviewer-a"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compileSelectedTestProfile(catalog, ProfileSelection{Profile: "bugs", Reviewer: "reviewer-b"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if first.revision.Revision == second.revision.Revision {
		t.Fatal("different reviewers produced the same Profile Revision")
	}
}

func TestCompiledBugProfileIncludesPromisedPass(t *testing.T) {
	profile, err := compileSelectedTestProfile(defaultReviewerCatalog(), ProfileSelection{Profile: "bugs", Reviewer: "grok"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	passes := profile.revision.Passes
	if len(passes) != 1 {
		t.Fatalf("passes = %#v, want one pass", passes)
	}
	if passes[0].Name != "bug-review" {
		t.Fatalf("pass name = %q, want bug-review", passes[0].Name)
	}
	if !passes[0].Required {
		t.Fatal("bug-review pass is not required")
	}
}

func TestSupportedReviewersResolveToMatchingAdapters(t *testing.T) {
	catalog := defaultReviewerCatalog()
	want := []string{"copilot", "grok", "opencode"}
	wantCandidates := map[string]reviewerCandidate{
		"copilot":  {ID: "copilot", Model: "auto", Effort: "auto", Harness: "github-copilot-cli", Transport: "direct-cli"},
		"grok":     {ID: "grok", Model: "grok-4.5", Effort: "high", Harness: "grok-build-cli", Transport: "direct-cli"},
		"opencode": {ID: "opencode", Effort: "default", Harness: "opencode-cli", Transport: "direct-cli"},
	}
	if got := SupportedReviewers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported reviewers = %#v, want %#v", got, want)
	}
	for _, id := range want {
		registration, err := catalog.resolve(id)
		if err != nil {
			t.Fatalf("resolve %q: %v", id, err)
		}
		if !reflect.DeepEqual(registration.candidate, wantCandidates[id]) {
			t.Fatalf("candidate for %q = %#v, want %#v", id, registration.candidate, wantCandidates[id])
		}
		executor, ok := registration.executor.(directExecutor)
		if !ok || executor.adapter.Name() != id {
			t.Fatalf("executor for %q = %#v", id, registration.executor)
		}
		if !reflect.DeepEqual(registration.capabilities, restrictedReviewCapabilities()) {
			t.Fatalf("capabilities for %q = %#v", id, registration.capabilities)
		}
	}
}

func TestGrokCommandRestrictsCapabilities(t *testing.T) {
	got := grokCommand(reviewerCandidate{Model: "grok-4.5", Effort: "high"}, "/repo", "/tmp/prompt")
	want := []string{"grok", "--prompt-file", "/tmp/prompt", "--cwd", "/repo", "--model", "grok-4.5", "--reasoning-effort", "high", "--tools", "view,grep,glob", "--disable-web-search", "--no-subagents", "--no-memory", "--no-plan", "--permission-mode", "dontAsk", "--max-turns", "30", "--output-format", "streaming-json", "--verbatim"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestGrokDecoderPreservesAssistantMessageBoundaries(t *testing.T) {
	output := []byte("{\"type\":\"text\",\"data\":\"Checking first.\"}\n" +
		"{\"type\":\"tool_call\",\"data\":\"ignored\"}\n" +
		"{\"type\":\"text\",\"data\":\"BEGIN_\"}\n" +
		"{\"type\":\"text\",\"data\":\"REVIEW\\nstatus: clean\\nsummary: No actionable findings.\\nEND_REVIEW\"}\n")
	decoded, err := decodeGrokOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != "Checking first.\n"+cleanReview {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestOpenCodeCommandAndConfigDenyUnreviewedCapabilities(t *testing.T) {
	command := openCodeCommand(reviewerCandidate{Model: "meta/muse-spark-1.2-contributor", Effort: "default"})
	want := []string{"opencode", "run", "--pure", "--agent", "build", "--format", "json", "--model", "meta/muse-spark-1.2-contributor"}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("command = %#v", command)
	}
	var config struct {
		Permission map[string]string `json:"permission"`
		Share      string            `json:"share"`
	}
	if err := json.Unmarshal([]byte(openCodeReviewConfig), &config); err != nil {
		t.Fatal(err)
	}
	wantPermissions := map[string]string{"*": "deny", "read": "allow", "glob": "allow", "grep": "allow", "list": "allow"}
	if !reflect.DeepEqual(config.Permission, wantPermissions) {
		t.Fatalf("permissions = %#v, want %#v", config.Permission, wantPermissions)
	}
	if config.Share != "disabled" {
		t.Fatalf("share = %q", config.Share)
	}
}

func TestOpenCodeCommandRequestsExplicitEffortVariant(t *testing.T) {
	command := openCodeCommand(reviewerCandidate{Model: "meta/muse-spark-1.2-contributor", Effort: "high"})
	want := []string{"opencode", "run", "--pure", "--agent", "build", "--format", "json", "--model", "meta/muse-spark-1.2-contributor", "--variant", "high"}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("command = %#v, want %#v", command, want)
	}
}

func TestOpenCodeDecoderUsesOnlyTextEvents(t *testing.T) {
	output := []byte("{\"type\":\"step_start\",\"part\":{\"text\":\"ignored\"}}\n" +
		"{\"type\":\"text\",\"part\":{\"text\":\"" + strings.ReplaceAll(cleanReview, "\n", "\\n") + "\"}}\n")
	decoded, err := decodeOpenCodeOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.assistantText != cleanReview {
		t.Fatalf("assistant text = %q", decoded.assistantText)
	}
}

func TestOpenCodeDecoderPreservesCompletedTextPartBoundaries(t *testing.T) {
	output := []byte("{\"type\":\"text\",\"part\":{\"text\":\"Inspecting files.\"}}\n" +
		"{\"type\":\"text\",\"part\":{\"text\":\"" + strings.ReplaceAll(cleanReview, "\n", "\\n") + "\"}}\n")
	decoded, err := decodeOpenCodeOutput(output)
	assertReviewWithPreamble(t, decoded.assistantText, err)
}

func assertReviewWithPreamble(t *testing.T, assistantText string, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if assistantText != "Inspecting files.\n"+cleanReview {
		t.Fatalf("assistant text = %q", assistantText)
	}
	if _, err := parseReviewResult(assistantText); err != nil {
		t.Fatalf("parse review result: %v", err)
	}
}

func TestOpenCodeAuthenticationFailureIsUnavailable(t *testing.T) {
	execution := classifyHarnessFailure("Token refresh failed: 401", context.Canceled)
	assertAttemptOutcome(t, execution, AttemptReviewerUnavailable)
	assertFailureLocation(t, execution, TerminationAuthenticationFailure, PhaseReviewerExecution)
}

func TestDecodeFailurePreservesHarnessFailureClassification(t *testing.T) {
	run := commandRun{Stdout: []byte("not-json"), Stderr: "authentication failed", WaitErr: errors.New("exit status 1")}
	execution := decodedRunFailure(run, errors.New("decode event"), "opencode")
	assertAttemptOutcome(t, execution, AttemptReviewerUnavailable)
	assertFailureLocation(t, execution, TerminationAuthenticationFailure, PhaseReviewerExecution)
	if execution.AssistantText != "not-json" {
		t.Fatalf("assistant text = %q", execution.AssistantText)
	}
}
