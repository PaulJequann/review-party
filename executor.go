package reviewparty

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
	AssistantText  string
	Outcome        AttemptOutcome
	Diagnostic     string
	ResolvedModel  string
	ResolvedEffort string
}

type attemptExecutor interface {
	Check(context.Context, reviewerCandidate) availability
	Execute(context.Context, attemptSpec) attemptExecution
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
	if errors.Is(err, context.DeadlineExceeded) {
		outcome = AttemptTransientFailure
	}
	return attemptExecution{Outcome: outcome, Diagnostic: err.Error()}
}

func finalizeHarnessRun(run commandRun, assistantText, diagnostic, model, effort, harness string) attemptExecution {
	if run.StartErr != nil {
		return attemptExecution{Outcome: AttemptReviewerUnavailable, Diagnostic: run.StartErr.Error()}
	}
	if run.ContextErr != nil {
		execution := contextExecution(run.ContextErr)
		execution.AssistantText = assistantText
		return execution
	}
	if run.WaitErr != nil {
		execution := classifyHarnessFailure(diagnostic, run.WaitErr)
		execution.AssistantText = assistantText
		execution.ResolvedModel = model
		execution.ResolvedEffort = effort
		return execution
	}
	if assistantText == "" {
		if strings.TrimSpace(diagnostic) != "" {
			return classifyHarnessFailure(diagnostic, errors.New(harness+" produced no assistant text"))
		}
		return attemptExecution{Outcome: AttemptInvalidResult, Diagnostic: harness + " produced no assistant text"}
	}
	return attemptExecution{
		AssistantText:  assistantText,
		Outcome:        AttemptCompleted,
		Diagnostic:     compactDiagnostic(diagnostic),
		ResolvedModel:  model,
		ResolvedEffort: effort,
	}
}

func overflowExecution(run commandRun, harness string) attemptExecution {
	return attemptExecution{
		AssistantText: string(run.Stdout),
		Outcome:       AttemptInvalidResult,
		Diagnostic:    harness + " output exceeded the capture limit",
	}
}

func decodedRunFailure(run commandRun, decodeErr error, harness string) attemptExecution {
	diagnostic := strings.TrimSpace(strings.Join([]string{run.Stderr, decodeErr.Error()}, " "))
	if run.WaitErr != nil || run.ContextErr != nil {
		return finalizeHarnessRun(run, string(run.Stdout), diagnostic, "", "", harness)
	}
	return attemptExecution{
		AssistantText: string(run.Stdout),
		Outcome:       AttemptInvalidResult,
		Diagnostic:    compactDiagnostic(diagnostic),
	}
}

func classifyHarnessFailure(diagnostic string, waitErr error) attemptExecution {
	normalized := strings.ToLower(diagnostic)
	outcome := AttemptUnknownFailure
	switch {
	case strings.Contains(normalized, "unauthorized"),
		strings.Contains(normalized, "unauthenticated"),
		strings.Contains(normalized, "not logged in"),
		strings.Contains(normalized, "authentication"),
		strings.Contains(normalized, "token refresh failed"),
		strings.Contains(normalized, "model") && strings.Contains(normalized, "not available"):
		outcome = AttemptReviewerUnavailable
	case strings.Contains(normalized, "rate limit"), strings.Contains(normalized, "too many requests"):
		outcome = AttemptTransientFailure
	}
	message := compactDiagnostic(diagnostic)
	if message == "" && waitErr != nil {
		message = waitErr.Error()
	}
	return attemptExecution{Outcome: outcome, Diagnostic: message}
}
