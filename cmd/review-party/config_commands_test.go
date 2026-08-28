package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type configCommandResult struct {
	stdout   string
	stderr   string
	exitCode int
}

type testFile struct {
	path     string
	contents string
}

type profileMutationCase struct {
	arguments  []string
	profileDir string
}

func TestConfigShowReportsEffectiveSelectionAndProvenance(t *testing.T) {
	configRoot := t.TempDir()
	repository := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	requireConfigSuccess(t, []string{
		"config", "profile", "create", "bugs", "--template", "bugs", "--reviewer", "grok",
		"--model", "grok-4.5", "--effort", "high", "--deadline", "1m", "--yes", "--format", "json",
	})
	requireConfigSuccess(t, []string{
		"config", "reviews", "add", "--scope", "global", "--profile", "bugs", "--repo", repository,
		"--yes", "--format", "json",
	})

	result := runConfigCommand(t, []string{"config", "show", "--repo", repository, "--format", "json"})
	requireCommandSuccess(t, result)
	var report struct {
		Effective struct {
			DefaultReviewer struct {
				Value  string `json:"value"`
				Source string `json:"source"`
			} `json:"default_reviewer"`
		} `json:"effective"`
		Reviews struct {
			Expanded []struct {
				Scope   string `json:"scope"`
				Profile string `json:"profile"`
			} `json:"expanded"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
		t.Fatalf("config show output = %q: %v", result.stdout, err)
	}
	if report.Effective.DefaultReviewer.Value != "grok" {
		t.Fatalf("default reviewer = %#v", report.Effective.DefaultReviewer)
	}
	if report.Effective.DefaultReviewer.Source != "packaged" {
		t.Fatalf("default reviewer source = %q", report.Effective.DefaultReviewer.Source)
	}
	if len(report.Reviews.Expanded) != 1 {
		t.Fatalf("expanded reviews = %#v", report.Reviews.Expanded)
	}
	if report.Reviews.Expanded[0].Scope != "global" || report.Reviews.Expanded[0].Profile != "bugs" {
		t.Fatalf("expanded review = %#v", report.Reviews.Expanded[0])
	}
}

func TestConfigFileShowReadsRequestedRepositoryScope(t *testing.T) {
	configRoot := t.TempDir()
	repository := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	payload := "{\n  \"schema_version\": 1,\n  \"reviews\": {\n    \"concurrency_limit\": 1,\n    \"global\": [],\n    \"repository\": []\n  }\n}\n"
	path := filepath.Join(repository, ".reviewparty", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	result := runConfigCommand(t, []string{
		"config", "file", "show", "--scope", "repository", "--repo", repository, "--format", "json",
	})
	requireCommandSuccess(t, result)
	if result.stdout != payload {
		t.Fatalf("authored payload = %q, want %q", result.stdout, payload)
	}
}

func TestConfigValidateReportsInvalidScopeWithoutWriting(t *testing.T) {
	configRoot := t.TempDir()
	repository := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	path := filepath.Join(repository, ".reviewparty", "config.json")
	writeTestFile(t, testFile{path: path, contents: `{"schema_version":1,"unexpected":true}`})

	result := runConfigCommand(t, []string{
		"config", "validate", "--scope", "repository", "--repo", repository, "--format", "json",
	})
	requireValidationFailure(t, result, path)
	if _, err := os.Stat(filepath.Join(configRoot, "review-party", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("validation created Global configuration: %v", err)
	}
}

func TestConfigValidateReportsAbsentFilesInJSONWithoutWriting(t *testing.T) {
	configRoot := t.TempDir()
	repository := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	result := runConfigCommand(t, []string{
		"config", "validate", "--repo", repository, "--format", "json",
	})
	requireCommandSuccess(t, result)
	var validation configurationValidationResult
	if err := json.Unmarshal([]byte(result.stdout), &validation); err != nil {
		t.Fatalf("validation output = %q: %v", result.stdout, err)
	}
	requireValidationScopes(t, validation.Scopes, []string{"global", "repository"})
	want := []configurationFileStatus{
		{Scope: "global", Path: filepath.Join(configRoot, "review-party", "config.json")},
		{Scope: "repository", Path: filepath.Join(repository, ".reviewparty", "config.json")},
	}
	requireValidationFiles(t, validation.Files, want)
	for _, file := range want {
		requireFileAbsent(t, file.Path)
	}
}

func TestConfigValidateReportsAbsentFilesInHumanOutput(t *testing.T) {
	configRoot := t.TempDir()
	repository := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	result := runConfigCommand(t, []string{
		"config", "validate", "--repo", repository, "--format", "human",
	})
	requireCommandSuccess(t, result)
	for _, want := range []string{
		"valid configuration: global, repository",
		"global: " + filepath.Join(configRoot, "review-party", "config.json") + " (absent)",
		"repository: " + filepath.Join(repository, ".reviewparty", "config.json") + " (absent)",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("human validation output = %q, missing %q", result.stdout, want)
		}
	}
}

func TestConfigMutationRequiresYesAndPublishesPlan(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	arguments := []string{
		"config", "profile", "create", "bugs", "--template", "bugs", "--reviewer", "grok",
		"--model", "grok-4.5", "--effort", "high", "--deadline", "1m",
	}
	mutation := profileMutationCase{
		arguments:  arguments,
		profileDir: filepath.Join(configRoot, "review-party", "profiles", "bugs"),
	}
	requireUnconfirmedMutation(t, mutation)
	requirePublishedProfile(t, mutation)
}

func TestConfigPartyAndReviewSelectionCommandsUseSemanticPlans(t *testing.T) {
	configRoot := t.TempDir()
	repository := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	requireConfigSuccess(t, []string{
		"config", "profile", "create", "bugs", "--blank", "--instructions", "Find bugs.\n", "--reviewer", "grok",
		"--model", "grok-4.5", "--effort", "high", "--deadline", "1m", "--yes", "--format", "json",
	})
	requireConfigSuccess(t, []string{
		"config", "party", "create", "baseline", "--scope", "global", "--profile", "global:bugs",
		"--concurrency-limit", "2", "--yes", "--format", "json",
	})
	requireConfigSuccess(t, []string{
		"config", "reviews", "add", "--scope", "global", "--party", "baseline", "--repo", repository,
		"--yes", "--format", "json",
	})
	requireConfigSuccess(t, []string{
		"config", "reviews", "set-concurrency", "3", "--repo", repository, "--yes", "--format", "json",
	})

	result := runConfigCommand(t, []string{"config", "show", "--repo", repository, "--format", "json"})
	requireCommandSuccess(t, result)
	var report struct {
		Reviews struct {
			ConcurrencyLimit int `json:"concurrency_limit"`
			Expanded         []struct {
				Profile string `json:"profile"`
			} `json:"expanded"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
		t.Fatalf("config show output = %q: %v", result.stdout, err)
	}
	if report.Reviews.ConcurrencyLimit != 3 {
		t.Fatalf("concurrency limit = %d", report.Reviews.ConcurrencyLimit)
	}
	if len(report.Reviews.Expanded) != 1 {
		t.Fatalf("expanded reviews = %#v", report.Reviews.Expanded)
	}
	if report.Reviews.Expanded[0].Profile != "bugs" {
		t.Fatalf("reviews = %#v", report.Reviews)
	}
}

func TestConfigMutationCommandsExposeDocumentedFlags(t *testing.T) {
	root := newRootCommand(productionCommandIO(bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{}))
	tests := []struct {
		path  []string
		flags []string
	}{
		{path: []string{"config", "profile", "create"}, flags: []string{"repo", "format", "config", "yes"}},
		{path: []string{"config", "profile", "copy"}, flags: []string{"repo", "format", "config", "yes", "target-scope"}},
		{path: []string{"config", "party", "create"}, flags: []string{"repo", "format", "config", "yes"}},
		{path: []string{"config", "reviews", "add"}, flags: []string{"scope", "profile", "party", "repo", "format", "config", "yes"}},
		{path: []string{"config", "reviews", "remove"}, flags: []string{"scope", "profile", "party", "index", "repo", "format", "config", "yes"}},
		{path: []string{"config", "reviews", "move"}, flags: []string{"scope", "from", "to", "repo", "format", "config", "yes"}},
		{path: []string{"config", "reviews", "set-concurrency"}, flags: []string{"repo", "format", "config", "yes"}},
	}
	for _, test := range tests {
		requireCommandFlags(t, findCommand(t, root, test.path), test.path, test.flags)
	}
	partyCommand := findCommand(t, root, []string{"config", "party", "create"})
	if partyCommand.Flags().Lookup("concurrency") != nil {
		t.Fatal("party create exposes the undocumented --concurrency alias")
	}
}

func writeTestFile(t *testing.T, file testFile) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file.path, []byte(file.contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireValidationFailure(t *testing.T, command configCommandResult, path string) {
	t.Helper()
	if command.exitCode == 0 {
		t.Fatalf("config validate succeeded, output = %q", command.stdout)
	}
	if command.stderr != "" {
		t.Fatalf("config validate wrote stderr = %q", command.stderr)
	}
	var result configurationValidationResult
	if err := json.Unmarshal([]byte(command.stdout), &result); err != nil {
		t.Fatalf("validation output = %q: %v", command.stdout, err)
	}
	if result.Valid {
		t.Fatalf("validation unexpectedly succeeded: %#v", result)
	}
	if !strings.Contains(result.Error, path) {
		t.Fatalf("validation result = %#v", result)
	}
	requireValidationFileStatus(t, result.Files, path)
}

func requireValidationFileStatus(t *testing.T, files []configurationFileStatus, path string) {
	t.Helper()
	if len(files) != 1 {
		t.Fatalf("validation file status = %#v", files)
	}
	if files[0].Path != path {
		t.Fatalf("validation file path = %q, want %q", files[0].Path, path)
	}
	if !files[0].Present {
		t.Fatalf("validation file was not reported present: %#v", files[0])
	}
}

func requireValidationScopes(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("validation scopes = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("validation scope[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func requireValidationFiles(t *testing.T, got, want []configurationFileStatus) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("validation files = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index].Scope != want[index].Scope {
			t.Fatalf("validation file[%d] scope = %q, want %q", index, got[index].Scope, want[index].Scope)
		}
		if got[index].Path != want[index].Path {
			t.Fatalf("validation file[%d] path = %q, want %q", index, got[index].Path, want[index].Path)
		}
		if got[index].Present != want[index].Present {
			t.Fatalf("validation file[%d] present = %t, want %t", index, got[index].Present, want[index].Present)
		}
	}
}

func requireFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("validation created %q: %v", path, err)
	}
}

func requireUnconfirmedMutation(t *testing.T, mutation profileMutationCase) {
	t.Helper()
	result := runConfigCommand(t, mutation.arguments)
	if result.exitCode == 0 {
		t.Fatalf("unconfirmed mutation succeeded")
	}
	if !strings.Contains(result.stderr, "--yes") {
		t.Fatalf("unconfirmed mutation exit = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if _, err := os.Stat(mutation.profileDir); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed mutation created Profile: %v", err)
	}
}

func requirePublishedProfile(t *testing.T, mutation profileMutationCase) {
	t.Helper()
	arguments := append(append([]string(nil), mutation.arguments...), "--yes", "--format", "json")
	result := runConfigCommand(t, arguments)
	requireCommandSuccess(t, result)
	var plan configurationPlanResult
	if err := json.Unmarshal([]byte(result.stdout), &plan); err != nil {
		t.Fatalf("plan output = %q: %v", result.stdout, err)
	}
	if !plan.Valid {
		t.Fatalf("plan is invalid: %#v", plan)
	}
	if !plan.Published {
		t.Fatalf("plan was not published: %#v", plan)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := os.Stat(filepath.Join(mutation.profileDir, "profile.json")); err != nil {
		t.Fatalf("confirmed mutation did not publish Profile: %v", err)
	}
}

func findCommand(t *testing.T, root *cobra.Command, path []string) *cobra.Command {
	t.Helper()
	command, _, err := root.Find(path)
	if err != nil {
		t.Fatalf("find %v: %v", path, err)
	}
	return command
}

func requireCommandFlags(t *testing.T, command *cobra.Command, path, flags []string) {
	t.Helper()
	for _, flag := range flags {
		if command.Flags().Lookup(flag) == nil {
			t.Errorf("%v does not expose --%s", path, flag)
		}
	}
}

func runConfigCommand(t *testing.T, arguments []string) configCommandResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), arguments, &stdout, &stderr)
	return configCommandResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exit}
}

func requireConfigSuccess(t *testing.T, arguments []string) {
	t.Helper()
	requireCommandSuccess(t, runConfigCommand(t, arguments))
}

func requireCommandSuccess(t *testing.T, result configCommandResult) {
	t.Helper()
	if result.exitCode != 0 {
		t.Fatalf("command exit = %d, stderr = %q, output = %q", result.exitCode, result.stderr, result.stdout)
	}
	if result.stderr != "" {
		t.Fatalf("command wrote stderr = %q", result.stderr)
	}
}
