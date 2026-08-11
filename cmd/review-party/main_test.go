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
	"time"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func TestPrintRecordUsesActualAttemptProvenance(t *testing.T) {
	record := model.ReviewRecord{
		Lifecycle:       model.LifecycleCompleted,
		ProfileRevision: model.ProfileRevision{ReviewerID: "copilot", Model: "auto", Effort: "auto"},
		Passes: []model.PassRecord{{Attempts: []model.AttemptRecord{{
			Provenance: model.ReviewerProvenance{ReviewerID: "copilot", Model: "gpt-5-mini", Effort: "high"},
		}}}},
	}
	var output bytes.Buffer
	if err := printRecord(&output, record, "human"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "copilot/gpt-5-mini (high)") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestHistoryUsesManagedXDGState(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	ledger, err := store.NewLedgerRecordStore(filepath.Join(stateHome, "review-party"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	record := model.ReviewRecord{
		SchemaVersion: model.CurrentReviewRecordSchemaVersion,
		ID:            "rp_1723200000000_0123456789abcdef",
		Lifecycle:     model.LifecycleCompleted,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ledger.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), []string{"history", "--format", "json"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), string(record.ID)) {
		t.Fatalf("history = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(stateHome, "review-party", "ledger.sqlite")); err != nil {
		t.Fatal(err)
	}
}

func TestInspectAndHistoryUseExplicitConfigurationState(t *testing.T) {
	stateDirectory := t.TempDir()
	configurationPath := filepath.Join(t.TempDir(), "review party.json")
	runMainCommand(t, []string{"init", "--repo", testGitRepository(t), "--state-dir", stateDirectory, "--config", configurationPath})
	ledger, err := store.NewLedgerRecordStore(stateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	record := model.ReviewRecord{SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: "rp_1723200000000_0123456789abcdef", Lifecycle: model.LifecycleCompleted, CreatedAt: now, UpdatedAt: now}
	if err := ledger.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}

	history := runMainCommand(t, []string{"history", "--format", "json", "--config", configurationPath})
	if !strings.Contains(history, string(record.ID)) {
		t.Fatalf("history = %q", history)
	}
	inspection := runMainCommand(t, []string{"inspect", string(record.ID), "--config", configurationPath})
	if !strings.Contains(inspection, "--config "+shellQuoteArgument(configurationPath)) {
		t.Fatalf("inspection = %q", inspection)
	}
}

func testGitRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	command := exec.Command("git", "-C", repository, "init", "--quiet")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	return repository
}

func runMainCommand(t *testing.T, arguments []string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), arguments, &stdout, &stderr); exit != 0 {
		t.Fatalf("run(%v) exit = %d, stderr = %q", arguments, exit, stderr.String())
	}
	return stdout.String()
}

func TestHistoryRejectsRemovedRecordsFlag(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(context.Background(), []string{"history", "--records", t.TempDir()}, &stdout, &stderr); exit != 2 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -records") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestReviewRequiresCompleteCommittedRange(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"review", "bugs", "--base", "HEAD~1"}, &stdout, &stderr); exit != 2 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--base and --head must be provided together") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHistoryParsesFiltersAndRendersEquivalentSummaries(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	repository := testGitRepository(t)
	ledger, err := store.NewLedgerRecordStore(filepath.Join(stateHome, "review-party"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	record := model.ReviewRecord{SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: "rp_1723200000000_0123456789abcdef", Lifecycle: model.LifecycleIncomplete, Subject: model.ReviewSubject{Repository: repository, Identity: "subject-one"}, ProfileRevision: model.ProfileRevision{Name: "bugs", ReviewerID: "opencode"}, Termination: &model.ReviewTermination{Category: model.TerminationDeadlineExceeded}, CreatedAt: now, UpdatedAt: now}
	if err := ledger.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	arguments := []string{"history", "--repo", filepath.Join(repository, "."), "--reviewer", "opencode", "--profile", "bugs", "--lifecycle", "incomplete", "--termination", "deadline_exceeded", "--subject", "subject-one", "--since", now.Add(-time.Second).Format(time.RFC3339)}
	human := runMainCommand(t, arguments)
	jsonOutput := runMainCommand(t, append(arguments, "--format", "json"))
	var page store.HistoryPage
	if err := json.Unmarshal([]byte(jsonOutput), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("JSON = %s", jsonOutput)
	}
	entry := page.Entries[0]
	for _, value := range []string{string(entry.ID), entry.Reviewer, string(entry.Termination)} {
		if !strings.Contains(human, value) {
			t.Fatalf("human = %q, missing %q", human, value)
		}
	}
}

func TestUsageListsEverySupportedReviewer(t *testing.T) {
	var output bytes.Buffer
	printUsage(&output)
	for _, reviewer := range engine.SupportedReviewers() {
		if !strings.Contains(output.String(), reviewer) {
			t.Fatalf("usage omits supported reviewer %q:\n%s", reviewer, output.String())
		}
	}
}

func TestPrintRecordJSONIncludesStructuredFindings(t *testing.T) {
	record := model.ReviewRecord{Result: &model.ReviewResult{
		Status: model.ResultFindings,
		Findings: []model.Finding{{
			Ordinal:  1,
			Severity: "HIGH",
			Category: "correctness",
			Location: "review.go:3",
			Failure:  "The changed state is not handled.",
			Evidence: "The Subject changes the state without updating its caller.",
			Fix:      "Update the caller with the state change.",
			Test:     "Exercise the caller with the changed state.",
		}},
	}}
	var output bytes.Buffer
	if err := printRecord(&output, record, "json"); err != nil {
		t.Fatal(err)
	}
	var inspected model.ReviewRecord
	if err := json.Unmarshal(output.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.Result == nil || len(inspected.Result.Findings) != 1 || inspected.Result.Findings[0].Evidence != record.Result.Findings[0].Evidence {
		t.Fatalf("inspected result = %#v, want structured finding", inspected.Result)
	}
}

func TestPrintRecordListsArtifactReferencesWithoutContents(t *testing.T) {
	record := model.ReviewRecord{Passes: []model.PassRecord{{Attempts: []model.AttemptRecord{{Artifacts: []model.ArtifactReference{{
		Kind: "assistant-text", Path: "artifacts/rp_1/1/assistant-text.txt", Size: 12, Digest: "digest",
	}}}}}}}
	var output bytes.Buffer
	if err := printRecord(&output, record, "human"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "artifact: assistant-text · artifacts/rp_1/1/assistant-text.txt · 12 bytes · sha256:digest") {
		t.Fatalf("output = %q, want artifact reference", output.String())
	}
}
