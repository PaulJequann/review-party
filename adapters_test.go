package reviewparty

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
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

func TestCopilotUnavailableModelIsClassifiedWithoutRetry(t *testing.T) {
	execution := classifyCopilotFailure(`Error: Model "gpt-5.6-luna" from --model flag is not available.`, context.Canceled)
	if execution.Outcome != AttemptReviewerUnavailable {
		t.Fatalf("outcome = %q", execution.Outcome)
	}
}

func TestReviewerSelectionChangesProfileRevision(t *testing.T) {
	subject := ReviewSubject{Kind: SubjectWorkingChanges, Identity: "subject", Patch: "patch"}
	catalog := defaultReviewerCatalog()
	grok, err := compileProfile(catalog, "bugs", "grok", subject)
	if err != nil {
		t.Fatal(err)
	}
	opencode, err := compileProfile(catalog, "bugs", "opencode", subject)
	if err != nil {
		t.Fatal(err)
	}
	if grok.revision.Revision == opencode.revision.Revision {
		t.Fatal("different reviewers produced the same Profile Revision")
	}
}

func TestCompiledBugProfileIncludesPromisedPass(t *testing.T) {
	profile, err := compileProfile(defaultReviewerCatalog(), "bugs", "grok", ReviewSubject{})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.passes) != 1 {
		t.Fatalf("passes = %#v, want one pass", profile.passes)
	}
	if profile.passes[0].name != "bug-review" {
		t.Fatalf("pass name = %q, want bug-review", profile.passes[0].name)
	}
	if !profile.passes[0].required {
		t.Fatal("bug-review pass is not required")
	}
}

func TestSupportedReviewersResolveToMatchingAdapters(t *testing.T) {
	catalog := defaultReviewerCatalog()
	for _, id := range SupportedReviewers() {
		registration, err := catalog.resolve(id)
		if err != nil {
			t.Fatalf("resolve %q: %v", id, err)
		}
		if registration.candidate.ID != id || registration.executor == nil {
			t.Fatalf("registration for %q = %#v", id, registration)
		}
	}
}

func TestGrokCommandRestrictsCapabilities(t *testing.T) {
	command := grokCommand(reviewerCandidate{Model: "grok-4.5", Effort: "high"}, "/repo", "/tmp/prompt")
	joined := strings.Join(command, " ")
	for _, required := range []string{"--tools view,grep,glob", "--disable-web-search", "--no-subagents", "--permission-mode dontAsk", "--prompt-file /tmp/prompt"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("command %q does not contain %q", joined, required)
		}
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
	command := openCodeCommand(reviewerCandidate{Model: "zai-coding-plan/glm-5.2", Effort: "default"})
	want := []string{"opencode", "run", "--pure", "--agent", "build", "--format", "json", "--model", "zai-coding-plan/glm-5.2"}
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
	assertPermission(t, config.Permission, "*", "deny")
	assertPermission(t, config.Permission, "read", "allow")
	assertPermission(t, config.Permission, "grep", "allow")
	if config.Share != "disabled" {
		t.Fatalf("share = %q", config.Share)
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

func TestOpenCodeAuthenticationFailureIsUnavailable(t *testing.T) {
	execution := classifyHarnessFailure("Token refresh failed: 401", context.Canceled)
	if execution.Outcome != AttemptReviewerUnavailable {
		t.Fatalf("outcome = %q", execution.Outcome)
	}
}

func TestDecodeFailurePreservesHarnessFailureClassification(t *testing.T) {
	run := commandRun{Stdout: []byte("not-json"), Stderr: "authentication failed", WaitErr: errors.New("exit status 1")}
	execution := decodedRunFailure(run, errors.New("decode event"), "opencode")
	if execution.Outcome != AttemptReviewerUnavailable || execution.AssistantText != "not-json" {
		t.Fatalf("execution = %#v", execution)
	}
}

func assertPermission(t *testing.T, permissions map[string]string, name, want string) {
	t.Helper()
	if permissions[name] != want {
		t.Fatalf("permission %q = %q, want %q", name, permissions[name], want)
	}
}
