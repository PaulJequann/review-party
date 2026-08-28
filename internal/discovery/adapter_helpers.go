package discovery

import (
	"context"
	"regexp"
	"strings"
	"time"
)

const optionalProbeFallback = time.Second

var versionPattern = regexp.MustCompile(`(?i)(?:version|v)[^0-9]*([0-9]+(?:\.[0-9]+){1,3})`)
var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)

var authenticationFailureMarkers = []string{
	"unauthorized", "not authenticated", "token refresh", "login required", "authentication required", "no credentials",
	"credentials not configured", "authentication failed", "requires authentication", "missing api key",
	"api key required", "api key not configured", "no api key", "run login", "not logged in", "sign in",
}

type humanDiscoverySpec struct {
	executable string
	parse      func([]byte) []Model
}

func versionFromOutput(output []byte) string {
	match := versionPattern.FindSubmatch(output)
	if len(match) == 2 {
		return string(match[1])
	}
	return ""
}

func harnessVersion(ctx context.Context, runner Runner, spec humanDiscoverySpec) string {
	probeContext, cancel := optionalProbeContext(ctx)
	defer cancel()
	run := runner.Run(probeContext, Command{Args: []string{spec.executable, "--version"}, Environment: environmentFor(spec.executable)})
	return firstNonempty(versionFromOutput(run.Stdout), versionFromOutput(run.Stderr), "unknown")
}

func optionalProbeContext(parent context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.WithCancel(parent)
		}
		return context.WithTimeout(parent, remaining/4)
	}
	return context.WithTimeout(parent, optionalProbeFallback)
}

func discoverHumanModels(ctx context.Context, runner Runner, spec humanDiscoverySpec, signIn *SignInAction) (RunResult, []Model, *Observation) {
	run := runner.Run(ctx, Command{Args: []string{spec.executable, "models"}, Environment: environmentFor(spec.executable)})
	if commandFailed(run) {
		if classifyCommandAuthentication(run) == AuthRequired {
			diagnostic := authenticationDiagnostic(run)
			failure := Observation{Status: StatusAuthenticationRequired, Authentication: Authentication{Status: AuthRequired, Diagnostic: diagnostic, SignIn: signIn}, Diagnostic: diagnostic}
			return run, nil, &failure
		}
		failure := firstStatusFailure(run, signIn)
		return run, nil, &failure
	}
	if ctx.Err() != nil {
		failure := Observation{Status: StatusUnavailable, Authentication: Authentication{Status: AuthUnavailable, SignIn: signIn}, Diagnostic: ctx.Err().Error()}
		return run, nil, &failure
	}
	models := spec.parse(run.Stdout)
	if len(models) > 0 {
		return run, models, nil
	}
	if classifyCommandAuthentication(run) == AuthRequired {
		diagnostic := "discovery command reported that authentication is required"
		failure := Observation{Status: StatusAuthenticationRequired, Authentication: Authentication{Status: AuthRequired, Diagnostic: diagnostic, SignIn: signIn}, Diagnostic: diagnostic}
		return run, nil, &failure
	}
	failure := Observation{Status: StatusUnsupported, Authentication: Authentication{Status: AuthUnknown, SignIn: signIn}, Diagnostic: spec.executable + " models output was not recognized"}
	return run, nil, &failure
}

func isAuthenticationDiagnostic(value []byte) bool {
	value = []byte(strings.ToLower(string(value)))
	return strings.Contains(string(value), "authentication") || strings.Contains(string(value), "credentials") || containsAuthenticationFailureMarker(value)
}

func classifyCommandAuthentication(run RunResult) AuthStatus {
	if commandRequiresAuthentication(run) {
		return AuthRequired
	}
	if commandFailed(run) {
		return AuthUnknown
	}
	return AuthConfigured
}

func commandRequiresAuthentication(run RunResult) bool {
	return isAuthenticationFailureText(run.Stdout) || isAuthenticationFailureText([]byte(rawCommandDiagnostic(run)))
}

func isAuthenticationFailureText(value []byte) bool {
	value = []byte(strings.ToLower(string(value)))
	if containsAuthenticationFailureMarker(value) {
		return true
	}
	for _, line := range strings.Split(string(value), "\n") {
		if strings.TrimSpace(line) == "authentication required" || strings.TrimSpace(line) == "authentication: required" {
			return true
		}
	}
	return false
}

func containsAuthenticationFailureMarker(value []byte) bool {
	for _, marker := range authenticationFailureMarkers {
		if strings.Contains(string(value), marker) {
			return true
		}
	}
	return false
}

func modelID(value []byte) string {
	text := strings.TrimSpace(string(value))
	return strings.Trim(text, "*`'\"[](),:")
}

func validHumanModelID(value []byte) bool {
	text := string(value)
	if text == "" {
		return false
	}
	if len(text) > 256 {
		return false
	}
	if !modelIDPattern.MatchString(text) {
		return false
	}
	switch strings.ToLower(text) {
	case "available", "found", "models", "model", "default", "authenticated", "provider", "name":
		return false
	default:
		return true
	}
}

func deduplicateModels(models []Model) []Model {
	seen := make(map[string]int, len(models))
	result := make([]Model, 0, len(models))
	for _, model := range models {
		model.ID = modelID([]byte(model.ID))
		if !validHumanModelID([]byte(model.ID)) {
			continue
		}
		if index, found := seen[model.ID]; found {
			result[index] = mergeModelMetadata(result[index], model)
			continue
		}
		seen[model.ID] = len(result)
		result = append(result, cloneModel(model))
	}
	return result
}
