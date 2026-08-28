package discovery

import (
	"context"
	"strings"
	"unicode"
)

type grokAdapter struct{ runner Runner }

// NewGrokAdapter constructs the Grok discovery adapter.
func NewGrokAdapter(runner Runner) Adapter {
	if runner == nil {
		runner = NewDefaultRunner()
	}
	return grokAdapter{runner: runner}
}

func (adapter grokAdapter) Reviewer() string { return "grok" }

func (adapter grokAdapter) Discover(ctx context.Context) Observation {
	signIn := &SignInAction{
		Command:          []string{"grok", "login"},
		Description:      "Authenticate Grok explicitly, then run discovery again.",
		DocumentationURL: "https://docs.x.ai/build/cli/reference",
	}
	spec := humanDiscoverySpec{executable: "grok", parse: parseGrokModels}
	_, models, failure := discoverHumanModels(ctx, adapter.runner, spec, signIn)
	if failure != nil {
		return *failure
	}
	return Observation{
		Status: StatusSupported, Models: models, ModelsComplete: true, HarnessVersion: harnessVersion(ctx, adapter.runner, spec),
		Authentication: Authentication{Status: AuthAvailable, SignIn: signIn},
	}
}

func parseGrokModels(output []byte) []Model {
	models := make([]Model, 0)
	for _, line := range strings.Split(string(output), "\n") {
		lower := strings.ToLower(line)
		if isAuthenticationDiagnostic([]byte(lower)) || strings.Contains(lower, "error") {
			continue
		}
		isDefault := strings.Contains(lower, "default")
		fields := strings.Fields(line)
		if len(fields) == 0 || !validGrokModelLine(fields) {
			continue
		}
		models = append(models, Model{ID: modelID([]byte(fields[0])), Default: isDefault})
	}
	return deduplicateModels(models)
}

func validGrokModelLine(fields []string) bool {
	candidate := modelID([]byte(fields[0]))
	if !validHumanModelID([]byte(candidate)) || !strings.ContainsFunc(candidate, unicode.IsLetter) {
		return false
	}
	for _, field := range fields[1:] {
		if modelID([]byte(field)) != "default" {
			return false
		}
	}
	return true
}
