package engine

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

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

func (candidate reviewerCandidate) provenance() ReviewerProvenance {
	return ReviewerProvenance{
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
	Outcome           AttemptOutcome
	Diagnostic        string
	ResolvedModel     string
	ResolvedEffort    string
	FailureCategory   TerminationCategory
	FailurePhase      ExecutionPhase
}

type attemptExecutor interface {
	Check(context.Context, reviewerCandidate) availability
	Execute(context.Context, attemptSpec) attemptExecution
}

type preparedAttempt struct {
	command *exec.Cmd
	cleanup func()
}

type decodedHarnessOutput struct {
	assistantText string
	diagnostic    string
	model         string
	effort        string
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

func (executor directExecutor) Execute(ctx context.Context, spec attemptSpec) attemptExecution {
	prepared, err := executor.adapter.Prepare(spec)
	if err != nil {
		return failedExecution(AttemptUnknownFailure, TerminationUnknownFailure, PhaseHarnessLaunch, err.Error())
	}
	if prepared.command == nil {
		return failedExecution(AttemptUnknownFailure, TerminationUnknownFailure, PhaseHarnessLaunch, executor.adapter.Name()+" prepared no command")
	}
	if prepared.cleanup != nil {
		defer prepared.cleanup()
	}
	return executor.executePrepared(ctx, spec, prepared.command)
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
	diagnostic := strings.TrimSpace(strings.Join([]string{decoded.diagnostic, run.Stderr}, " "))
	model := decoded.model
	if model == "" {
		model = spec.Candidate.Model
	}
	effort := decoded.effort
	if effort == "" {
		effort = spec.Candidate.Effort
	}
	decoded.diagnostic = diagnostic
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
	outcome := AttemptCancelled
	category := TerminationCancelled
	if errors.Is(err, context.DeadlineExceeded) {
		outcome = AttemptTransientFailure
		category = TerminationDeadlineExceeded
	}
	return failedExecution(outcome, category, PhaseReviewerExecution, err.Error())
}

func finalizeHarnessRun(run commandRun, decoded decodedHarnessOutput, harness string) attemptExecution {
	if run.StartErr != nil {
		return failedExecution(AttemptReviewerUnavailable, TerminationReviewerUnavailable, PhaseHarnessLaunch, run.StartErr.Error())
	}
	if run.ContextErr != nil {
		execution := contextExecution(run.ContextErr)
		execution.AssistantText = decoded.assistantText
		return execution
	}
	if run.WaitErr != nil {
		execution := classifyHarnessFailure(decoded.diagnostic, run.WaitErr)
		execution.AssistantText = decoded.assistantText
		execution.ResolvedModel = decoded.model
		execution.ResolvedEffort = decoded.effort
		return execution
	}
	if decoded.assistantText == "" {
		if strings.TrimSpace(decoded.diagnostic) != "" {
			return classifyHarnessFailure(decoded.diagnostic, errors.New(harness+" produced no assistant text"))
		}
		return failedExecution(AttemptInvalidResult, TerminationMalformedOutput, PhaseOutputDecode, harness+" produced no assistant text")
	}
	return attemptExecution{
		AssistantText:  decoded.assistantText,
		Outcome:        AttemptCompleted,
		Diagnostic:     compactDiagnostic(decoded.diagnostic),
		ResolvedModel:  decoded.model,
		ResolvedEffort: decoded.effort,
	}
}

func overflowExecution(run commandRun, harness string) attemptExecution {
	execution := failedExecution(AttemptInvalidResult, TerminationMalformedOutput, PhaseOutputCapture, harness+" output exceeded the capture limit")
	execution.AssistantText = string(run.Stdout)
	execution.ArtifactTruncated = true
	return execution
}

func decodedRunFailure(run commandRun, decodeErr error, harness string) attemptExecution {
	diagnostic := strings.TrimSpace(strings.Join([]string{run.Stderr, decodeErr.Error()}, " "))
	if run.WaitErr != nil || run.ContextErr != nil {
		return finalizeHarnessRun(run, decodedHarnessOutput{assistantText: string(run.Stdout), diagnostic: diagnostic}, harness)
	}
	execution := failedExecution(AttemptInvalidResult, TerminationMalformedOutput, PhaseOutputDecode, compactDiagnostic(diagnostic))
	execution.AssistantText = string(run.Stdout)
	return execution
}

func classifyHarnessFailure(diagnostic string, waitErr error) attemptExecution {
	category := diagnosticFailureCategory(diagnostic)
	outcome := attemptOutcomeForTermination(category)
	message := compactDiagnostic(diagnostic)
	if message == "" && waitErr != nil {
		message = waitErr.Error()
	}
	return failedExecution(outcome, category, PhaseReviewerExecution, message)
}

func diagnosticFailureCategory(diagnostic string) TerminationCategory {
	normalized := strings.ToLower(diagnostic)
	switch {
	case strings.Contains(normalized, "unauthorized"),
		strings.Contains(normalized, "unauthenticated"),
		strings.Contains(normalized, "not logged in"),
		strings.Contains(normalized, "authentication"),
		strings.Contains(normalized, "token refresh failed"):
		return TerminationAuthenticationFailure
	case strings.Contains(normalized, "model") && strings.Contains(normalized, "not available"):
		return TerminationReviewerUnavailable
	case isTransportDiagnostic(normalized):
		return TerminationTransportFailure
	default:
		return TerminationUnknownFailure
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

func attemptOutcomeForTermination(category TerminationCategory) AttemptOutcome {
	switch category {
	case TerminationAuthenticationFailure, TerminationReviewerUnavailable:
		return AttemptReviewerUnavailable
	case TerminationTransportFailure, TerminationDeadlineExceeded:
		return AttemptTransientFailure
	case TerminationCancelled:
		return AttemptCancelled
	case TerminationMalformedOutput, TerminationResultValidationFailure:
		return AttemptInvalidResult
	default:
		return AttemptUnknownFailure
	}
}

func failedExecution(outcome AttemptOutcome, category TerminationCategory, phase ExecutionPhase, diagnostic string) attemptExecution {
	return attemptExecution{
		Outcome:         outcome,
		Diagnostic:      diagnostic,
		FailureCategory: category,
		FailurePhase:    phase,
	}
}
