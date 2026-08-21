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
	"reviewparty/internal/store"
)

func TestPartiesListIncludesPackagedStandard(t *testing.T) {
	repository := t.TempDir()
	writeRepositoryFixture(t, repository)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), []string{"parties", "--repo", repository, "--format", "json"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	var summaries []model.PartySummary
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatal(err)
	}
	assertPackagedStandardSummary(t, summaries)
}

func assertPackagedStandardSummary(t *testing.T, summaries []model.PartySummary) {
	t.Helper()
	if len(summaries) != 1 {
		t.Fatalf("summaries = %#v, want exactly the packaged party", summaries)
	}
	standard := summaries[0]
	if standard.Name != "standard" || standard.Source != "packaged" {
		t.Fatalf("summary = %#v, want packaged standard", standard)
	}
	if len(standard.Members) != 3 {
		t.Fatalf("members = %#v, want three packaged members", standard.Members)
	}
}

func TestPartyRunPersistsInspectableIncompleteBundleWithoutInstalledReviewers(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	isolateReviewerBinaries(t)
	repository := t.TempDir()
	writeRepositoryFixture(t, repository)
	writePartyFixture(t, filepath.Join(repository, ".reviewparty", "parties", "duo.json"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), []string{"init", "--repo", repository}, &stdout, &stderr); exit != 0 {
		t.Fatalf("init exit = %d, stderr = %q", exit, stderr.String())
	}
	stdout.Reset()
	exit := run(context.Background(), []string{"party", "run", "duo", "--repo", repository, "--format", "json"}, &stdout, &stderr)
	if exit != 2 {
		t.Fatalf("exit = %d, stderr = %q, stdout = %q", exit, stderr.String(), stdout.String())
	}
	bundle := decodeBundle(t, stdout.Bytes())
	assertIncompleteBundle(t, bundle)
	assertBundleInspectionMatchesRun(t, stateHome, bundle)
}

func assertIncompleteBundle(t *testing.T, bundle model.ReviewBundle) {
	t.Helper()
	if bundle.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("bundle lifecycle = %s, want incomplete", bundle.Lifecycle)
	}
	if bundle.Party != "duo" {
		t.Fatalf("bundle party = %q, want duo", bundle.Party)
	}
	if bundle.PartyRevision == "" {
		t.Fatal("bundle party revision is empty")
	}
	if len(bundle.Members) != 2 {
		t.Fatalf("members = %#v, want every planned member visible", bundle.Members)
	}
}

func decodeBundle(t *testing.T, payload []byte) model.ReviewBundle {
	t.Helper()
	var bundle model.ReviewBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		t.Fatalf("decode bundle: %v\n%s", err, payload)
	}
	return bundle
}

func assertBundleInspectionMatchesRun(t *testing.T, stateHome string, bundle model.ReviewBundle) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), []string{"inspect", string(bundle.ID), "--format", "json"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", exit, stderr.String())
	}
	inspected := decodeBundle(t, stdout.Bytes())
	for _, member := range inspected.Members {
		assertIncompleteMemberRecord(t, member)
		record := readReviewRecord(t, stateHome, member.ReviewID)
		if record.Subject.Identity != inspected.SubjectIdentity {
			t.Fatalf("member %s subject identity drift", member.ReviewID)
		}
	}
}

func assertIncompleteMemberRecord(t *testing.T, member model.BundleMember) {
	t.Helper()
	if member.ReviewID != "" && member.Lifecycle == model.LifecycleIncomplete {
		return
	}
	t.Fatalf("member = %#v, want an inspectable honest incomplete child review", member)
}

// isolateReviewerBinaries keeps the CLI test hermetic: git stays reachable for
// Subject resolution while every Reviewer harness binary becomes unavailable,
// so the run exercises the honest Incomplete path instead of launching a live
// agent.
func isolateReviewerBinaries(t *testing.T) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binDirectory := t.TempDir()
	if err := os.Symlink(gitPath, filepath.Join(binDirectory, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDirectory)
}

func readReviewRecord(t *testing.T, stateHome string, id model.ReviewID) model.ReviewRecord {
	t.Helper()
	ledger, err := store.NewLedgerRecordStore(filepath.Join(stateHome, "review-party"))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	record, err := ledger.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func writeRepositoryFixture(t *testing.T, repository string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repository, ".reviewparty", "parties"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "review.go"), []byte("package demo\n\nconst state = \"changed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repository, "init", "--quiet")
	runGitCommand(t, repository, "config", "user.email", "review-party@example.invalid")
	runGitCommand(t, repository, "config", "user.name", "Review Party Test")
	runGitCommand(t, repository, "add", "-A")
	runGitCommand(t, repository, "commit", "--quiet", "-m", "base")
}

func writePartyFixture(t *testing.T, path string) {
	t.Helper()
	payload := `{"schema_version":1,"name":"duo","description":"test duo","profiles":[{"profile":"bugs"},{"profile":"code-quality"}]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGitCommand(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
