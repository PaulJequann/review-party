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

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func TestPrintRecordUsesActualAttemptProvenance(t *testing.T) {
	record := model.ReviewRecord{
		ID: "rp_provenance", Lifecycle: model.LifecycleCompleted,
		ProfileRevision: model.ProfileRevision{ReviewerID: "copilot", Model: "auto", Effort: "auto"},
		Passes: []model.PassRecord{{Attempts: []model.AttemptRecord{{
			Provenance: model.ReviewerProvenance{ReviewerID: "copilot", Model: "gpt-5-mini", Effort: "high"},
		}}}},
	}
	if output := renderReport(t, recordReport(record, false), "human"); !strings.Contains(output, "reviewer: copilot/gpt-5-mini (high)") {
		t.Fatalf("output = %q", output)
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
	if !strings.Contains(stderr.String(), "unknown flag: --records") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunRequiresCompleteCommittedRange(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"run", "--base", "HEAD~1"}, &stdout, &stderr); exit != 2 {
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

func TestRunHelpOmitsExecutionOverrides(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"run", "--help"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	for _, flag := range []string{"--reviewer", "--model", "--effort", "--deadline"} {
		if strings.Contains(stdout.String(), flag) {
			t.Fatalf("help includes retired override %q:\n%s", flag, stdout.String())
		}
	}
}

func TestEvalUserConfigurationControlsRuntimeDefaultsAndFlagsOverride(t *testing.T) {
	configuration := filepath.Join(t.TempDir(), "review-party.json")
	if err := os.WriteFile(configuration, []byte(`{"schema_version":1,"defaults":{"reviewer":"opencode"},"reviewers":{"opencode":{"model":"configured-model"}},"eval":{"retry_policy":{"max_attempts":4,"initial_backoff":"2s","max_backoff":"20s"},"concurrency_limit":3}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	experiment, _, err := resolveEvalExperiment(evalRunOptions{configuration: configuration, attempts: 2, overrides: map[string]bool{"attempts": true}})
	if err != nil {
		t.Fatal(err)
	}
	assertEvalSelectionDefaults(t, experiment)
}

func assertEvalSelectionDefaults(t *testing.T, experiment model.ExperimentConfiguration) {
	t.Helper()
	checks := map[string]bool{
		"reviewer": experiment.Reviewer == "opencode", "model": experiment.Model == "configured-model",
		"attempts": experiment.RetryPolicy.MaxAttempts == 2, "backoff": experiment.RetryPolicy.InitialBackoff == "2s",
		"concurrency": experiment.ConcurrencyLimit == 3,
	}
	for name, valid := range checks {
		if !valid {
			t.Fatalf("%s missing from experiment %#v", name, experiment)
		}
	}
}

func TestEvalInspectReadsSuiteAndCaseRecords(t *testing.T) {
	configurationPath, suite, evalRun := preparePendingEvalInspection(t)
	for _, id := range []string{string(suite.ID), string(evalRun.ID)} {
		output := runMainCommand(t, []string{"eval", "inspect", id, "--config", configurationPath, "--format", "json"})
		if !strings.Contains(output, id) {
			t.Fatalf("inspection = %s", output)
		}
	}
	assertPendingEvalHumanInspection(t, configurationPath, evalRun.ID)
	assertEvalUpdatedAtInspection(t, configurationPath, evalRun.ID)
}

func preparePendingEvalInspection(t *testing.T) (string, model.EvalSuiteRun, model.EvalRun) {
	t.Helper()
	stateDirectory := t.TempDir()
	configurationPath := filepath.Join(t.TempDir(), "review-party.json")
	runMainCommand(t, []string{"init", "--repo", testGitRepository(t), "--state-dir", stateDirectory, "--config", configurationPath})
	ledger, err := store.NewLedgerRecordStore(stateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	review := model.ReviewRecord{SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: "rp_1723200000000_0123456789abcdef", Lifecycle: model.LifecycleCompleted, CreatedAt: now, UpdatedAt: now}
	if err := ledger.Save(review); err != nil {
		t.Fatal(err)
	}
	suite := model.EvalSuiteRun{ID: "esr_1723200000000_0123456789abcdef", Suite: "global:general-bugs", SuiteRevision: "v1", SuiteDigest: "digest", Experiment: model.ExperimentConfiguration{Profile: "bugs", Reviewer: "opencode", Model: "model", Deadline: "1m"}, Lifecycle: model.LifecyclePending, EvalRunIDs: []model.EvalRunID{"er_1723200000000_0123456789abcdef"}, StartedAt: now}
	evalRun := model.EvalRun{ID: suite.EvalRunIDs[0], SuiteRunID: suite.ID, Case: model.EvalCaseRevision{ID: "case-one", SchemaVersion: 1, Digest: "case-digest"}, ExecutionState: model.EvalPending, AdjudicationState: model.EvalAdjudicationNotReady, CreatedAt: now, UpdatedAt: now}
	if err := ledger.CreateEvalSuiteRun(suite, []model.EvalRun{evalRun}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	return configurationPath, suite, evalRun
}

func assertPendingEvalHumanInspection(t *testing.T, configurationPath string, id model.EvalRunID) {
	t.Helper()
	human := runMainCommand(t, []string{"eval", "inspect", string(id), "--config", configurationPath})
	if !strings.Contains(human, "review not started") {
		t.Fatalf("inspection = %s", human)
	}
}

func assertEvalUpdatedAtInspection(t *testing.T, configurationPath string, id model.EvalRunID) {
	t.Helper()
	encoded := runMainCommand(t, []string{"eval", "inspect", string(id), "--config", configurationPath, "--format", "json"})
	var inspected model.EvalRun
	if err := json.Unmarshal([]byte(encoded), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.UpdatedAt.IsZero() {
		t.Fatal("inspection omitted updated_at")
	}
}

func TestEvalAdjudicationExportAndScoreRoundTrip(t *testing.T) {
	configurationPath, suite := prepareAdjudicationCommandTest(t)
	document := exportAdjudicationForTest(t, configurationPath, suite.ID)
	markFirstFindingMatched(&document)
	path := writeAdjudicationForTest(t, document)
	scored := runMainCommand(t, []string{"eval", "score", string(suite.ID), "--adjudication", path, "--config", configurationPath, "--format", "json"})
	var revision model.AdjudicationRevision
	if err := json.Unmarshal([]byte(scored), &revision); err != nil {
		t.Fatal(err)
	}
	if revision.Score.DefectRecall.Numerator != 1 || revision.Score.FindingPrecision.Numerator != 1 {
		t.Fatalf("revision = %#v", revision)
	}
	inspected := runMainCommand(t, []string{"eval", "inspect", string(revision.ID), "--config", configurationPath, "--format", "json"})
	if !strings.Contains(inspected, string(revision.ID)) {
		t.Fatalf("inspection = %s", inspected)
	}
}

func TestEvalCompareJSONRoundTrip(t *testing.T) {
	configurationPath, baseline, candidate := setupComparisonCommand(t)
	output := runMainCommand(t, []string{"eval", "compare", "--baseline", string(baseline.ID), "--candidate", string(candidate.ID), "--config", configurationPath, "--format", "json"})
	var comparison model.EvalComparison
	if err := json.Unmarshal([]byte(output), &comparison); err != nil {
		t.Fatal(err)
	}
	if comparison.Coverage.ComparedCases != 1 {
		t.Fatalf("comparison coverage = %#v", comparison.Coverage)
	}
	if comparison.BaselineAdjudication != baseline.ID || comparison.CandidateAdjudication != candidate.ID {
		t.Fatalf("comparison = %#v", comparison)
	}
}

func setupComparisonCommand(t *testing.T) (string, model.AdjudicationRevision, model.AdjudicationRevision) {
	t.Helper()
	stateDirectory := t.TempDir()
	configurationPath := filepath.Join(t.TempDir(), "review-party.json")
	runMainCommand(t, []string{"init", "--repo", testGitRepository(t), "--state-dir", stateDirectory, "--config", configurationPath})
	ledger := mustComparisonLedger(t, stateDirectory)
	now := time.Now().UTC()
	baselineSuite := comparisonCommandSuite("esr_1723200000000_0123456789abcdef", "er_1723200000000_0123456789abcdef", now)
	candidateSuite := comparisonCommandSuite("esr_1723200000001_0123456789abcdef", "er_1723200000001_0123456789abcdef", now.Add(time.Second))
	mustSaveComparisonData(t, comparisonCommandData{ledger: ledger, baselineSuite: baselineSuite, candidateSuite: candidateSuite, now: now})
	baseline := mustPublishComparison(t, ledger, model.AdjudicationRevision{ID: "ar_1723200000000_0123456789abcdef", SuiteRunID: baselineSuite.ID, Document: comparisonCommandDocument(baselineSuite, true), CreatedAt: now})
	candidate := mustPublishComparison(t, ledger, model.AdjudicationRevision{ID: "ar_1723200000001_0123456789abcdef", SuiteRunID: candidateSuite.ID, Document: comparisonCommandDocument(candidateSuite, false), CreatedAt: now.Add(time.Second)})
	mustCloseComparison(t, ledger)
	return configurationPath, baseline, candidate
}

func mustComparisonLedger(t *testing.T, stateDirectory string) *store.LedgerRecordStore {
	t.Helper()
	ledger, err := store.NewLedgerRecordStore(stateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

type comparisonCommandData struct {
	ledger                        *store.LedgerRecordStore
	baselineSuite, candidateSuite model.EvalSuiteRun
	now                           time.Time
}

func mustSaveComparisonData(t *testing.T, data comparisonCommandData) {
	t.Helper()
	for _, review := range []model.ReviewRecord{comparisonCommandReview("rp_1723200000000_0123456789abcdef", data.now, 100), comparisonCommandReview("rp_1723200000001_0123456789abcdef", data.now.Add(time.Second), 120)} {
		mustSaveComparisonReview(t, data.ledger, review)
	}
	mustSaveComparisonSuite(t, data.ledger, data.baselineSuite, comparisonCommandEvalRun(data.baselineSuite, "rp_1723200000000_0123456789abcdef", data.now))
	mustSaveComparisonSuite(t, data.ledger, data.candidateSuite, comparisonCommandEvalRun(data.candidateSuite, "rp_1723200000001_0123456789abcdef", data.now.Add(time.Second)))
}

func mustSaveComparisonReview(t *testing.T, ledger *store.LedgerRecordStore, review model.ReviewRecord) {
	t.Helper()
	if err := ledger.Save(review); err != nil {
		t.Fatal(err)
	}
}

func mustSaveComparisonSuite(t *testing.T, ledger *store.LedgerRecordStore, suite model.EvalSuiteRun, run model.EvalRun) {
	t.Helper()
	if err := ledger.CreateEvalSuiteRun(suite, []model.EvalRun{run}); err != nil {
		t.Fatal(err)
	}
}

func mustPublishComparison(t *testing.T, ledger *store.LedgerRecordStore, revision model.AdjudicationRevision) model.AdjudicationRevision {
	t.Helper()
	published, err := ledger.PublishAdjudication(revision)
	if err != nil {
		t.Fatal(err)
	}
	return published
}

func mustCloseComparison(t *testing.T, ledger *store.LedgerRecordStore) {
	t.Helper()
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
}

func comparisonCommandReview(id string, created time.Time, totalMS int64) model.ReviewRecord {
	return model.ReviewRecord{SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: model.ReviewID(id), Lifecycle: model.LifecycleCompleted, Subject: model.ReviewSubject{Kind: model.SubjectCapturedChange, Identity: id}, ProfileRevision: model.ProfileRevision{Name: "bugs", Revision: "v1", ReviewerID: "opencode", Model: "model", Effort: "high", Reviewer: model.ReviewerProvenance{ReviewerID: "opencode", Model: "model", Effort: "high", Harness: "opencode-cli", Transport: "direct-cli"}}, ProfileSnapshot: model.ProfileSnapshot{Name: "bugs"}, Runtime: &model.RuntimeProvenance{Version: "v1", VCSRevision: "build", VCSModified: boolPointer(false)}, Timings: &model.ReviewTimings{TotalMS: totalMS}, Result: &model.ReviewResult{Status: model.ResultFindings, Findings: []model.Finding{}}, CreatedAt: created, UpdatedAt: created}
}

func boolPointer(value bool) *bool {
	return &value
}

func comparisonCommandSuite(id string, evalID model.EvalRunID, started time.Time) model.EvalSuiteRun {
	return model.EvalSuiteRun{ID: model.EvalSuiteRunID(id), Suite: "suite", SuiteRevision: "v1", SuiteDigest: "digest", Experiment: model.ExperimentConfiguration{Profile: "bugs", Reviewer: "opencode", Model: "model", Effort: "high", Deadline: "1m"}, EvalRunIDs: []model.EvalRunID{evalID}, StartedAt: started, CompletedAt: started.Add(time.Second)}
}

func comparisonCommandEvalRun(suite model.EvalSuiteRun, reviewID model.ReviewID, created time.Time) model.EvalRun {
	return model.EvalRun{ID: suite.EvalRunIDs[0], SuiteRunID: suite.ID, Case: model.EvalCaseRevision{ID: "shared-case", SchemaVersion: 1, Digest: "shared-digest", Classification: "defect", ExpectedFindings: []model.ExpectedFinding{{ID: "bug", Behavior: "behavior", Impact: "impact", Evidence: []string{"evidence"}}}}, ReviewID: reviewID, ExecutionState: model.EvalCompletedFindings, AdjudicationState: "scored", CreatedAt: created}
}

func comparisonCommandDocument(suite model.EvalSuiteRun, matched bool) model.AdjudicationDocument {
	disposition := model.ExpectedMissed
	if matched {
		disposition = model.ExpectedMatched
	}
	reported := []model.ReportedFindingAdjudication{}
	if matched {
		reported = append(reported, model.ReportedFindingAdjudication{Finding: model.Finding{Ordinal: 1}, Disposition: model.ReportedMatchedExpected, ExpectedFindingID: "bug"})
	}
	return model.AdjudicationDocument{SchemaVersion: 1, SuiteRunID: suite.ID, Cases: []model.EvalCaseAdjudication{{EvalRunID: suite.EvalRunIDs[0], CaseID: "shared-case", ExecutionState: model.EvalCompletedFindings, ExpectedFindings: []model.ExpectedFindingAdjudication{{Finding: model.ExpectedFinding{ID: "bug", Behavior: "behavior", Impact: "impact", Evidence: []string{"evidence"}}, Disposition: disposition, ReportedOrdinal: func() int {
		if matched {
			return 1
		}
		return 0
	}()}}, ReportedFindings: reported}}}
}

func prepareAdjudicationCommandTest(t *testing.T) (string, model.EvalSuiteRun) {
	t.Helper()
	stateDirectory := t.TempDir()
	configurationPath := filepath.Join(t.TempDir(), "review-party.json")
	runMainCommand(t, []string{"init", "--repo", testGitRepository(t), "--state-dir", stateDirectory, "--config", configurationPath})
	ledger, err := store.NewLedgerRecordStore(stateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	review := model.ReviewRecord{SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: "rp_1723200000000_0123456789abcdef", Lifecycle: model.LifecycleCompleted, Result: &model.ReviewResult{Status: model.ResultFindings, Findings: []model.Finding{{Ordinal: 1, Failure: "failure"}}}, CreatedAt: now, UpdatedAt: now}
	if err := ledger.Save(review); err != nil {
		t.Fatal(err)
	}
	suite := model.EvalSuiteRun{ID: "esr_1723200000000_0123456789abcdef", Suite: "suite", SuiteRevision: "v1", SuiteDigest: "digest", EvalRunIDs: []model.EvalRunID{"er_1723200000000_0123456789abcdef"}, StartedAt: now}
	evalRun := model.EvalRun{ID: suite.EvalRunIDs[0], SuiteRunID: suite.ID, Case: model.EvalCaseRevision{ID: "case", SchemaVersion: 1, Digest: "digest", ExpectedFindings: []model.ExpectedFinding{{ID: "bug", Behavior: "behavior", Impact: "impact", Evidence: []string{"evidence"}}}}, ReviewID: review.ID, ExecutionState: model.EvalCompletedFindings, AdjudicationState: "awaiting_adjudication", CreatedAt: now, UpdatedAt: now}
	if err := ledger.CreateEvalSuiteRun(suite, []model.EvalRun{evalRun}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	return configurationPath, suite
}

func exportAdjudicationForTest(t *testing.T, configurationPath string, suiteID model.EvalSuiteRunID) model.AdjudicationDocument {
	t.Helper()
	exported := runMainCommand(t, []string{"eval", "adjudication", "export", string(suiteID), "--config", configurationPath})
	var document model.AdjudicationDocument
	if err := json.Unmarshal([]byte(exported), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func markFirstFindingMatched(document *model.AdjudicationDocument) {
	document.Cases[0].ExpectedFindings[0].Disposition = model.ExpectedMatched
	document.Cases[0].ExpectedFindings[0].ReportedOrdinal = 1
	document.Cases[0].ReportedFindings[0].Disposition = model.ReportedMatchedExpected
	document.Cases[0].ReportedFindings[0].ExpectedFindingID = "bug"
}

func writeAdjudicationForTest(t *testing.T, document model.AdjudicationDocument) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adjudication.json")
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrintReportJSONIncludesStructuredFindings(t *testing.T) {
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
	var inspected reviewReport
	if err := json.Unmarshal([]byte(renderReport(t, recordReport(record, false), "json")), &inspected); err != nil {
		t.Fatal(err)
	}
	if len(inspected.Reviews) != 1 || len(inspected.Reviews[0].Findings) != 1 {
		t.Fatalf("reviews = %#v, want one review with one structured finding", inspected.Reviews)
	}
	if inspected.Reviews[0].Findings[0] != (reportFinding{Finding: record.Result.Findings[0]}) {
		t.Fatalf("finding = %#v, want %#v", inspected.Reviews[0].Findings[0], record.Result.Findings[0])
	}
}

func TestPrintFullReportListsArtifactReferencesWithoutContents(t *testing.T) {
	record := model.ReviewRecord{ID: "rp_artifacts", Passes: []model.PassRecord{{Attempts: []model.AttemptRecord{{Artifacts: []model.ArtifactReference{{
		Kind: "assistant-text", Path: "artifacts/rp_1/1/assistant-text.txt", Size: 12, Digest: "digest",
	}}}}}}}
	output := renderReport(t, recordReport(record, true), "human")
	if !strings.Contains(output, "artifact: assistant-text · artifacts/rp_1/1/assistant-text.txt · 12 bytes · sha256:digest\n") {
		t.Fatalf("output = %q, want artifact reference", output)
	}
}
