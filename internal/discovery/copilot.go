package discovery

import "context"

type copilotAdapter struct{}

// NewCopilotAdapter constructs the intentionally unsupported Copilot adapter.
func NewCopilotAdapter() Adapter { return copilotAdapter{} }

func (copilotAdapter) Reviewer() string { return "copilot" }

func (copilotAdapter) Discover(context.Context) Observation {
	signIn := &SignInAction{
		Command:          []string{"copilot", "login"},
		Description:      "Authenticate GitHub Copilot explicitly. Copilot model discovery is not available through a safe machine interface.",
		DocumentationURL: "https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/authenticate-copilot-cli",
	}
	return Observation{
		Status:         StatusUnsupported,
		Authentication: Authentication{Status: AuthUnsupported, SignIn: signIn},
		Diagnostic:     "Copilot has no supported observational model-list interface",
	}
}
