package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

type codexAdapter struct {
	start codexSessionFactory
}

type codexSessionFactory func(context.Context, *codexCapture) (codexSession, error)

type codexDiscoveryState struct {
	callerContext context.Context
	runContext    context.Context
	session       codexSession
	reader        *bufio.Reader
	capture       *codexCapture
	signIn        *SignInAction
}

type codexSession interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Close()
}

// NewCodexAdapter constructs the Codex app-server discovery adapter.
func NewCodexAdapter() Adapter { return codexAdapter{start: newCodexSession} }

func (codexAdapter) Reviewer() string { return "codex" }

func (adapter codexAdapter) Discover(ctx context.Context) Observation {
	signIn := &SignInAction{
		Command:          []string{"codex", "login"},
		Description:      "Authenticate Codex explicitly, then run discovery again.",
		DocumentationURL: "https://developers.openai.com/codex/auth/",
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	capture := &codexCapture{boundedCapture: boundedCapture{limit: maxCaptureBytes, cancel: cancel}}
	start := adapter.start
	if start == nil {
		start = newCodexSession
	}
	session, err := start(runCtx, capture)
	if err != nil {
		return Observation{Status: StatusUnavailable, Authentication: Authentication{Status: AuthUnavailable, SignIn: signIn}, Diagnostic: compactDiagnostic(err.Error())}
	}
	finish := func() {
		cancel()
		session.Close()
	}
	defer finish()

	reader := bufio.NewReaderSize(session.Stdout(), 64*1024)
	return adapter.discoverCodexSession(codexDiscoveryState{
		callerContext: ctx, runContext: runCtx, session: session,
		reader: reader, capture: capture, signIn: signIn,
	})
}

func (adapter codexAdapter) discoverCodexSession(state codexDiscoveryState) Observation {
	userAgent, failure := initializeCodexSession(state)
	if failure != nil {
		return *failure
	}
	authentication, failure := readCodexAuthentication(state)
	if failure != nil {
		return *failure
	}
	if authentication.Status == AuthRequired {
		return Observation{Status: StatusAuthenticationRequired, Authentication: authentication, HarnessVersion: versionFromUserAgent(userAgent), Diagnostic: authentication.Diagnostic}
	}
	models, failure := listCodexModels(state)
	if failure != nil {
		return *failure
	}
	return Observation{
		Status: StatusSupported, Models: models, ModelsComplete: true,
		HarnessVersion: versionFromUserAgent(userAgent), Authentication: authentication,
	}
}

func initializeCodexSession(state codexDiscoveryState) (string, *Observation) {
	if err := writeRPC(state.session.Stdin(), 1, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "review-party", "title": "Review Party", "version": "dev"},
		"capabilities": map[string]bool{"experimentalApi": false},
	}); err != nil {
		failure := codexUnavailable(state.callerContext, state.signIn, err)
		return "", &failure
	}
	initialized, err := readRPCResponse(state.runContext, state.reader, state.capture, 1)
	if err != nil {
		failure := codexRPCFailure(state.callerContext, state.signIn, err)
		return "", &failure
	}
	var initializeResult struct {
		UserAgent string `json:"userAgent"`
	}
	if err := json.Unmarshal(initialized.Result, &initializeResult); err != nil {
		failure := codexUnsupported(state.callerContext, state.signIn, fmt.Errorf("decode initialize response: %w", err))
		return "", &failure
	}
	if err := writeRPCNotification(state.session.Stdin(), "initialized", nil); err != nil {
		failure := codexUnavailable(state.callerContext, state.signIn, err)
		return "", &failure
	}
	return initializeResult.UserAgent, nil
}

func readCodexAuthentication(state codexDiscoveryState) (Authentication, *Observation) {
	if err := writeRPC(state.session.Stdin(), 2, "account/read", map[string]bool{"refreshToken": false}); err != nil {
		failure := codexUnavailable(state.callerContext, state.signIn, err)
		return Authentication{}, &failure
	}
	accountResponse, err := readRPCResponse(state.runContext, state.reader, state.capture, 2)
	if err != nil {
		failure := codexRPCFailure(state.callerContext, state.signIn, err)
		return Authentication{}, &failure
	}
	authentication, err := codexAuthentication(accountResponse.Result, state.signIn)
	if err != nil {
		failure := codexUnsupported(state.callerContext, state.signIn, err)
		return Authentication{}, &failure
	}
	return authentication, nil
}

func listCodexModels(state codexDiscoveryState) ([]Model, *Observation) {
	models := make([]Model, 0)
	var cursor *string
	for page := 0; page < 100; page++ {
		requestID := page + 3
		if err := writeRPC(state.session.Stdin(), requestID, "model/list", map[string]any{"cursor": cursor, "limit": 100, "includeHidden": false}); err != nil {
			failure := codexUnavailable(state.callerContext, state.signIn, err)
			return nil, &failure
		}
		response, err := readRPCResponse(state.runContext, state.reader, state.capture, requestID)
		if err != nil {
			failure := codexRPCFailure(state.callerContext, state.signIn, err)
			return nil, &failure
		}
		var pageResult codexModelPage
		if err := json.Unmarshal(response.Result, &pageResult); err != nil {
			failure := codexUnsupported(state.callerContext, state.signIn, fmt.Errorf("decode model/list response: %w", err))
			return nil, &failure
		}
		models = append(models, pageResult.ModelsAsModels()...)
		if noNextCursor(pageResult.NextCursor) {
			return deduplicateModels(models), nil
		}
		cursor = pageResult.NextCursor
	}
	failure := codexUnsupported(state.callerContext, state.signIn, errors.New("model/list pagination exceeded the page limit"))
	return nil, &failure
}

func noNextCursor(cursor *string) bool {
	if cursor == nil {
		return true
	}
	return *cursor == ""
}

type execCodexSession struct {
	process *processSession
	stdin   io.WriteCloser
	stdout  io.ReadCloser
}

func newCodexSession(ctx context.Context, capture *codexCapture) (codexSession, error) {
	_ = ctx
	environment := environmentFor("codex")
	executable, err := trustedExecutable("codex", environment)
	if err != nil {
		return nil, err
	}
	process := exec.Command(executable, "app-server")
	process.Env = environment
	stdin, err := process.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	process.Stderr = capture
	started, err := startProcessSession(process, stdin, stdout)
	if err != nil {
		return nil, err
	}
	return &execCodexSession{process: started, stdin: stdin, stdout: stdout}, nil
}

func (session *execCodexSession) Stdin() io.WriteCloser { return session.stdin }
func (session *execCodexSession) Stdout() io.ReadCloser { return session.stdout }

func (session *execCodexSession) Close() {
	session.process.Close()
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type codexModelPage struct {
	Models     []codexModel `json:"data"`
	NextCursor *string      `json:"nextCursor"`
}

type codexModel struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"displayName"`
	Name             string   `json:"name"`
	Default          bool     `json:"isDefault"`
	LegacyDefault    bool     `json:"default"`
	ReasoningEfforts []string `json:"supportedReasoningEfforts"`
}

func (model codexModel) toModel() Model {
	return Model{ID: model.ID, DisplayName: firstNonempty(model.DisplayName, model.Name), Default: model.Default || model.LegacyDefault, ReasoningEfforts: append([]string(nil), model.ReasoningEfforts...)}
}

func (page codexModelPage) ModelsAsModels() []Model {
	models := make([]Model, 0, len(page.Models))
	for _, model := range page.Models {
		models = append(models, model.toModel())
	}
	return models
}

func writeRPC(writer io.Writer, id int, method string, params any) error {
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	return json.NewEncoder(writer).Encode(payload)
}

func writeRPCNotification(writer io.Writer, method string, params any) error {
	payload := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	return json.NewEncoder(writer).Encode(payload)
}

func readRPCResponse(ctx context.Context, reader *bufio.Reader, capture *codexCapture, id int) (rpcMessage, error) {
	type response struct {
		message rpcMessage
		err     error
	}
	result := make(chan response, 1)
	go func() {
		message, err := readRPCResponseFromStream(reader, capture, id)
		result <- response{message: message, err: err}
	}()
	select {
	case response := <-result:
		return response.message, response.err
	case <-ctx.Done():
		return rpcMessage{}, ctx.Err()
	}
}

func readRPCResponseFromStream(reader *bufio.Reader, capture *codexCapture, id int) (rpcMessage, error) {
	for {
		line, err := readRPCLine(reader, capture)
		if err != nil {
			return rpcMessage{}, err
		}
		if len(line) == 0 {
			continue
		}
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			return rpcMessage{}, fmt.Errorf("decode app-server message: %w", err)
		}
		if !rpcIDMatches(message.ID, id) {
			continue
		}
		if message.Error != nil {
			return rpcMessage{}, fmt.Errorf("app-server request %d: %s", id, message.Error.Message)
		}
		return message, nil
	}
}

func readRPCLine(reader *bufio.Reader, capture *codexCapture) ([]byte, error) {
	line := make([]byte, 0, 4096)
	for {
		part, prefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		lineBytes := len(part)
		if !prefix {
			lineBytes++
		}
		if capture.reserve(lineBytes) < lineBytes {
			return nil, errors.New("app-server output exceeded the aggregate capture limit")
		}
		line = append(line, part...)
		if !prefix {
			return line, nil
		}
	}
}

type codexCapture struct {
	boundedCapture
	stderr boundedBuffer
}

func (capture *codexCapture) Write(value []byte) (int, error) {
	allowed := capture.reserve(len(value))
	if allowed > 0 {
		capture.stderr.Write(value[:allowed])
	}
	return len(value), nil
}

func rpcIDMatches(raw json.RawMessage, id int) bool {
	if string(raw) == strconv.Itoa(id) {
		return true
	}
	var value string
	return json.Unmarshal(raw, &value) == nil && value == strconv.Itoa(id)
}

func codexAuthentication(payload json.RawMessage, signIn *SignInAction) (Authentication, error) {
	var response struct {
		Account            json.RawMessage `json:"account"`
		RequiresOpenAIAuth bool            `json:"requiresOpenaiAuth"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return Authentication{}, err
	}
	if !response.RequiresOpenAIAuth {
		return Authentication{Status: AuthAvailable, SignIn: signIn}, nil
	}
	if accountIsMissing(response.Account) {
		return Authentication{Status: AuthRequired, SignIn: signIn, Diagnostic: "Codex requires authentication"}, nil
	}
	return Authentication{Status: AuthAvailable, SignIn: signIn}, nil
}

func accountIsMissing(account json.RawMessage) bool {
	if len(account) == 0 {
		return true
	}
	return string(account) == "null"
}

func versionFromUserAgent(userAgent string) string {
	return firstNonempty(versionFromOutput([]byte(userAgent)), userAgent)
}

func codexUnavailable(ctx context.Context, signIn *SignInAction, err error) Observation {
	diagnostic := compactDiagnostic(err.Error())
	if ctx.Err() != nil {
		diagnostic = firstNonempty(diagnostic, ctx.Err().Error())
	}
	return Observation{Status: StatusUnavailable, Authentication: Authentication{Status: AuthUnavailable, SignIn: signIn}, Diagnostic: diagnostic}
}

func codexRPCFailure(ctx context.Context, signIn *SignInAction, err error) Observation {
	diagnostic := compactDiagnostic(err.Error())
	if isAuthenticationDiagnostic([]byte(diagnostic)) {
		return Observation{Status: StatusAuthenticationRequired, Authentication: Authentication{Status: AuthRequired, Diagnostic: diagnostic, SignIn: signIn}, Diagnostic: diagnostic}
	}
	return codexUnavailable(ctx, signIn, errors.New(strings.TrimSpace(diagnostic)))
}

func codexUnsupported(ctx context.Context, signIn *SignInAction, err error) Observation {
	diagnostic := compactDiagnostic(err.Error())
	if ctx.Err() != nil {
		return codexUnavailable(ctx, signIn, err)
	}
	return Observation{Status: StatusUnsupported, Authentication: Authentication{Status: AuthUnknown, SignIn: signIn}, Diagnostic: diagnostic}
}
