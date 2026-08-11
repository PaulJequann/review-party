package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func TestProfilesCommandEmitsJSONCatalog(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	repository := isolatedProfilesRepository(t)

	exitCode := run(context.Background(), []string{"profiles", "--repo", repository, "--format", "json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var profiles []model.ProfileSummary
	if err := json.Unmarshal(stdout.Bytes(), &profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles[1].Name != "documentation" {
		t.Fatalf("profiles = %#v", profiles)
	}
}

func TestExplainCommandReportsRecipeWithoutAvailability(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	repository := isolatedProfilesRepository(t)

	exitCode := run(context.Background(), []string{"explain", "documentation", "--repo", repository, "--reviewer", "copilot"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, expected := range []string{"profile: documentation", "reviewer: copilot (explicit)", "pass: documentation-review", "availability: not checked", "no Agent Harness launched"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("output omits %q:\n%s", expected, stdout.String())
		}
	}
}

func TestProfilesCommandUsesConfiguredDefault(t *testing.T) {
	configurationRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configurationRoot)
	path := filepath.Join(configurationRoot, "review-party", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := `{"version":1,"default_reviewer":"opencode","reviewers":{"grok":{"enabled":false},"opencode":{"enabled":true,"model":"meta/muse-spark-1.2-contributor"}}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	repository := isolatedProfilesRepository(t)

	exitCode := run(context.Background(), []string{"profiles", "--repo", repository, "--format", "json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var profiles []model.ProfileSummary
	if err := json.Unmarshal(stdout.Bytes(), &profiles); err != nil {
		t.Fatal(err)
	}
	assertProfileNames(t, profiles)
	if profiles[0].DefaultReviewer.ReviewerID != "opencode" {
		t.Fatalf("profile = %#v", profiles[0])
	}
	if profiles[1].DefaultReviewer.ReviewerID != "opencode" {
		t.Fatalf("profile = %#v", profiles[1])
	}
}

func TestProfilesDoesNotRequireUsableRecordStorage(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(statePath, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", statePath)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	repository := isolatedProfilesRepository(t)

	exitCode := run(context.Background(), []string{"profiles", "--repo", repository, "--format", "json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var profiles []model.ProfileSummary
	if err := json.Unmarshal(stdout.Bytes(), &profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %#v", profiles)
	}
}

func isolatedProfilesRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	command := exec.Command("git", "init", "--quiet")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	return repository
}

func assertProfileNames(t *testing.T, profiles []model.ProfileSummary) {
	t.Helper()
	if len(profiles) != 2 {
		t.Fatalf("profiles = %#v", profiles)
	}
	got := []string{profiles[0].Name, profiles[1].Name}
	want := []string{"bugs", "documentation"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profile names = %#v, want %#v", got, want)
	}
}
