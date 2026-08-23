package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"reviewparty/internal/engine"
	"reviewparty/internal/store"
)

func TestInitPreparesManagedStateWithoutCreatingProfiles(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	command := exec.Command("git", "init", "--quiet")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}

	initOutput := runProfileCommand(t, []string{"init", "--repo", repository})
	assertOutputContains(t, initOutput, "Review Party is ready", "State is managed automatically", "Next: review-party review bugs")
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_STATE_HOME"), "review-party", "ledger.sqlite")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repository, ".reviewparty")); !os.IsNotExist(err) {
		t.Fatalf("init created Profile material: %v", err)
	}
	second := runProfileCommand(t, []string{"init", "--repo", repository})
	assertOutputContains(t, second, "Existing state was kept unchanged")
}

func TestProfileCreateRequiresExplicitSourceAndRefusesOverwrite(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), []string{"profile", "create", "security", "--repo", repository}, &stdout, &stderr); exit != 1 || !strings.Contains(stderr.String(), "exactly one starting point") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	output := runProfileCommand(t, []string{"profile", "create", "security", "--repo", repository, "--from-packaged", "documentation"})
	assertOutputContains(t, output, "Created owned Profile", "shadow packaged updates")
	stdout.Reset()
	stderr.Reset()
	if exit := run(context.Background(), []string{"profile", "create", "security", "--repo", repository, "--blank"}, &stdout, &stderr); exit != 1 || !strings.Contains(stderr.String(), "refusing to overwrite") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
}

func TestProfileInstallDefaultsCreatesCompleteOwnedSetWithoutSelectingDefault(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	output := runProfileCommand(t, []string{"profile", "install-defaults", "--repo", repository})
	assertOutputContains(t, output, "Installed "+strconv.Itoa(len(engine.SupportedProfiles())), "Default Profile selection was not changed")
	for _, name := range engine.SupportedProfiles() {
		name += ".md"
		if _, err := os.Stat(filepath.Join(repository, ".reviewparty", "profiles", name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(repository, ".reviewparty", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("default selection configuration was created: %v", err)
	}
}

func TestInitRemembersAdvancedStateSelection(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	configurationPath := defaultUserConfigurationPath()
	if err := os.MkdirAll(filepath.Dir(configurationPath), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := `{"schema_version":1}`
	if err := os.WriteFile(configurationPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDirectory := filepath.Join(t.TempDir(), "advanced-state")
	output := runProfileCommand(t, []string{"init", "--repo", repository, "--state-dir", stateDirectory})
	assertOutputContains(t, output, "Advanced state location: "+stateDirectory)
	second := runProfileCommand(t, []string{"init", "--repo", repository})
	assertOutputContains(t, second, "Existing state was kept unchanged", stateDirectory)
	assertEmptyHistory(t, runProfileCommand(t, []string{"history", "--format", "json"}))
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_STATE_HOME"), "review-party")); !os.IsNotExist(err) {
		t.Fatalf("default state was created despite remembered advanced selection: %v", err)
	}
	assertStateSwitchRejected(t, repository)
}

func assertEmptyHistory(t *testing.T, history string) {
	t.Helper()
	var page store.HistoryPage
	if err := json.Unmarshal([]byte(history), &page); err != nil || len(page.Entries) != 0 {
		t.Fatalf("history = %q, page = %#v, error = %v", history, page, err)
	}
}

func assertStateSwitchRejected(t *testing.T, repository string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other-state")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), []string{"init", "--repo", repository, "--state-dir", other}, &stdout, &stderr); exit != 1 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stderr.String(), "refusing to switch") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatalf("conflicting state was created: %v", err)
	}
}

func TestReviewBeforeInitDoesNotCreateState(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(context.Background(), []string{"review", "bugs", "--repo", repository, "--reviewer", "opencode", "--model", "meta/muse-spark-1.2-contributor"}, &stdout, &stderr)
	if exit != 1 || !strings.Contains(stderr.String(), "run review-party init --repo") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_STATE_HOME"), "review-party")); !os.IsNotExist(err) {
		t.Fatalf("review created state before init: %v", err)
	}
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
	if err := os.WriteFile(filepath.Join(repository, ".reviewparty", "config.json"), []byte(`{"schema_version":1,"defaults":{"profile":"security"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDirectory, "security.md"), []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "add", ".reviewparty"))
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "commit", "--quiet", "-m", "test fixture"))
	runProfileCommand(t, []string{"init", "--repo", repository})

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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func prepareCommandState(t *testing.T) {
	t.Helper()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	if err := store.PrepareReviewRecordState(filepath.Join(stateHome, "review-party")); err != nil {
		t.Fatal(err)
	}
}
