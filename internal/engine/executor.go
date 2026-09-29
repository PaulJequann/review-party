package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"reviewparty/internal/model"
	"strings"
	"time"
)

func reviewerEnvironment(reviewer string, additions ...string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "TERM": true, "NO_COLOR": true, "XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true}
	for _, name := range map[string][]string{
		"grok":     {"GROK_API_KEY", "XAI_API_KEY"},
		"opencode": {"OPENCODE_CONFIG", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_GENERATIVE_AI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY", "MISTRAL_API_KEY", "DEEPSEEK_API_KEY"},
		"copilot":  {"GH_TOKEN", "GITHUB_TOKEN"},
		"codex":    {"OPENAI_API_KEY", "CODEX_API_KEY"},
		"claude":   {"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CONFIG_DIR"},
	}[reviewer] {
		allowed[name] = true
	}
	result := make([]string, 0, len(allowed)+len(additions))
	for _, value := range os.Environ() {
		name, _, found := strings.Cut(value, "=")
		if found && allowed[name] {
			result = append(result, value)
		}
	}
	return append(result, additions...)
}

const (
	maxHarnessStdout = 4 * 1024 * 1024
	maxHarnessStderr = 64 * 1024
	processKillGrace = 5 * time.Second
)

type reviewerCandidate struct {
	ID        string
	Model     string
	Effort    string
	Harness   string
	Transport string
}

func (candidate reviewerCandidate) provenance() model.ReviewerProvenance {
	return model.ReviewerProvenance{
		ReviewerID: candidate.ID,
		Model:      candidate.Model,
		Effort:     candidate.Effort,
		Harness:    candidate.Harness,
		Transport:  candidate.Transport,
	}
}

type availability struct {
	Available  bool
	Diagnostic string
}

type attemptSpec struct {
	Repository string
	Prompt     string
	Candidate  reviewerCandidate
}

type attemptExecution struct {
	AssistantText     string
	ArtifactTruncated bool
	Outcome           model.AttemptOutcome
	Diagnostic        string
	ReviewerNoise     string
	ResolvedModel     string
	ResolvedEffort    string
	FailureCategory   model.TerminationCategory
	FailurePhase      model.ExecutionPhase
	RetryAfter        time.Duration
}

type attemptExecutor interface {
	Check(context.Context, reviewerCandidate) availability
	Execute(context.Context, attemptSpec) attemptExecution
}

type preparedAttempt struct {
	command *exec.Cmd
	cleanup func() error
}

type decodedHarnessOutput struct {
	assistantText string
	diagnostic    string
	noise         string
	model         string
	effort        string
	// incomplete marks a run the harness itself reported as unfinished, so a
	// clean process exit cannot turn it into a completed attempt.
	incomplete bool
}

type harnessAdapter interface {
	Name() string
	Check(context.Context, reviewerCandidate) availability
	Prepare(attemptSpec) (preparedAttempt, error)
	Decode([]byte) (decodedHarnessOutput, error)
}

type directExecutor struct {
	adapter harnessAdapter
	run     func(context.Context, *exec.Cmd) commandRun
}

func newDirectExecutor(adapter harnessAdapter) directExecutor {
	return directExecutor{adapter: adapter, run: runCommand}
}

func (executor directExecutor) Check(ctx context.Context, candidate reviewerCandidate) availability {
	return executor.adapter.Check(ctx, candidate)
}

func (executor directExecutor) Execute(ctx context.Context, spec attemptSpec) (execution attemptExecution) {
	prepared, err := executor.adapter.Prepare(spec)
	if err != nil {
		return failedExecution(model.AttemptUnknownFailure, model.TerminationUnknownFailure, model.PhaseHarnessLaunch, err.Error())
	}
	if prepared.command == nil {
		return failedExecution(model.AttemptUnknownFailure, model.TerminationUnknownFailure, model.PhaseHarnessLaunch, executor.adapter.Name()+" prepared no command")
	}
	if prepared.cleanup != nil {
		defer func() {
			execution = applyCleanupError(execution, prepared.cleanup())
		}()
	}
	return executor.executePrepared(ctx, spec, prepared.command)
}

func applyCleanupError(execution attemptExecution, cleanupErr error) attemptExecution {
	if cleanupErr == nil {
		return execution
	}
	execution.Diagnostic = strings.TrimSpace(strings.Join([]string{execution.Diagnostic, "cleanup failed: " + cleanupErr.Error()}, " "))
	if execution.Outcome == "" || execution.Outcome == model.AttemptCompleted {
		execution.Outcome = model.AttemptUnknownFailure
		execution.FailureCategory = model.TerminationUnknownFailure
		execution.FailurePhase = model.PhaseReviewerExecution
	}
	return execution
}

func (executor directExecutor) executePrepared(ctx context.Context, spec attemptSpec, command *exec.Cmd) attemptExecution {
	run := executor.run(ctx, command)
	if run.StartErr != nil {
		return finalizeHarnessRun(run, decodedHarnessOutput{diagnostic: run.Stderr}, executor.adapter.Name())
	}
	if run.OutputOverflow {
		return overflowExecution(run, executor.adapter.Name())
	}
	decoded, decodeErr := executor.adapter.Decode(run.Stdout)
	if decodeErr != nil {
		return decodedRunFailure(run, decodeErr, executor.adapter.Name())
	}
	model := decoded.model
	if model == "" {
		model = spec.Candidate.Model
	}
	effort := decoded.effort
	if effort == "" {
		effort = spec.Candidate.Effort
	}
	decoded.noise = joinReport(decoded.noise, run.Stderr)
	decoded.model = model
	decoded.effort = effort
	return finalizeHarnessRun(run, decoded, executor.adapter.Name())
}

type commandRun struct {
	Stdout         []byte
	Stderr         string
	WaitErr        error
	StartErr       error
	ContextErr     error
	OutputOverflow bool
}

func runCommand(ctx context.Context, command *exec.Cmd) commandRun {
	stdout := newBoundedBuffer(maxHarnessStdout)
	stderr := newBoundedBuffer(maxHarnessStderr)
	command.Stdout = &stdout
	command.Stderr = &stderr
	configureProcessGroup(command)
	if err := command.Start(); err != nil {
		return commandRun{StartErr: err}
	}

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case waitErr := <-waited:
		return commandResult(stdout, stderr, waitErr, nil)
	case <-ctx.Done():
		terminateProcessGroup(command)
		grace := time.NewTimer(5 * time.Second)
		select {
		case waitErr := <-waited:
			stopTimer(grace)
			return commandResult(stdout, stderr, waitErr, ctx.Err())
		case <-grace.C:
			killProcessGroup(command)
			finalWait := time.NewTimer(processKillGrace)
			select {
			case waitErr := <-waited:
				stopTimer(finalWait)
				return commandResult(stdout, stderr, waitErr, ctx.Err())
			case <-finalWait.C:
				return commandRun{WaitErr: errors.New("process did not exit after forced termination"), ContextErr: ctx.Err()}
			}
		}
	}
}

func commandResult(stdout, stderr boundedBuffer, waitErr, contextErr error) commandRun {
	return commandRun{
		Stdout:         stdout.Bytes(),
		Stderr:         stderr.String(),
		WaitErr:        waitErr,
		ContextErr:     contextErr,
		OutputOverflow: stdout.overflow || stderr.overflow,
	}
}

type boundedBuffer struct {
	data     []byte
	limit    int
	overflow bool
}

func newBoundedBuffer(limit int) boundedBuffer {
	return boundedBuffer{data: make([]byte, 0, limit), limit: limit}
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := buffer.limit - len(buffer.data)
	if remaining <= 0 {
		buffer.overflow = true
		return written, nil
	}
	if len(value) > remaining {
		buffer.data = append(buffer.data, value[:remaining]...)
		buffer.overflow = true
		return written, nil
	}
	buffer.data = append(buffer.data, value...)
	return written, nil
}

func (buffer boundedBuffer) Bytes() []byte {
	return append([]byte(nil), buffer.data...)
}

func (buffer boundedBuffer) String() string {
	return string(buffer.data)
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func contextExecution(err error) attemptExecution {
	outcome := model.AttemptCancelled
	category := model.TerminationCancelled
	if errors.Is(err, context.DeadlineExceeded) {
		outcome = model.AttemptTransientFailure
		category = model.TerminationDeadlineExceeded
	}
	return failedExecution(outcome, category, model.PhaseReviewerExecution, err.Error())
}

func finalizeHarnessRun(run commandRun, decoded decodedHarnessOutput, harness string) attemptExecution {
	execution := classifyHarnessRun(run, decoded, harness)
	execution.ReviewerNoise = reviewerNoise(execution, decoded)
	return execution
}

func classifyHarnessRun(run commandRun, decoded decodedHarnessOutput, harness string) attemptExecution {
	if run.StartErr != nil {
		return failedExecution(model.AttemptReviewerUnavailable, model.TerminationReviewerUnavailable, model.PhaseHarnessLaunch, run.StartErr.Error())
	}
	if run.ContextErr != nil {
		execution := contextExecution(run.ContextErr)
		execution.AssistantText = decoded.assistantText
		return execution
	}
	waitErr := run.WaitErr
	if waitErr == nil && decoded.incomplete {
		waitErr = errors.New(harness + " reported an incomplete run")
	}
	if waitErr != nil {
		execution := classifyHarnessFailure(decoded, waitErr)
		execution.AssistantText = decoded.assistantText
		execution.ResolvedModel = decoded.model
		execution.ResolvedEffort = decoded.effort
		return execution
	}
	if decoded.assistantText == "" {
		if strings.TrimSpace(joinReport(decoded.diagnostic, decoded.noise)) != "" {
			return classifyHarnessFailure(decoded, errors.New(harness+" produced no assistant text"))
		}
		return failedExecution(model.AttemptInvalidResult, model.TerminationMalformedOutput, model.PhaseOutputDecode, harness+" produced no assistant text")
	}
	return attemptExecution{
		AssistantText:  decoded.assistantText,
		Outcome:        model.AttemptCompleted,
		ResolvedModel:  decoded.model,
		ResolvedEffort: decoded.effort,
	}
}

func reviewerNoise(execution attemptExecution, decoded decodedHarnessOutput) string {
	if execution.Outcome == model.AttemptCompleted {
		return joinReport(decoded.diagnostic, decoded.noise)
	}
	return decoded.noise
}

func joinReport(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	return strings.Join(nonEmpty, "\n")
}

func overflowExecution(run commandRun, harness string) attemptExecution {
	execution := failedExecution(model.AttemptInvalidResult, model.TerminationMalformedOutput, model.PhaseOutputCapture, harness+" output exceeded the capture limit")
	execution.AssistantText = string(run.Stdout)
	execution.ArtifactTruncated = true
	return execution
}

func decodedRunFailure(run commandRun, decodeErr error, harness string) attemptExecution {
	decoded := decodedHarnessOutput{assistantText: string(run.Stdout), diagnostic: decodeErr.Error(), noise: run.Stderr}
	if run.WaitErr != nil || run.ContextErr != nil {
		return finalizeHarnessRun(run, decoded, harness)
	}
	execution := failedExecution(model.AttemptInvalidResult, model.TerminationMalformedOutput, model.PhaseOutputDecode, compactDiagnostic(decoded.diagnostic))
	execution.AssistantText = decoded.assistantText
	execution.ReviewerNoise = decoded.noise
	return execution
}

func classifyHarnessFailure(decoded decodedHarnessOutput, waitErr error) attemptExecution {
	category := diagnosticFailureCategory(joinReport(decoded.diagnostic, decoded.noise))
	outcome := attemptOutcomeForTermination(category)
	message := decisiveDiagnostic(decoded, category)
	if message == "" && waitErr != nil {
		message = waitErr.Error()
	}
	return failedExecution(outcome, category, model.PhaseReviewerExecution, message)
}

func decisiveDiagnostic(decoded decodedHarnessOutput, category model.TerminationCategory) string {
	if category != model.TerminationUnknownFailure {
		for line := range strings.Lines(joinReport(decoded.diagnostic, decoded.noise)) {
			if diagnosticFailureCategory(line) == category {
				return compactDiagnostic(line)
			}
		}
	}
	if decoded.diagnostic != "" {
		return compactDiagnostic(decoded.diagnostic)
	}
	return compactDiagnostic(decoded.noise)
}

func diagnosticFailureCategory(diagnostic string) model.TerminationCategory {
	normalized := strings.ToLower(diagnostic)
	switch {
	case strings.Contains(normalized, "unauthorized"),
		strings.Contains(normalized, "unauthenticated"),
		strings.Contains(normalized, "not logged in"),
		strings.Contains(normalized, "authentication"),
		strings.Contains(normalized, "token refresh failed"):
		return model.TerminationAuthenticationFailure
	case strings.Contains(normalized, "model") && strings.Contains(normalized, "not available"):
		return model.TerminationReviewerUnavailable
	case isTransportDiagnostic(normalized):
		return model.TerminationTransportFailure
	default:
		return model.TerminationUnknownFailure
	}
}

func isTransportDiagnostic(normalized string) bool {
	fragments := []string{
		"rate limit", "too many requests",
		"connection refused", "connection reset", "connection closed",
		"network unreachable", "network is unreachable", "no route to host",
		"temporary failure in name resolution", "dial tcp", "tls handshake timeout",
		"i/o timeout", "request timeout", "request timed out", "upstream timeout",
		"internal server error", "bad gateway", "service unavailable", "gateway timeout",
	}
	for _, fragment := range fragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func attemptOutcomeForTermination(category model.TerminationCategory) model.AttemptOutcome {
	switch category {
	case model.TerminationAuthenticationFailure, model.TerminationReviewerUnavailable:
		return model.AttemptReviewerUnavailable
	case model.TerminationTransportFailure, model.TerminationDeadlineExceeded:
		return model.AttemptTransientFailure
	case model.TerminationCancelled:
		return model.AttemptCancelled
	case model.TerminationMalformedOutput, model.TerminationResultValidationFailure:
		return model.AttemptInvalidResult
	case model.TerminationUnknownFailure:
		return model.AttemptUnknownFailure
	default:
		return model.AttemptUnknownFailure
	}
}

func failedExecution(outcome model.AttemptOutcome, category model.TerminationCategory, phase model.ExecutionPhase, diagnostic string) attemptExecution {
	return attemptExecution{
		Outcome:         outcome,
		Diagnostic:      diagnostic,
		FailureCategory: category,
		FailurePhase:    phase,
	}
}
