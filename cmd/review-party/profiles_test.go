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

func TestProfilesCommandListsOnlySavedProfiles(t *testing.T) {
	repository := isolatedProfilesRepository(t)
	directory := filepath.Join(repository, ".reviewparty", "profiles", "security")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := `{"schema_version":1,"name":"security","reviewer":"grok","model":"grok-4.5","reasoning_effort":"high","attempt_deadline":"1m"}`
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "instructions.md"), []byte("Review security.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"profiles", "--repo", repository, "--format", "json"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	var profiles []model.ProfileSummary
	if err := json.Unmarshal(stdout.Bytes(), &profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "security" {
		t.Fatalf("profiles = %#v", profiles)
	}
}

func isolatedProfilesRepository(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repository := t.TempDir()
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	return repository
}

func writeRepositoryBugsProfile(t *testing.T, repository string) {
	t.Helper()
	directory := filepath.Join(repository, ".reviewparty", "profiles", "bugs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := `{"schema_version":1,"name":"bugs","reviewer":"grok","model":"grok-4.5","reasoning_effort":"high","attempt_deadline":"1m"}`
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "instructions.md"), []byte("Review bugs.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func createGlobalBugsProfile(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requireConfigSuccess(t, []string{
		"config", "profile", "create", "bugs", "--blank", "--instructions", "Find bugs.\n", "--reviewer", "grok",
		"--model", "grok-4.5", "--effort", "high", "--deadline", "1m", "--yes", "--format", "json",
	})
}

func explainedProfileSource(t *testing.T, arguments ...string) string {
	t.Helper()
	result := runConfigCommand(t, append(arguments, "--format", "json"))
	requireCommandSuccess(t, result)
	var explanation model.ProfileExplanation
	if err := json.Unmarshal([]byte(result.stdout), &explanation); err != nil {
		t.Fatalf("explain output = %q: %v", result.stdout, err)
	}
	return explanation.ProfileRevision.Source
}

func TestExplainResolvesScopePrefixedProfileNames(t *testing.T) {
	repository := isolatedProfilesRepository(t)
	createGlobalBugsProfile(t)
	writeRepositoryBugsProfile(t, repository)
	tests := []struct {
		arguments []string
		source    string
	}{
		{[]string{"explain", "bugs", "--repo", repository}, "repository:.reviewparty/profiles/bugs"},
		{[]string{"explain", "global:bugs", "--repo", repository}, "global:profiles/bugs"},
		{[]string{"explain", "repository:bugs", "--repo", repository}, "repository:.reviewparty/profiles/bugs"},
		{[]string{"profile", "explain", "global:bugs", "--repo", repository}, "global:profiles/bugs"},
	}
	for _, test := range tests {
		if source := explainedProfileSource(t, test.arguments...); source != test.source {
			t.Fatalf("%v source = %q, want %q", test.arguments, source, test.source)
		}
	}
}

func TestExplainScopedMissingProfileNamesTheScope(t *testing.T) {
	repository := isolatedProfilesRepository(t)
	createGlobalBugsProfile(t)
	result := runConfigCommand(t, []string{"explain", "repository:bugs", "--repo", repository})
	if result.exitCode == 0 {
		t.Fatalf("explain repository:bugs succeeded: %q", result.stdout)
	}
	if !strings.Contains(result.stderr, "in repository Configuration") || strings.Contains(result.stderr, "must match") {
		t.Fatalf("stderr = %q", result.stderr)
	}
}

func TestExplainRejectsMalformedScopedNames(t *testing.T) {
	repository := isolatedProfilesRepository(t)
	result := runConfigCommand(t, []string{"explain", "global:Bad_Name", "--repo", repository})
	if result.exitCode == 0 || !strings.Contains(result.stderr, "must match") {
		t.Fatalf("exit = %d, stderr = %q", result.exitCode, result.stderr)
	}
}
