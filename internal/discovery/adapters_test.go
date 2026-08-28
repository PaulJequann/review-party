package discovery

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

type scriptedRunner struct {
	runs map[string]RunResult
	args [][]string
}

func (runner *scriptedRunner) Run(_ context.Context, command Command) RunResult {
	runner.args = append(runner.args, append([]string(nil), command.Args...))
	return runner.runs[strings.Join(command.Args[1:], " ")]
}

func TestGrokParserHandlesHumanOutputAndDefault(t *testing.T) {
	got := parseGrokModels([]byte("Found 2 models:\nAvailable models:\n  grok-4.6 (default)\n  grok-4.5\n"))
	want := []Model{{ID: "grok-4.6", Default: true}, {ID: "grok-4.5"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %#v, want %#v", got, want)
	}
}

func TestGrokParserRejectsAuthenticationAndProse(t *testing.T) {
	got := parseGrokModels([]byte("authentication required\nNo models found\ngrok-4.6 is the default model\n"))
	if len(got) != 0 {
		t.Fatalf("prose models = %#v", got)
	}
}

func TestHarnessVersionUsesDedicatedVersionCommand(t *testing.T) {
	runner := &scriptedRunner{runs: map[string]RunResult{
		"models":    {Stdout: []byte("v9.9\n")},
		"--version": {Stdout: []byte("grok version 1.2.3\n")},
	}}
	result := NewService(Options{Adapters: []Adapter{NewGrokAdapter(runner)}}).Discover(context.Background(), "grok")
	if result.HarnessVersion != "1.2.3" {
		t.Fatalf("harness version = %q", result.HarnessVersion)
	}
}

func TestHumanDiscoveryKeepsModelsWhenOptionalVersionTimesOut(t *testing.T) {
	result := NewService(Options{
		Adapters: []Adapter{NewGrokAdapter(versionTimeoutRunner{})},
		Deadline: 10 * time.Millisecond,
	}).Discover(context.Background(), "grok")
	if result.Status != StatusSupported {
		t.Fatalf("result status = %s, want supported", result.Status)
	}
	if len(result.Models) != 1 {
		t.Fatalf("result models = %#v, want one model", result.Models)
	}
	if result.Models[0].ID != "grok-4.6" {
		t.Fatalf("result = %#v, want supported model result", result)
	}
	if result.HarnessVersion != "unknown" {
		t.Fatalf("harness version = %q, want unknown", result.HarnessVersion)
	}
}

type versionTimeoutRunner struct{}

func (versionTimeoutRunner) Run(ctx context.Context, command Command) RunResult {
	if len(command.Args) > 1 && command.Args[1] == "models" {
		return RunResult{Stdout: []byte("grok-4.6\n")}
	}
	<-ctx.Done()
	return RunResult{Err: ctx.Err(), TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded), Canceled: errors.Is(ctx.Err(), context.Canceled)}
}

func TestHumanModelIDAcceptsBareAliases(t *testing.T) {
	if !validHumanModelID([]byte("sonnet")) {
		t.Fatal("bare model alias was rejected")
	}
	if validHumanModelID([]byte("available")) {
		t.Fatal("reserved output heading was accepted as a model")
	}
}

func TestGrokAuthenticationFailureIsExplicit(t *testing.T) {
	runner := &scriptedRunner{runs: map[string]RunResult{
		"models": {Err: errors.New("exit status 1"), Stderr: []byte("authentication required")},
	}}
	result := NewService(Options{Adapters: []Adapter{NewGrokAdapter(runner)}}).Discover(context.Background(), "grok")
	requireDiscoveryStatus(t, result, StatusAuthenticationRequired, AuthRequired)
	if result.Authentication.SignIn == nil {
		t.Fatalf("result omitted sign-in action: %#v", result)
	}
	if !reflect.DeepEqual(runner.args[0], []string{"grok", "models"}) {
		t.Fatalf("command = %#v", runner.args)
	}
}

func TestGrokAuthenticationFailureOnStdoutIsExplicit(t *testing.T) {
	runner := &scriptedRunner{runs: map[string]RunResult{
		"models": {Err: errors.New("exit status 1"), Stdout: []byte("authentication required")},
	}}
	result := NewService(Options{Adapters: []Adapter{NewGrokAdapter(runner)}}).Discover(context.Background(), "grok")
	requireDiscoveryStatus(t, result, StatusAuthenticationRequired, AuthRequired)
	if result.Authentication.SignIn == nil {
		t.Fatalf("result omitted sign-in action: %#v", result)
	}
}

func TestOpenCodeParserRequiresCanonicalProviderModelID(t *testing.T) {
	got := parseOpenCodeModels([]byte("openai/gpt-5\ninvalid\nopenrouter/deepseek-v4\n"))
	want := []Model{{ID: "openai/gpt-5"}, {ID: "openrouter/deepseek-v4"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %#v, want %#v", got, want)
	}
}

func TestOpenCodeDiscoverySeparatesProviderAuthentication(t *testing.T) {
	runner := &scriptedRunner{runs: map[string]RunResult{
		"models":         {Stdout: []byte("openai/gpt-5\n")},
		"providers list": {Stdout: []byte("No credentials configured; run login\n")},
	}}
	result := NewService(Options{Adapters: []Adapter{NewOpenCodeAdapter(runner)}}).Discover(context.Background(), "opencode")
	requireDiscoveryStatus(t, result, StatusSupported, AuthRequired)
	if !reflect.DeepEqual(runner.args, [][]string{{"opencode", "models"}, {"opencode", "providers", "list"}, {"opencode", "--version"}}) {
		t.Fatalf("commands = %#v", runner.args)
	}
}

func TestOpenCodeMixedProviderAuthDoesNotHideUsableModels(t *testing.T) {
	result := discoverOpenCodeWithProviders(t, "openai: authenticated\nother: authentication: required\n")
	requireDiscoveryStatus(t, result, StatusSupported, AuthConfigured)
}

func TestOpenCodeGlobalAuthenticationFailureIsExplicit(t *testing.T) {
	result := discoverOpenCodeWithProviders(t, "authentication required\n")
	requireDiscoveryStatus(t, result, StatusSupported, AuthRequired)
}

func TestOpenCodePositiveAuthenticationTextIsConfigured(t *testing.T) {
	result := discoverOpenCodeWithProviders(t, "openai: logged in\nopenai: API key configured\n")
	requireDiscoveryStatus(t, result, StatusSupported, AuthConfigured)
}

func TestCodexAuthenticationPayloadDoesNotExposeAccount(t *testing.T) {
	signIn := &SignInAction{Command: []string{"codex", "login"}}
	authentication, err := codexAuthentication([]byte(`{"account":null,"requiresOpenaiAuth":true}`), signIn)
	if err != nil {
		t.Fatal(err)
	}
	if authentication.Status != AuthRequired || authentication.Diagnostic != "Codex requires authentication" {
		t.Fatalf("authentication = %#v", authentication)
	}
}

func TestCodexAdapterUsesInjectableSession(t *testing.T) {
	stdout := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex 1.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"account":{"email":"redacted"},"requiresOpenaiAuth":false}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"data":[{"id":"gpt-5.6-luna","isDefault":true}]}}`,
	}, "\n"))
	session := &testCodexSession{stdin: &testWriteCloser{Writer: &bytes.Buffer{}}, stdout: io.NopCloser(stdout)}
	adapter := codexAdapter{start: func(context.Context, *codexCapture) (codexSession, error) {
		return session, nil
	}}
	result := adapter.Discover(context.Background())
	if result.Status != StatusSupported || result.Authentication.Status != AuthAvailable {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Models) != 1 {
		t.Fatalf("models = %#v", result.Models)
	}
	if result.Models[0].ID != "gpt-5.6-luna" {
		t.Fatalf("model = %#v", result.Models[0])
	}
	if !session.closed {
		t.Fatal("Codex session was not closed")
	}
}

type testWriteCloser struct{ io.Writer }

func (testWriteCloser) Close() error { return nil }

type testCodexSession struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	closed bool
}

func (session *testCodexSession) Stdin() io.WriteCloser { return session.stdin }
func (session *testCodexSession) Stdout() io.ReadCloser { return session.stdout }
func (session *testCodexSession) Close() {
	session.closed = true
	_ = session.stdin.Close()
	_ = session.stdout.Close()
}

func TestCodexCaptureUsesOneAggregateBudget(t *testing.T) {
	cancelled := false
	capture := &codexCapture{boundedCapture: boundedCapture{limit: 4, cancel: func() { cancelled = true }}}
	if got, _ := capture.Write([]byte("abc")); got != 3 {
		t.Fatalf("first write = %d, want 3", got)
	}
	if got, _ := capture.Write([]byte("def")); got != 3 {
		t.Fatalf("second write = %d, want 3", got)
	}
	if capture.stderr.String() != "abcd" {
		t.Fatalf("capture = %q, want abcd", capture.stderr.String())
	}
	if !cancelled {
		t.Fatal("capture did not cancel after exceeding its budget")
	}
	if !capture.overflow.Load() {
		t.Fatal("capture did not record overflow")
	}
}

func TestCodexRPCReadHonorsContextCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	capture := &codexCapture{boundedCapture: boundedCapture{limit: maxCaptureBytes}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := readRPCResponse(ctx, bufio.NewReader(reader), capture, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("context-aware RPC read took %s", elapsed)
	}
	_ = writer.Close()
}

func TestCommandDiagnosticDoesNotExposeHarnessSecrets(t *testing.T) {
	run := RunResult{Stderr: []byte(`provider quota exhausted authentication required api_key=secret-value account_id=acct-secret`), Err: errors.New("token=token-secret")}
	diagnostic := commandDiagnostic(run)
	if !strings.Contains(diagnostic, "provider quota exhausted") {
		t.Fatalf("diagnostic %q omitted provider failure", diagnostic)
	}
	for _, secret := range []string{"secret-value", "acct-secret", "token-secret"} {
		if strings.Contains(diagnostic, secret) {
			t.Fatalf("diagnostic %q exposed %q", diagnostic, secret)
		}
	}
	if authenticationDiagnostic(run) == "" {
		t.Fatal("secret-bearing authentication diagnostic was not classified")
	}
	jsonDiagnostic := commandDiagnostic(RunResult{Err: errors.New(`{"api_key":"secret","account_id":"acct-secret","account_email":"user@example.com"}`)})
	for _, secret := range []string{"secret", "acct-secret", "user@example.com"} {
		if strings.Contains(jsonDiagnostic, secret) {
			t.Fatalf("JSON diagnostic %q exposed %q", jsonDiagnostic, secret)
		}
	}
	envDiagnostic := commandDiagnostic(RunResult{Err: errors.New("OPENAI_API_KEY=secret-value")})
	if strings.Contains(envDiagnostic, "secret-value") {
		t.Fatalf("environment diagnostic %q exposed an API key", envDiagnostic)
	}
}

func TestCodexEnvironmentIncludesConfiguredHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "/tmp/review-party-codex")
	for _, entry := range environmentFor("codex") {
		if entry == "CODEX_HOME=/tmp/review-party-codex" {
			return
		}
	}
	t.Fatal("CODEX_HOME was omitted from the Codex environment allowlist")
}

func requireDiscoveryStatus(t *testing.T, result Result, wantStatus Status, wantAuth AuthStatus) {
	t.Helper()
	if result.Status != wantStatus {
		t.Fatalf("status = %s, want %s", result.Status, wantStatus)
	}
	if result.Authentication.Status != wantAuth {
		t.Fatalf("authentication status = %s, want %s", result.Authentication.Status, wantAuth)
	}
}

func discoverOpenCodeWithProviders(t *testing.T, providers string) Result {
	t.Helper()
	runner := &scriptedRunner{runs: map[string]RunResult{
		"models":         {Stdout: []byte("openai/gpt-5\n")},
		"providers list": {Stdout: []byte(providers)},
	}}
	return NewService(Options{Adapters: []Adapter{NewOpenCodeAdapter(runner)}}).Discover(context.Background(), "opencode")
}
