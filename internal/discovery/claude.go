package discovery

import (
	"context"
	"encoding/json"
	"regexp"
)

// claudeVersionPattern matches the bare leading version Claude Code prints,
// such as "2.1.283 (Claude Code)", which the shared prefixed pattern misses.
var claudeVersionPattern = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+){1,3})\b`)

type claudeAdapter struct{ runner Runner }

// NewClaudeAdapter constructs the Claude Code discovery adapter. Claude Code
// reports authentication through a machine interface but has no model list,
// so models are always entered manually.
func NewClaudeAdapter(runner Runner) Adapter {
	if runner == nil {
		runner = NewDefaultRunner()
	}
	return claudeAdapter{runner: runner}
}

func (adapter claudeAdapter) Reviewer() string { return "claude" }

func (adapter claudeAdapter) Discover(ctx context.Context) Observation {
	signIn := &SignInAction{
		Command:          []string{"claude", "auth", "login"},
		Description:      "Authenticate Claude Code explicitly, then run discovery again.",
		DocumentationURL: "https://code.claude.com/docs/en/authentication",
	}
	run := adapter.runner.Run(ctx, Command{Args: []string{"claude", "auth", "status", "--json"}, Environment: environmentFor("claude")})
	var status struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if json.Unmarshal(run.Stdout, &status) != nil || status.LoggedIn == nil {
		if commandFailed(run) {
			return firstStatusFailure(run, signIn)
		}
		return Observation{Status: StatusUnsupported, Authentication: Authentication{Status: AuthUnknown, SignIn: signIn}, Diagnostic: "claude auth status output was not recognized"}
	}
	if !*status.LoggedIn {
		diagnostic := "Claude Code is not logged in"
		return Observation{Status: StatusAuthenticationRequired, Authentication: Authentication{Status: AuthRequired, Diagnostic: diagnostic, SignIn: signIn}, Diagnostic: diagnostic}
	}
	return Observation{
		Status:         StatusUnsupported,
		HarnessVersion: claudeVersion(ctx, adapter.runner),
		Authentication: Authentication{Status: AuthConfigured, SignIn: signIn},
		Diagnostic:     "Claude Code has no observational model-list interface",
	}
}

func claudeVersion(ctx context.Context, runner Runner) string {
	probeContext, cancel := optionalProbeContext(ctx)
	defer cancel()
	run := runner.Run(probeContext, Command{Args: []string{"claude", "--version"}, Environment: environmentFor("claude")})
	if match := claudeVersionPattern.FindSubmatch(run.Stdout); len(match) == 2 {
		return string(match[1])
	}
	return "unknown"
}
