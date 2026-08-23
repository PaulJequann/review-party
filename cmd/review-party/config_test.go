package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func TestExplainUsesDefaultUserConfiguration(t *testing.T) {
	configurationRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configurationRoot)
	path := filepath.Join(configurationRoot, "review-party", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := `{"schema_version":1,"defaults":{"reviewer":"opencode"},"reviewers":{"opencode":{"enabled":true,"model":"meta/muse-spark-1.2-contributor","allowed_models":["meta/muse-spark-1.2-contributor","opencode-go/deepseek-v4-flash"]}}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"explain", "bugs", "--format", "json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var explanation model.ProfileExplanation
	if err := json.Unmarshal(stdout.Bytes(), &explanation); err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.Reviewer.ReviewerID != "opencode" || explanation.ProfileRevision.Model != "meta/muse-spark-1.2-contributor" {
		t.Fatalf("explanation = %#v", explanation)
	}
}

func TestExplainAppliesExplicitEffort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	payload := `{"schema_version":1,"defaults":{"reviewer":"opencode"},"reviewers":{"opencode":{"enabled":true,"model":"meta/muse-spark-1.2-contributor"}}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"explain", "bugs", "--config", path, "--effort", "high", "--format", "json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var explanation model.ProfileExplanation
	if err := json.Unmarshal(stdout.Bytes(), &explanation); err != nil {
		t.Fatal(err)
	}
	if got := explanation.ProfileRevision.Reviewer.Effort; got != "high" {
		t.Fatalf("effort = %q, want high", got)
	}
}

func TestConfigPathUsesXDGConfigurationDirectory(t *testing.T) {
	configurationRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configurationRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"config", "path"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	want := filepath.Join(configurationRoot, "review-party", "config.json") + "\n"
	if stdout.String() != want {
		t.Fatalf("path = %q, want %q", stdout.String(), want)
	}
}

func TestConfigShowReadsSelectedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selected.json")
	payload := `{"schema_version":1,"defaults":{"reviewer":"opencode"},"reviewers":{"opencode":{"enabled":true,"model":"meta/muse-spark-1.2-contributor"}}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"config", "show", "--config", path}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var configuration struct {
		Defaults struct {
			Reviewer string `json:"reviewer"`
		} `json:"defaults"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &configuration); err != nil {
		t.Fatal(err)
	}
	if configuration.Defaults.Reviewer != "opencode" {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestReviewAppliesConfiguredModelPolicyBeforeSubjectResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	payload := `{"schema_version":1,"defaults":{"reviewer":"opencode"},"reviewers":{"opencode":{"enabled":true,"model":"meta/muse-spark-1.2-contributor","allowed_models":["meta/muse-spark-1.2-contributor"]}}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	prepareCommandState(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"review", "bugs", "--config", path, "--repo", "/repository-must-not-be-resolved", "--reviewer", "opencode", "--model", "unapproved/model"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), `model "unapproved/model" is not allowed for reviewer "opencode"`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestExplainDoesNotLoadConfigurationFromWorkingDirectory(t *testing.T) {
	repository := t.TempDir()
	command := exec.Command("git", "init", "--quiet")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	payload := `{"schema_version":1,"reviewers":{"grok":{"enabled":false}}}`
	if err := os.WriteFile(filepath.Join(repository, "review-party-config.json"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repository)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"explain", "bugs", "--format", "json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var explanation model.ProfileExplanation
	if err := json.Unmarshal(stdout.Bytes(), &explanation); err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.Reviewer.ReviewerID != "grok" {
		t.Fatalf("reviewer = %#v", explanation.ProfileRevision.Reviewer)
	}
}
