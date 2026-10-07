package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"reviewparty/internal/hostrun"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxCaptureBytes = 4 * 1024 * 1024
const processCleanupGrace = time.Second

var diagnosticSecretPattern = regexp.MustCompile(`(?i)\b((?:[a-z0-9]+_)*(?:api[_ -]?key|access[_ -]?(?:key|token)(?:[_ -]?(?:id|secret))?|refresh[_ -]?token|authorization|bearer|token|secret|password|account(?:[_ -]?(?:id|email))?|user(?:[_ -]?(?:id|email))?|email))\b["']?\s*[:=]\s*(?:bearer\s+)?(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
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

type processSession struct {
	process   *hostrun.Process
	resources []io.Closer
	closeOnce sync.Once
}

// startProcessSession starts process under the invocation's run, which owns
// its process tree. resources are the owner's ends of the process's pipes;
// they are closed on a failed start and when the session closes.
func startProcessSession(ctx context.Context, process *exec.Cmd, resources ...io.Closer) (*processSession, error) {
	run, err := hostrun.From(ctx)
	if err != nil {
		closeProcessResources(resources)
		return nil, err
	}
	started, err := run.Start(process)
	if err != nil {
		closeProcessResources(resources)
		return nil, err
	}
	return &processSession{process: started, resources: resources}, nil
}

func closeProcessResources(resources []io.Closer) {
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		if err := resource.Close(); err != nil {
			continue
		}
	}
}

// Stop ends the process tree, terminate then kill with processCleanupGrace
// each, and reports how the process exited.
func (session *processSession) Stop() error {
	if err := session.process.Stop(processCleanupGrace); err != nil {
		return err
	}
	return session.process.Wait()
}

func (session *processSession) Close() {
	session.closeOnce.Do(func() {
		closeProcessResources(session.resources)
		if err := session.Stop(); err != nil {
			return
		}
	})
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, command Command) RunResult {
	if err := validateCommand(command); err != nil {
		return RunResult{Err: err}
	}
	commandContext, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &combinedBoundedOutput{boundedCapture: boundedCapture{limit: maxCaptureBytes, cancel: cancel}}
	session, err := startDiscoveryProcess(commandContext, command, output)
	if err != nil {
		return processStartResult(err, ctx, output)
	}
	runErr := waitForDiscoveryProcess(commandContext, session)
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

func startDiscoveryProcess(ctx context.Context, command Command, output *combinedBoundedOutput) (*processSession, error) {
	executable, err := trustedExecutable(command.Args[0])
	if err != nil {
		return nil, err
	}
	process := exec.Command(executable, command.Args[1:]...)
	process.Env = append([]string(nil), command.Environment...)
	process.Stdout = &boundedStream{output: output, target: &output.stdout}
	process.Stderr = &boundedStream{output: output, target: &output.stderr}
	return startProcessSession(ctx, process)
}

func trustedExecutable(name string) (string, error) {
	if filepath.IsAbs(name) {
		return validateExecutablePath(name)
	}
	for _, directory := range trustedExecutableRoots() {
		for _, path := range executableCandidates(directory, name) {
			candidate, err := validateExecutablePath(path)
			if err == nil {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("discovery executable %q was not found in trusted executable roots", name)
}

func validateExecutablePath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || !isExecutableFile(resolved, info) {
		return "", fmt.Errorf("%q is not an executable file", path)
	}
	workingDirectory, err := os.Getwd()
	if err == nil && pathWithinDirectory(workingDirectory, resolved) {
		return "", fmt.Errorf("repository-local discovery executable %q is not trusted", path)
	}
	return resolved, nil
}

func pathWithinDirectory(directory, path string) bool {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func processStartResult(err error, ctx context.Context, output *combinedBoundedOutput) RunResult {
	return RunResult{
		Err: err, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		Canceled: errors.Is(ctx.Err(), context.Canceled), OutputOverflow: output.overflow.Load(),
	}
}

func waitForDiscoveryProcess(ctx context.Context, session *processSession) error {
	select {
	case <-session.process.Done():
		return session.process.Wait()
	case <-ctx.Done():
		return session.Stop()
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
		if _, err := stream.target.Write(value[:allowed]); err != nil {
			return 0, err
		}
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
