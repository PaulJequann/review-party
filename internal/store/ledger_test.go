package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestLedgerRoundTripsCompleteAndIncompleteReviews(t *testing.T) {
	store, err := NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clean := ledgerFixture(model.LifecycleCompleted)
	clean.Result = &model.ReviewResult{Status: model.ResultClean, Summary: "clean", Raw: "raw", Findings: []model.Finding{}}
	withoutAttempts := ledgerFixture(model.LifecycleIncomplete)
	withoutAttempts.ID = "rp_1723200000003_0123456789abcdef"
	withoutAttempts.Passes[0].Attempts = []model.AttemptRecord{}
	for _, record := range []model.ReviewRecord{ledgerFixture(model.LifecycleCompleted), ledgerFixture(model.LifecycleIncomplete), clean, withoutAttempts} {
		if err := store.Save(record); err != nil {
			t.Fatal(err)
		}
		loaded, err := store.Load(record.ID)
		if err != nil {
			t.Fatal(err)
		}
		gotJSON, err := json.Marshal(loaded)
		if err != nil {
			t.Fatal(err)
		}
		wantJSON, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("loaded = %s\nwant %s", gotJSON, wantJSON)
		}
	}
}

func TestLedgerPreparationIsIdempotent(t *testing.T) {
	directory := t.TempDir()
	record := ledgerFixture(model.LifecycleCompleted)
	store, err := NewLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	history, err := reopened.History(HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Entries) != 1 {
		t.Fatalf("history = %#v", history)
	}
}

func TestLedgerAggregateWriteRollsBackAfterChildFailure(t *testing.T) {
	store, err := NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	original := ledgerFixture(model.LifecycleCompleted)
	if err := store.Save(original); err != nil {
		t.Fatal(err)
	}
	invalid := original
	invalid.Lifecycle = model.LifecycleRunning
	invalid.Result = &model.ReviewResult{Status: model.ResultFindings, Findings: []model.Finding{{Ordinal: 1}, {Ordinal: 1}}}
	if err := store.Save(invalid); err == nil {
		t.Fatal("expected duplicate child ordinal to fail")
	}
	loaded, err := store.Load(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("aggregate changed after failed save\ngot  %s\nwant %s", got, want)
	}
}

func TestDeferredLedgerReadDoesNotCreateMissingState(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "missing", "review-party")
	store, err := NewDeferredLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.History(HistoryQuery{Limit: 10})
	if !errors.Is(err, ErrReviewRecordStateNotInitialized) {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(directory); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state directory was created: %v", statErr)
	}
}

func TestLedgerPreparationRejectsSymlinkedStateDirectory(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	directory := filepath.Join(root, "review-party")
	if err := os.Symlink(external, directory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := PrepareReviewRecordState(directory); err == nil || !strings.Contains(err.Error(), "must not contain symlinks") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(external, ledgerFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ledger was created through symlink: %v", err)
	}
}

func TestLedgerRejectsCorruptStateWithoutReplacingIt(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, ledgerFilename)
	want := []byte("not a sqlite database")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewDeferredLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.History(HistoryQuery{Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "is corrupt") {
		t.Fatalf("error = %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("corrupt state was modified: %q", got)
	}
}

func TestLedgerRestrictsManagedStatePermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "review-party")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, path := range []string{directory, filepath.Join(directory, ledgerFilename)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o700)
		if path != directory {
			want = 0o600
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("permissions for %s = %o, want %o", path, got, want)
		}
	}
}

func TestLedgerRejectsBlockedStatePathPrecisely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review-party")
	if err := os.WriteFile(path, []byte("blocks managed state"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewLedgerRecordStore(path)
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("error = %v", err)
	}
}

func TestLedgerHistoryBreaksTimestampTiesByReviewID(t *testing.T) {
	store, err := NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, second := ledgerFixture(model.LifecycleCompleted), ledgerFixture(model.LifecycleCompleted)
	first.ID, second.ID = "rp_1723200000000_aaaaaaaaaaaaaaaa", "rp_1723200000000_bbbbbbbbbbbbbbbb"
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	history, err := store.History(HistoryQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]model.ReviewID{history.Entries[0].ID, history.Entries[1].ID}, []model.ReviewID{second.ID, first.ID}) {
		t.Fatalf("history = %#v", history)
	}
}

func TestLedgerHistoryCombinesRecordedFilters(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer ledger.Close()
	matching := ledgerFixture(model.LifecycleIncomplete)
	matching.ID = "rp_1723200000000_aaaaaaaaaaaaaaaa"
	matching.Subject.Repository = "/repo/one"
	matching.Subject.Identity = "subject-one"
	matching.ProfileRevision.Name = "bugs"
	matching.ProfileRevision.ReviewerID = "opencode"
	other := matching
	other.ID = "rp_1723200000000_bbbbbbbbbbbbbbbb"
	other.ProfileRevision.ReviewerID = "grok"
	saveTestReviews(t, ledger, matching, other)
	since := matching.CreatedAt.Add(-time.Second)
	page := loadTestHistory(t, ledger, HistoryQuery{Repository: "/repo/one", Reviewer: "opencode", Profile: "bugs", Lifecycle: model.LifecycleIncomplete, Termination: model.TerminationDeadlineExceeded, Subject: "subject-one", Since: &since, Limit: 10})
	if len(page.Entries) != 1 {
		t.Fatalf("history = %#v, want one entry", page)
	}
	if page.Entries[0].ID != matching.ID {
		t.Fatalf("history = %#v, want %s", page, matching.ID)
	}
	entry := page.Entries[0]
	if entry.Reviewer != "opencode" {
		t.Fatalf("reviewer = %s", entry.Reviewer)
	}
	if entry.Profile != "bugs" {
		t.Fatalf("profile = %s", entry.Profile)
	}
	if entry.Termination != model.TerminationDeadlineExceeded {
		t.Fatalf("termination = %s", entry.Termination)
	}
}

func TestLedgerHistoryEnforcesLimitAndReportsMore(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer ledger.Close()
	first, second := ledgerFixture(model.LifecycleCompleted), ledgerFixture(model.LifecycleCompleted)
	second.ID = "rp_1723200000000_bbbbbbbbbbbbbbbb"
	saveTestReviews(t, ledger, first, second)
	page := loadTestHistory(t, ledger, HistoryQuery{Limit: 1})
	if len(page.Entries) != 1 {
		t.Fatalf("entries = %#v", page.Entries)
	}
	if !page.HasMore {
		t.Fatalf("page = %#v, want more", page)
	}
	if page.Limit != 1 {
		t.Fatalf("limit = %d", page.Limit)
	}
	if _, err := ledger.History(HistoryQuery{Limit: MaxHistoryLimit + 1}); err == nil {
		t.Fatal("expected excessive limit error")
	}
}

func TestLedgerHistoryReviewerQueryUsesApprovedIndex(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer ledger.Close()
	for index := 0; index < 32; index++ {
		record := ledgerFixture(model.LifecycleCompleted)
		record.ID = model.ReviewID(fmt.Sprintf("rp_1723200000000_%016x", index))
		record.ProfileRevision.ReviewerID = []string{"opencode", "grok"}[index%2]
		saveTestReviews(t, ledger, record)
	}
	details := historyReviewerQueryPlan(t, ledger)
	if !strings.Contains(strings.Join(details, "\n"), "reviews_history_reviewer") {
		t.Fatalf("query plan = %q, want reviewer history index", details)
	}
}

func historyReviewerQueryPlan(t *testing.T, ledger *LedgerRecordStore) []string {
	t.Helper()
	rows, err := ledger.db.Query(`EXPLAIN QUERY PLAN SELECT id FROM reviews
		WHERE json_extract(profile_revision,'$.reviewer_id') = ?
		ORDER BY created_at DESC,id DESC LIMIT ?`, "opencode", 20)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return details
}

func TestLedgerPreparationMigratesVersionOneHistoryState(t *testing.T) {
	directory := t.TempDir()
	ledger := newTestLedger(t, directory)
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(directory, ledgerFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE adjudication_revisions; DROP TABLE eval_runs; DROP TABLE eval_suite_runs; DROP INDEX IF EXISTS review_bundles_party_history; DROP TABLE IF EXISTS review_bundles; ALTER TABLE attempts DROP COLUMN retry_after_ms; DROP INDEX reviews_replay_source; ALTER TABLE reviews DROP COLUMN replays_review_id; DROP INDEX reviews_history_order; DROP INDEX reviews_history_reviewer; DELETE FROM schema_migrations WHERE version>=2"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	migrated := newTestLedger(t, directory)
	defer migrated.Close()
	var count int
	if err := migrated.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name LIKE 'reviews_history_%'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("history index count = %d, want 2", count)
	}
}

func TestAdjudicationCorrectionsPreserveImmutableRevisions(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer ledger.Close()
	now := time.Now().UTC()
	review := ledgerFixture(model.LifecycleCompleted)
	if err := ledger.Save(review); err != nil {
		t.Fatal(err)
	}
	suite := model.EvalSuiteRun{ID: "esr_1723200000000_0123456789abcdef", Suite: "suite", SuiteRevision: "v1", SuiteDigest: "digest", Experiment: model.ExperimentConfiguration{}, EvalRunIDs: []model.EvalRunID{}, StartedAt: now}
	if err := ledger.CreateEvalSuiteRun(suite, nil); err != nil {
		t.Fatal(err)
	}
	first := publishTestAdjudication(t, ledger, adjudicationFixture{"ar_1723200000000_0123456789abcdef", suite.ID, 1, now})
	second := publishTestAdjudication(t, ledger, adjudicationFixture{"ar_1723200000001_0123456789abcdef", suite.ID, 0, now.Add(time.Second)})
	if first.RevisionNumber != 1 || second.RevisionNumber != 2 {
		t.Fatalf("revisions = %d, %d", first.RevisionNumber, second.RevisionNumber)
	}
	loadedFirst := loadTestAdjudication(t, ledger, first.ID)
	loadedSecond := loadTestAdjudication(t, ledger, second.ID)
	if loadedFirst.Score.IncompleteCases != 1 || loadedSecond.Score.IncompleteCases != 0 {
		t.Fatal("correction overwrote adjudication history")
	}
}

type adjudicationFixture struct {
	id         model.AdjudicationRevisionID
	suiteID    model.EvalSuiteRunID
	incomplete int
	createdAt  time.Time
}

func publishTestAdjudication(t *testing.T, ledger *LedgerRecordStore, fixture adjudicationFixture) model.AdjudicationRevision {
	t.Helper()
	revision, err := ledger.PublishAdjudication(model.AdjudicationRevision{ID: fixture.id, SuiteRunID: fixture.suiteID, Document: model.AdjudicationDocument{SuiteRunID: fixture.suiteID}, Score: model.EvalScore{IncompleteCases: fixture.incomplete}, CreatedAt: fixture.createdAt})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func loadTestAdjudication(t *testing.T, ledger *LedgerRecordStore, id model.AdjudicationRevisionID) model.AdjudicationRevision {
	t.Helper()
	revision, err := ledger.LoadAdjudication(id)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func newTestLedger(t *testing.T, directory string) *LedgerRecordStore {
	t.Helper()
	ledger, err := NewLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func saveTestReviews(t *testing.T, ledger *LedgerRecordStore, records ...model.ReviewRecord) {
	t.Helper()
	for _, record := range records {
		if err := ledger.Save(record); err != nil {
			t.Fatal(err)
		}
	}
}

func loadTestHistory(t *testing.T, ledger *LedgerRecordStore, query HistoryQuery) HistoryPage {
	t.Helper()
	page, err := ledger.History(query)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestLedgerRejectsNewerSchema(t *testing.T) {
	directory := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(directory, ledgerFilename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY); INSERT INTO schema_migrations(version) VALUES(99)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := NewLedgerRecordStore(directory); err == nil {
		t.Fatal("expected newer schema error")
	}
}

func TestDeferredLedgerReadDoesNotMigrateOlderSchema(t *testing.T) {
	directory := t.TempDir()
	writeSchemaVersion(t, directory, 0)
	deferred, err := NewDeferredLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deferred.History(HistoryQuery{Limit: 10}); err == nil || !strings.Contains(err.Error(), "requires state preparation") {
		t.Fatalf("error = %v", err)
	}
	if version := readSchemaVersion(t, directory); version != 0 {
		t.Fatalf("schema version = %d, want unchanged 0", version)
	}
}

func writeSchemaVersion(t *testing.T, directory string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(directory, ledgerFilename))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY); INSERT INTO schema_migrations(version) VALUES(?)", version); err != nil {
		t.Fatal(err)
	}
}

func readSchemaVersion(t *testing.T, directory string) int {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(directory, ledgerFilename))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func ledgerFixture(lifecycle model.Lifecycle) model.ReviewRecord {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	record := model.ReviewRecord{SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: "rp_1723200000000_0123456789abcdef", Lifecycle: lifecycle, Subject: model.ReviewSubject{Kind: model.SubjectWorkingChanges, Repository: "/repo", Identity: "subject", ChangedPaths: []string{"a.go"}, Patch: "diff"}, ProfileRevision: model.ProfileRevision{Name: "bugs"}, ProfileSnapshot: model.ProfileSnapshot{Name: "bugs"}, Runtime: &model.RuntimeProvenance{Version: "test"}, Timings: &model.ReviewTimings{TotalMS: 1}, CreatedAt: now, UpdatedAt: now, Passes: []model.PassRecord{{Name: "review", Required: true, Attempts: []model.AttemptRecord{{Number: 1, Outcome: model.AttemptCompleted, Provenance: model.ReviewerProvenance{ReviewerID: "opencode"}, RetryAfterMS: 750, Artifacts: []model.ArtifactReference{{Kind: "assistant-text", Path: "artifacts/a", Size: 1, Digest: "digest"}}, StartedAt: now, CompletedAt: now}}}}}
	if lifecycle == model.LifecycleCompleted {
		record.Result = &model.ReviewResult{Status: model.ResultFindings, Summary: "finding", Raw: "raw", Findings: []model.Finding{{Ordinal: 1, Severity: "high", Category: "correctness", Location: "a.go:1", Failure: "failure", Evidence: "evidence", Fix: "fix", Test: "test"}}}
	} else {
		record.Termination = &model.ReviewTermination{Category: model.TerminationDeadlineExceeded, Phase: model.PhaseReviewerExecution, Message: "deadline"}
	}
	return record
}
