package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	repository := t.TempDir()
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	return repository
}
