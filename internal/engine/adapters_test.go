package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"reviewparty/internal/model"
	"reviewparty/internal/result"
	"slices"
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
	if execution.Outcome != model.AttemptReviewerUnavailable {
		t.Fatalf("outcome = %q", execution.Outcome)
	}
}

func TestSupportedReviewersResolveToMatchingAdapters(t *testing.T) {
	catalog := defaultReviewerCatalog()
	want := []string{"claude", "codex", "copilot", "grok", "opencode"}
	wantCandidates := map[string]reviewerCandidate{
		"claude":   {ID: "claude", Model: "claude-opus-5-5", Effort: "high", Harness: "claude-code-cli", Transport: "direct-cli"},
		"codex":    {ID: "codex", Model: "gpt-5.6-luna", Effort: "high", Harness: "codex-cli", Transport: "direct-cli"},
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
	if _, err := result.CanonicalReviewResultContract.Parse(assistantText); err != nil {
		t.Fatalf("parse review result: %v", err)
	}
}

func TestOpenCodeAuthenticationFailureIsUnavailable(t *testing.T) {
	execution := classifyHarnessFailure(decodedHarnessOutput{diagnostic: "Token refresh failed: 401"}, context.Canceled)
	assertAttemptOutcome(t, execution, model.AttemptReviewerUnavailable)
	assertFailureLocation(t, execution, model.TerminationAuthenticationFailure, model.PhaseReviewerExecution)
}

func TestDecodeFailurePreservesHarnessFailureClassification(t *testing.T) {
	run := commandRun{Stdout: []byte("not-json"), Stderr: "authentication failed", WaitErr: errors.New("exit status 1")}
	execution := decodedRunFailure(run, decodedHarnessOutput{}, errors.New("decode event"), "opencode")
	assertAttemptOutcome(t, execution, model.AttemptReviewerUnavailable)
	assertFailureLocation(t, execution, model.TerminationAuthenticationFailure, model.PhaseReviewerExecution)
	if execution.AssistantText != "not-json" {
		t.Fatalf("assistant text = %q", execution.AssistantText)
	}
}

func TestClaudeCommandRestrictsCapabilities(t *testing.T) {
	got := claudeCommand(reviewerCandidate{Model: "claude-opus-5-5", Effort: "high"}, "")
	want := []string{"claude", "-p", "--output-format", "stream-json", "--verbose", "--model", "claude-opus-5-5", "--tools", "Read,Grep,Glob", "--permission-mode", "dontAsk", "--permission-prompts", "none", "--restricted", "--safe-mode", "--strict-mcp-config", "--no-session-persistence", "--effort", "high"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestClaudeCommandOmitsDefaultEffort(t *testing.T) {
	for _, effort := range []string{"", "default"} {
		if command := claudeCommand(reviewerCandidate{Model: "claude-opus-5-5", Effort: effort}, ""); slices.Contains(command, "--effort") {
			t.Fatalf("effort %q command = %#v", effort, command)
		}
	}
}

func TestClaudeRejectsAutoEffort(t *testing.T) {
	check := (claudeAdapter{}).Check(context.Background(), reviewerCandidate{ID: "claude", Model: "claude-opus-5-5", Effort: "auto"})
	if check.Available || !strings.Contains(check.Diagnostic, "does not support explicit effort") {
		t.Fatalf("availability = %#v, want explicit unsupported-effort diagnostic", check)
	}
}

func TestClaudeDecoderJoinsTextBlocksAndResolvesModel(t *testing.T) {
	output := []byte(`{"type":"system","subtype":"init","model":"claude-opus-5-5"}` + "\n" +
		`{"type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"text","text":"Inspecting files."},{"type":"tool_use","input":{"pattern":"**/*.go"}}]}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"tool output"}}` + "\n" +
		`{"type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"ignored"}]}}` + "\n" +
		`{"type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"text","text":"` + strings.ReplaceAll(cleanReview, "\n", "\\n") + `"}]}}` + "\n" +
		`{"type":"result","subtype":"success","is_error":false,"result":"ignored final copy"}` + "\n")
	decoded, err := decodeClaudeOutput(output)
	assertReviewWithPreamble(t, decoded.assistantText, err)
	if decoded.model != "claude-opus-5-5" || decoded.incomplete || decoded.diagnostic != "" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestClaudeInBandErrorIsIncompleteDespiteCleanExit(t *testing.T) {
	output := []byte(`{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"Not logged in · Please run /login"}]}}` + "\n" +
		`{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login"}` + "\n")
	decoded, err := (claudeAdapter{}).Decode(output, "")
	if err != nil {
		t.Fatal(err)
	}
	if decoded.assistantText != "" {
		t.Fatalf("synthetic harness text leaked into assistant text: %q", decoded.assistantText)
	}
	execution := finalizeHarnessRun(commandRun{}, decoded, "claude")
	assertAttemptOutcome(t, execution, model.AttemptReviewerUnavailable)
	assertFailureLocation(t, execution, model.TerminationAuthenticationFailure, model.PhaseReviewerExecution)
}

func TestClaudeMissingResultEventIsIncomplete(t *testing.T) {
	output := []byte(`{"type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"text","text":"` + strings.ReplaceAll(cleanReview, "\n", "\\n") + `"}]}}` + "\n")
	decoded, err := (claudeAdapter{}).Decode(output, "")
	if err != nil {
		t.Fatal(err)
	}
	execution := finalizeHarnessRun(commandRun{}, decoded, "claude")
	if execution.Outcome == model.AttemptCompleted {
		t.Fatalf("run without a result event completed: %#v", execution)
	}
	if execution.AssistantText != cleanReview || !strings.Contains(execution.Diagnostic, "without a result event") {
		t.Fatalf("execution = %#v", execution)
	}
}

func TestClaudeRepositoryInstructionsLoadClaudeThenAgents(t *testing.T) {
	repository := t.TempDir()
	writeInstruction(t, repository, "AGENTS.md", "agents rule")
	writeInstruction(t, repository, "CLAUDE.md", "claude rule")
	instructions, err := repositoryInstructions(repository)
	if err != nil {
		t.Fatal(err)
	}
	claude, agents := strings.Index(instructions, "claude rule"), strings.Index(instructions, "agents rule")
	if claude < 0 || agents < claude || !strings.Contains(instructions, "Contents of AGENTS.md") {
		t.Fatalf("instructions = %q", instructions)
	}
}

func TestClaudeRepositoryInstructionsLoadLinkedFileOnce(t *testing.T) {
	repository := t.TempDir()
	writeInstruction(t, repository, "AGENTS.md", "shared rule")
	if err := os.Symlink("AGENTS.md", filepath.Join(repository, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	instructions, err := repositoryInstructions(repository)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(instructions, "shared rule") != 1 {
		t.Fatalf("instructions = %q", instructions)
	}
}

func TestClaudeRepositoryInstructionsRejectEscapingSymlink(t *testing.T) {
	repository, outside := t.TempDir(), t.TempDir()
	writeInstruction(t, outside, "secret", "outside content")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(repository, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	instructions, err := repositoryInstructions(repository)
	if err == nil || strings.Contains(instructions, "outside content") {
		t.Fatalf("instructions = %q, err = %v", instructions, err)
	}
}

func TestClaudeRepositoryInstructionsHonorBudget(t *testing.T) {
	repository := t.TempDir()
	writeInstruction(t, repository, "CLAUDE.md", strings.Repeat("c", claudeInstructionBudget+10))
	writeInstruction(t, repository, "AGENTS.md", "agents rule")
	instructions, err := repositoryInstructions(repository)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(instructions, "c") < claudeInstructionBudget || strings.Contains(instructions, "agents rule") || len(instructions) > claudeInstructionBudget+200 {
		t.Fatalf("instructions length = %d", len(instructions))
	}
}

func TestClaudePrepareAppendsRepositoryInstructions(t *testing.T) {
	repository := t.TempDir()
	prepared, err := (claudeAdapter{}).Prepare(attemptSpec{Repository: repository, Candidate: reviewerCandidate{ID: "claude", Model: "claude-opus-5-5"}})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(prepared.command.Args, "--append-system-prompt") {
		t.Fatalf("empty repository appended instructions: %#v", prepared.command.Args)
	}
	writeInstruction(t, repository, "AGENTS.md", "agents rule")
	prepared, err = (claudeAdapter{}).Prepare(attemptSpec{Repository: repository, Candidate: reviewerCandidate{ID: "claude", Model: "claude-opus-5-5"}})
	if err != nil {
		t.Fatal(err)
	}
	args := prepared.command.Args
	index := slices.Index(args, "--append-system-prompt")
	if index < 0 || index+1 >= len(args) || !strings.Contains(args[index+1], "agents rule") {
		t.Fatalf("args = %#v", args)
	}
}

func writeInstruction(t *testing.T, directory, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
