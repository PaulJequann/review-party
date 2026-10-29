package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"reviewparty/internal/configuration"
)

func TestConfigurationHubResolvesGitRepositoryRoot(t *testing.T) {
	root := testGitRepository(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveConfigurationHubRepository(nested, true)
	if err != nil {
		t.Fatalf("resolve repository: %v", err)
	}
	if resolved != root {
		t.Fatalf("resolved repository = %q, want %q", resolved, root)
	}
}

func runOutsideRepository(t *testing.T, input string, arguments ...string) (int, string, string) {
	t.Helper()
	isolateProfileCommandEnvironment(t)
	parent := t.TempDir()
	plain := filepath.Join(parent, "plain")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", parent)
	t.Chdir(plain)
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	var stdout, stderr bytes.Buffer
	exit := execute(context.Background(), arguments, commandIO{
		input: iotest.OneByteReader(strings.NewReader(input)), output: &stdout, errors: &stderr,
		terminal:             func(any) bool { return true },
		configurationManager: func(string) *configuration.Manager { return manager },
	})
	return exit, stdout.String(), stderr.String()
}

func TestConfigurationHubOutsideARepositoryOpensGlobalConfigurationOnly(t *testing.T) {
	exit, stdout, stderr := runOutsideRepository(t, "8\n", "config", "--accessible")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q\n%s", exit, stderr, stdout)
	}
	for _, want := range []string{"[global only]", "Not in a git repository"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("Hub outside a repository lacks %q:\n%s", want, stdout)
		}
	}
}

func TestConfigurationHubWithAnExplicitNonRepositoryStillFails(t *testing.T) {
	for _, repository := range []string{".", "../plain"} {
		exit, stdout, stderr := runOutsideRepository(t, "8\n", "config", "--accessible", "--repo", repository)
		if exit == 0 || !strings.Contains(stderr, "not a git repository") || strings.Contains(stdout, "Configuration Hub") {
			t.Fatalf("--repo %s: exit = %d, stderr = %q, stdout = %q", repository, exit, stderr, stdout)
		}
	}
}

func TestInitOutsideARepositoryDoesNotFallBackToGlobalOnly(t *testing.T) {
	exit, stdout, stderr := runOutsideRepository(t, "", "init", "--accessible")
	if exit == 0 || !strings.Contains(stderr, "not a git repository") || strings.Contains(stdout, "[global only]") {
		t.Fatalf("init exit = %d, stderr = %q, stdout = %q", exit, stderr, stdout)
	}
}
