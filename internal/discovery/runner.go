package discovery

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const maxCaptureBytes = 4 * 1024 * 1024
const processCleanupGrace = time.Second

var diagnosticSecretPattern = regexp.MustCompile(`(?i)\b((?:[a-z0-9]+_)*(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|authorization|bearer|token|secret|password|account(?:[_ -]?(?:id|email))?|user(?:[_ -]?(?:id|email))?|email))\b["']?\s*[:=]\s*(?:bearer\s+)?(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
var diagnosticTokenPattern = regexp.MustCompile(`(?i)\b(?:sk-[A-Za-z0-9_-]{8,}|xai-[A-Za-z0-9_-]{8,}|gh[pousr]_[A-Za-z0-9_-]{8,})\b`)

// Command is an argv-based external command. No shell is involved.
type Command struct {
	Args        []string
	Environment []string
}

// RunResult is bounded process output and lifecycle state.
type RunResult struct {
	Stdout         []byte
	Stderr         []byte
	Err            error
	TimedOut       bool
	Canceled       bool
	OutputOverflow bool
}

// Runner is injectable so adapter parsing tests never need a real harness.
type Runner interface {
	Run(context.Context, Command) RunResult
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, command Command) RunResult {
	if err := validateCommand(command); err != nil {
		return RunResult{Err: err}
	}
	if !processTreeCleanupAvailable() {
		return RunResult{Err: errors.New("discovery process-tree cleanup is unavailable on this platform")}
	}
	commandContext, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &combinedBoundedOutput{boundedCapture: boundedCapture{limit: maxCaptureBytes, cancel: cancel}}
	process, finished, err := startDiscoveryProcess(command, output)
	if err != nil {
		return processStartResult(err, ctx, output)
	}
	runErr := waitForDiscoveryProcess(commandContext, process, finished)
	return discoveryRunResult(output, ctx, runErr)
}

func validateCommand(command Command) error {
	if len(command.Args) == 0 || strings.TrimSpace(command.Args[0]) == "" {
		return errors.New("discovery command is empty")
	}
	if command.Environment == nil {
		return errors.New("discovery command environment is required")
	}
	return nil
}

func startDiscoveryProcess(command Command, output *combinedBoundedOutput) (*exec.Cmd, chan error, error) {
	process := exec.Command(command.Args[0], command.Args[1:]...)
	configureProcessGroup(process)
	process.Env = append([]string(nil), command.Environment...)
	process.Stdout = &boundedStream{output: output, target: &output.stdout}
	process.Stderr = &boundedStream{output: output, target: &output.stderr}
	if err := process.Start(); err != nil {
		return nil, nil, err
	}
	finished := make(chan error, 1)
	go func() { finished <- process.Wait() }()
	return process, finished, nil
}

func processStartResult(err error, ctx context.Context, output *combinedBoundedOutput) RunResult {
	return RunResult{
		Err: err, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		Canceled: errors.Is(ctx.Err(), context.Canceled), OutputOverflow: output.overflow.Load(),
	}
}

func waitForDiscoveryProcess(ctx context.Context, process *exec.Cmd, finished <-chan error) error {
	select {
	case runErr := <-finished:
		if ctx.Err() != nil {
			killProcessGroup(process)
		}
		return runErr
	case <-ctx.Done():
		return stopProcess(process, finished)
	}
}

func discoveryRunResult(output *combinedBoundedOutput, ctx context.Context, runErr error) RunResult {
	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}
	if runErr != nil {
		return RunResult{
			Stdout: output.stdout.Bytes(), Stderr: output.stderr.Bytes(), Err: runErr,
			TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
			Canceled: errors.Is(ctx.Err(), context.Canceled), OutputOverflow: output.overflow.Load(),
		}
	}
	return RunResult{Stdout: output.stdout.Bytes(), Stderr: output.stderr.Bytes(), OutputOverflow: output.overflow.Load()}
}

func stopProcess(process *exec.Cmd, finished <-chan error) error {
	terminateProcessGroup(process)
	err, stopped := waitForProcess(finished, processCleanupGrace)
	if stopped {
		// The leader may have exited while descendants still hold its pipes.
		// Reap the leader first, then clear the process group as well.
		killProcessGroup(process)
		return err
	}
	killProcessGroup(process)
	if err, stopped := waitForProcess(finished, processCleanupGrace); stopped {
		return err
	}
	return errors.New("discovery process did not terminate after forced cleanup")
}

func waitForProcess(finished <-chan error, timeout time.Duration) (error, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-finished:
		return err, true
	case <-timer.C:
		return nil, false
	}
}

type combinedBoundedOutput struct {
	boundedCapture
	stdout boundedBuffer
	stderr boundedBuffer
}

type boundedStream struct {
	output *combinedBoundedOutput
	target *boundedBuffer
}

func (stream *boundedStream) Write(value []byte) (int, error) {
	allowed := stream.output.reserve(len(value))
	if allowed > 0 {
		stream.target.Write(value[:allowed])
	}
	return len(value), nil
}

type boundedCapture struct {
	limit    int
	used     atomic.Int64
	overflow atomic.Bool
	cancel   context.CancelFunc
}

func (capture *boundedCapture) reserve(length int) int {
	used := int(capture.used.Add(int64(length)))
	if used <= capture.limit {
		return length
	}
	previous := used - length
	remaining := capture.limit - previous
	if remaining < 0 {
		remaining = 0
	}
	capture.overflow.Store(true)
	if capture.cancel != nil {
		capture.cancel()
	}
	return remaining
}

type boundedBuffer struct {
	data  []byte
	limit int
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	length := len(value)
	if buffer.limit > 0 {
		remaining := buffer.limit - len(buffer.data)
		if remaining <= 0 {
			return length, nil
		}
		if len(value) > remaining {
			value = value[:remaining]
		}
	}
	buffer.data = append(buffer.data, value...)
	return length, nil
}

func (buffer boundedBuffer) Bytes() []byte  { return append([]byte(nil), buffer.data...) }
func (buffer boundedBuffer) String() string { return string(buffer.data) }

// NewDefaultRunner returns the production argv runner.
func NewDefaultRunner() Runner { return execRunner{} }

func commandFailed(run RunResult) bool {
	return run.Err != nil || run.TimedOut || run.Canceled || run.OutputOverflow
}

func commandDiagnostic(run RunResult) string {
	diagnostic := strings.TrimSpace(strings.Join([]string{errorString(run.Err), string(run.Stderr)}, " "))
	if run.TimedOut {
		diagnostic = strings.TrimSpace(strings.Join([]string{diagnostic, "discovery deadline exceeded"}, " "))
	}
	if run.OutputOverflow {
		diagnostic = strings.TrimSpace(strings.Join([]string{diagnostic, "discovery output exceeded the capture limit"}, " "))
	}
	return compactDiagnostic(diagnostic)
}

func rawCommandDiagnostic(run RunResult) string {
	return strings.TrimSpace(strings.Join([]string{string(run.Stderr), errorString(run.Err)}, " "))
}

func authenticationDiagnostic(run RunResult) string {
	output := strings.Join([]string{string(run.Stdout), rawCommandDiagnostic(run)}, " ")
	if isAuthenticationDiagnostic([]byte(output)) {
		return "discovery command reported that authentication is required"
	}
	return ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func compactDiagnostic(value string) string {
	value = diagnosticSecretPattern.ReplaceAllString(value, "$1=[REDACTED]")
	value = diagnosticTokenPattern.ReplaceAllString(value, "[REDACTED]")
	fields := strings.Fields(value)
	if len(fields) > 40 {
		fields = fields[len(fields)-40:]
	}
	return strings.Join(fields, " ")
}

func firstStatusFailure(run RunResult, signIn *SignInAction) Observation {
	diagnostic := commandDiagnostic(run)
	return Observation{Status: StatusUnavailable, Authentication: Authentication{Status: AuthUnavailable, SignIn: signIn}, Diagnostic: diagnostic}
}
