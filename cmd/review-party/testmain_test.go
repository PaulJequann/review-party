package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reviewparty/internal/hostrun"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	isolateGitTestEnvironment()
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		panic(err)
	}
	home, err := os.MkdirTemp(tempRoot, "cmd-review-party-")
	if err != nil {
		panic(err)
	}
	temp := filepath.Join(home, "tmp")
	if err := os.Mkdir(temp, 0o700); err != nil {
		panic(err)
	}
	gitConfig := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(gitConfig, []byte("[init]\n\tdefaultBranch = main\n"), 0o600); err != nil {
		panic(err)
	}
	for key, value := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_STATE_HOME": filepath.Join(home, "state"), "XDG_CACHE_HOME": filepath.Join(home, "cache"),
		"APPDATA": filepath.Join(home, "AppData", "Roaming"),
		"TMPDIR":  temp, "GIT_CONFIG_GLOBAL": gitConfig,
	} {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	hostrun.Init()
	status := m.Run()
	if err := os.RemoveAll(home); err != nil {
		fmt.Fprintln(os.Stderr, err)
		status = 1
	}
	os.Exit(status)
}

func isolatedConfigRoot(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

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
