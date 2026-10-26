package subject

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	isolateGitTestEnvironment()
	os.Exit(m.Run())
}

// Clear caller routing once, before tests install their deliberate Git settings.
func isolateGitTestEnvironment() {
	output, err := exec.Command("git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		panic(err)
	}
	keys := strings.Fields(string(output))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_CONFIG_") {
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
	for _, key := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if err := os.Setenv(key, os.DevNull); err != nil {
			panic(err)
		}
	}
}
