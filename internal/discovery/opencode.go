package discovery

import (
	"context"
	"strings"
)

type openCodeAdapter struct{ runner Runner }

// NewOpenCodeAdapter constructs the OpenCode discovery adapter.
func NewOpenCodeAdapter(runner Runner) Adapter {
	if runner == nil {
		runner = NewDefaultRunner()
	}
	return openCodeAdapter{runner: runner}
}

func (adapter openCodeAdapter) Reviewer() string { return "opencode" }

func (adapter openCodeAdapter) Discover(ctx context.Context) Observation {
	signIn := &SignInAction{
		Command:          []string{"opencode", "providers", "login"},
		Description:      "Authenticate an OpenCode provider explicitly, then run discovery again.",
		DocumentationURL: "https://opencode.ai/docs/cli/",
	}
	spec := humanDiscoverySpec{executable: "opencode", parse: parseOpenCodeModels}
	_, models, failure := discoverHumanModels(ctx, adapter.runner, spec, signIn)
	if failure != nil {
		return *failure
	}
	authentication := adapter.observeOpenCodeAuthentication(ctx, signIn)
	return Observation{
		Status: StatusSupported, Models: models, ModelsComplete: true, HarnessVersion: harnessVersion(ctx, adapter.runner, spec),
		Authentication: authentication,
	}
}

func (adapter openCodeAdapter) observeOpenCodeAuthentication(ctx context.Context, signIn *SignInAction) Authentication {
	run := adapter.runner.Run(ctx, Command{Args: []string{"opencode", "providers", "list"}, Environment: environmentFor("opencode")})
	if isOpenCodeCredentialFailure(run.Stdout) || isOpenCodeCredentialFailure([]byte(rawCommandDiagnostic(run))) {
		diagnostic := "OpenCode reported that provider authentication is required"
		return Authentication{Status: AuthRequired, Diagnostic: diagnostic, SignIn: signIn}
	}
	diagnostic := commandDiagnostic(run)
	if commandFailed(run) {
		return Authentication{Status: AuthUnknown, Diagnostic: diagnostic, SignIn: signIn}
	}
	return Authentication{Status: AuthConfigured, SignIn: signIn}
}

func parseOpenCodeModels(output []byte) []Model {
	models := make([]Model, 0)
	for _, line := range strings.Split(string(output), "\n") {
		candidate := modelID([]byte(line))
		if !validOpenCodeModelID(candidate) {
			continue
		}
		models = append(models, Model{ID: candidate})
	}
	return deduplicateModels(models)
}

func validOpenCodeModelID(value string) bool {
	if !validHumanModelID([]byte(value)) {
		return false
	}
	provider, model, found := strings.Cut(value, "/")
	return found && provider != "" && model != "" && !strings.Contains(model, "/")
}
