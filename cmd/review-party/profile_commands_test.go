package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitAndProfilesCommandsExposeRepositoryLibrary(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	command := exec.Command("git", "init", "--quiet")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}

	initOutput := runProfileCommand(t, []string{"init", "--repo", repository})
	assertOutputContains(t, initOutput, ".reviewparty", "bugs.md")
	profilesOutput := runProfileCommand(t, []string{"profiles", "--repo", repository})
	assertOutputContains(t, profilesOutput, "bugs", "repository:.reviewparty/profiles/bugs.md")
	explainOutput := runProfileCommand(t, []string{"profile", "explain", "bugs", "--repo", repository})
	assertOutputContains(t, explainOutput, "repository:.reviewparty/profiles/bugs.md", "grok-4.5/high", "bug-review", "PROFILE MARKDOWN")
}

func runProfileCommand(t *testing.T, arguments []string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), arguments, &stdout, &stderr); exit != 0 {
		t.Fatalf("run(%v) exit = %d, stderr = %q", arguments, exit, stderr.String())
	}
	return stdout.String()
}

func assertOutputContains(t *testing.T, output string, expectedValues ...string) {
	t.Helper()
	for _, expected := range expectedValues {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestReadOnlyProfileCommandsDoNotInitializeRecordStorage(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	command := exec.Command("git", "init", "--quiet")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	blockedStateHome := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedStateHome, []byte("file blocks state directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blockedStateHome)

	for _, arguments := range [][]string{
		{"profiles", "--repo", repository},
		{"profile", "explain", "bugs", "--repo", repository},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exit := run(context.Background(), arguments, &stdout, &stderr); exit != 0 {
			t.Fatalf("run(%v) exit = %d, stderr = %q", arguments, exit, stderr.String())
		}
	}
}

func TestReviewWithoutProfileUsesRepositoryDefault(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "config", "user.email", "review-party@example.invalid"))
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "config", "user.name", "Review Party Test"))
	profileDirectory := filepath.Join(repository, ".reviewparty", "profiles")
	if err := os.MkdirAll(profileDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".reviewparty", "config.json"), []byte(`{"schema":1,"defaultProfile":"security"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDirectory, "security.md"), []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "add", ".reviewparty"))
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "commit", "--quiet", "-m", "test fixture"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(context.Background(), []string{"review", "--repo", repository}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
	for _, expected := range []string{"repository:.reviewparty/profiles/security.md", "is empty"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("stderr %q does not contain %q", stderr.String(), expected)
		}
	}
}

func runProfileTestCommand(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", command.Args, err, output)
	}
}

func isolateProfileCommandEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("REVIEW_PARTY_HOME", t.TempDir())
}
