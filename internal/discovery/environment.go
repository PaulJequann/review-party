package discovery

import (
	"os"
	"strings"
)

func environmentFor(reviewer string) []string {
	allowed := baseEnvironmentNames()
	addReviewerEnvironmentNames(allowed, reviewer)
	delete(allowed, "PATH")
	delete(allowed, environmentNameKey("PATH"))
	return append(currentEnvironment(allowed), "PATH="+strings.Join(trustedExecutableRoots(), string(os.PathListSeparator)))
}

func baseEnvironmentNames() map[string]bool {
	return map[string]bool{
		"PATH": true, "HOME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true,
		"TERM": true, "NO_COLOR": true, "XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true,
		"XDG_DATA_HOME": true, "XDG_STATE_HOME": true, "HTTP_PROXY": true,
		"HTTPS_PROXY": true, "NO_PROXY": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
		"USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "SystemRoot": true,
		"TEMP": true, "TMP": true, "PATHEXT": true, "ComSpec": true,
	}
}

func addReviewerEnvironmentNames(allowed map[string]bool, reviewer string) {
	for _, name := range map[string][]string{
		"grok":     {"GROK_API_KEY", "XAI_API_KEY"},
		"opencode": {"OPENCODE_CONFIG", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_GENERATIVE_AI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY", "MISTRAL_API_KEY", "DEEPSEEK_API_KEY"},
		"copilot":  {"GH_TOKEN", "GITHUB_TOKEN"},
		"codex":    {"CODEX_HOME", "OPENAI_API_KEY", "CODEX_API_KEY"},
	}[reviewer] {
		allowed[name] = true
	}
	for name := range allowed {
		key := environmentNameKey(name)
		if key != name {
			delete(allowed, name)
			allowed[key] = true
		}
	}
}

func currentEnvironment(allowed map[string]bool) []string {
	for name := range allowed {
		key := environmentNameKey(name)
		allowed[key] = true
	}
	result := make([]string, 0, len(allowed))
	for _, value := range os.Environ() {
		name, _, found := strings.Cut(value, "=")
		if found && allowed[environmentNameKey(name)] {
			result = append(result, value)
		}
	}
	return result
}
