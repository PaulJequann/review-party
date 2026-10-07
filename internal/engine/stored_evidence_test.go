package engine

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/artifact"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

const patchSentinel = "PATCH-SENTINEL-4f1c"

func hostStateConductor(t *testing.T, executor attemptExecutor) (*Conductor, string) {
	t.Helper()
	state := t.TempDir()
	ledger, err := store.NewLedgerRecordStore(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	})
	manager := newTestConfigurationManagerWithDeadline(t, time.Second)
	conductor, err := newConductorWithManager(ledger, catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor}), manager, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conductor.artifacts = mustNewArtifactStore(t, state)
	conductor.getRunner().publisher = newArtifactPublisher(conductor.artifacts)
	return conductor, state
}

func sentinelRepository(t *testing.T) string {
	t.Helper()
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \""+patchSentinel+"\"\n")
	return repository
}

func filesHolding(t *testing.T, root, needle string) []string {
	t.Helper()
	var holding []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(contents, []byte(needle)) {
			holding = append(holding, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return holding
}

func TestReviewLeavesNoPatchInHostState(t *testing.T) {
	repository := sentinelRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor, state := hostStateConductor(t, executor)

	if _, err := conductor.Review(testContext(t), testSelection(repository)); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(promptPatch(t, executor.attempts[0].Prompt), patchSentinel) {
		t.Fatal("the reviewer prompt lost the patch")
	}
	if holding := filesHolding(t, state, patchSentinel); len(holding) != 0 {
		t.Fatalf("host state holds the patch: %v", holding)
	}
}

func storedAttempt(t *testing.T, conductor *Conductor, id model.ReviewID) model.AttemptRecord {
	t.Helper()
	record, err := conductor.store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return record.Passes[0].Attempts[0]
}

func TestCompletedReviewKeepsNoRawOutput(t *testing.T) {
	executor := &scriptedExecutor{availability: availability{Available: true}, execute: func(context.Context, attemptSpec) attemptExecution {
		return attemptExecution{AssistantText: cleanReview, ReviewerNoise: codexWebsocketNoise, Outcome: model.AttemptCompleted}
	}}
	conductor, state := hostStateConductor(t, executor)

	record, err := conductor.Review(testContext(t), testSelection(changedTestRepository(t)))
	if err != nil {
		t.Fatal(err)
	}

	if record.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("lifecycle = %s, want completed", record.Lifecycle)
	}
	if artifacts := storedAttempt(t, conductor, record.ID).Artifacts; len(artifacts) != 0 {
		t.Fatalf("completed attempt kept evidence %v", artifacts)
	}
	if holding := filesHolding(t, state, "405 Method Not Allowed"); len(holding) != 0 {
		t.Fatalf("host state holds the reviewer noise: %v", holding)
	}
}

func TestIncompleteReviewKeepsItsRawOutput(t *testing.T) {
	conductor, _ := hostStateConductor(t, successfulExecutor("not a result contract"))

	record, err := conductor.Review(testContext(t), testSelection(changedTestRepository(t)))
	if err != nil {
		t.Fatal(err)
	}

	if record.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("lifecycle = %s, want incomplete", record.Lifecycle)
	}
	artifacts := storedAttempt(t, conductor, record.ID).Artifacts
	if len(artifacts) != 1 || artifacts[0].Kind != artifact.AssistantText {
		t.Fatalf("incomplete attempt evidence = %v, want its assistant text", artifacts)
	}
	contents, err := conductor.artifacts.Read(artifacts[0])
	if err != nil || string(contents) != "not a result contract" {
		t.Fatalf("assistant text = %q, %v", contents, err)
	}
}

func steppingClock() func() time.Time {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		current = current.Add(time.Minute)
		return current
	}
}

func TestFailureEvidenceKeepsOnlyTheNewestAttempts(t *testing.T) {
	conductor, state := hostStateConductor(t, successfulExecutor("not a result contract"))
	conductor.now = steppingClock()
	repository := changedTestRepository(t)
	var reviews []model.ReviewID
	for range retainedFailureEvidence + 2 {
		record, err := conductor.Review(testContext(t), testSelection(repository))
		if err != nil {
			t.Fatal(err)
		}
		reviews = append(reviews, record.ID)
	}

	kept := make([]bool, 0, len(reviews))
	for _, id := range reviews {
		kept = append(kept, len(storedAttempt(t, conductor, id).Artifacts) > 0)
	}
	if want := append([]bool{false, false}, slices.Repeat([]bool{true}, retainedFailureEvidence)...); !slices.Equal(kept, want) {
		t.Fatalf("kept evidence by age = %v, want %v", kept, want)
	}
	entries, err := os.ReadDir(filepath.Join(state, artifact.Directory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != retainedFailureEvidence {
		t.Fatalf("artifact directories = %d, want %d", len(entries), retainedFailureEvidence)
	}
}

func artifactFiles(t *testing.T, state string) []string {
	t.Helper()
	files := []string{}
	err := filepath.WalkDir(filepath.Join(state, artifact.Directory), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func writeOrphanEvidence(t *testing.T, state string) string {
	t.Helper()
	directory := filepath.Join(state, artifact.Directory, "rp_1_orphan", "1")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "constructed-prompt.txt"), "prompt")
	return directory
}

func initializeTestState(t *testing.T) string {
	t.Helper()
	if _, err := InitializeReviewParty(ReviewPartyInitialization{Repository: testRepository(t)}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), "review-party")
}

func TestPreparingTheLedgerRemovesEvidenceItDoesNotName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	orphan := writeOrphanEvidence(t, filepath.Join(os.Getenv("XDG_STATE_HOME"), "review-party"))

	state := initializeTestState(t)

	if files := artifactFiles(t, state); len(files) != 0 {
		t.Fatalf("artifact files = %v, want none", files)
	}
	if _, err := os.Stat(filepath.Dir(orphan)); !os.IsNotExist(err) {
		t.Fatalf("emptied artifact directory remains: %v", err)
	}
}

type keepAllEvidence struct {
	store.RecordStore
}

func (keepAllEvidence) ExpireEvidence(int, func([]model.ArtifactReference) error) error {
	return nil
}

func failedReviewsWithoutExpiry(t *testing.T, state string, count int) []model.ReviewID {
	t.Helper()
	ledger, err := store.NewLedgerRecordStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	}()
	executors := map[string]attemptExecutor{defaultReviewer: successfulExecutor("not a result contract")}
	conductor, err := newConductorWithManager(keepAllEvidence{ledger}, catalogWithExecutors(executors), newTestConfigurationManagerWithDeadline(t, time.Second), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conductor.artifacts = mustNewArtifactStore(t, state)
	conductor.getRunner().publisher = newArtifactPublisher(conductor.artifacts)
	conductor.now = steppingClock()
	repository := changedTestRepository(t)
	reviews := make([]model.ReviewID, 0, count)
	for range count {
		record, err := conductor.Review(testContext(t), testSelection(repository))
		if err != nil {
			t.Fatal(err)
		}
		reviews = append(reviews, record.ID)
	}
	return reviews
}

func execLedger(t *testing.T, state string, statements ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(state, "ledger.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func forgetStoredTextUpgrade(t *testing.T, state string) {
	t.Helper()
	execLedger(t, state,
		"ALTER TABLE reviews ADD COLUMN result_raw BLOB",
		"ALTER TABLE attempts ADD COLUMN raw_output BLOB",
		"DROP TABLE pending_maintenance",
		"DELETE FROM schema_migrations WHERE version = 15",
	)
}

func TestUpgradingTheLedgerKeepsOnlyTheNewestFailureEvidence(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := initializeTestState(t)
	reviews := failedReviewsWithoutExpiry(t, state, retainedFailureEvidence+2)
	forgetStoredTextUpgrade(t, state)

	initializeTestState(t)

	if kept, want := reviewsWithEvidence(t, state), reviews[2:]; !slices.Equal(kept, want) {
		t.Fatalf("reviews with evidence after upgrade = %v, want the newest %v", kept, want)
	}
}

func reviewsWithEvidence(t *testing.T, state string) []model.ReviewID {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(state, artifact.Directory))
	if err != nil {
		t.Fatal(err)
	}
	reviews := []model.ReviewID{}
	for _, entry := range entries {
		reviews = append(reviews, model.ReviewID(entry.Name()))
	}
	return reviews
}

func freeLedgerPages(t *testing.T, state string) int {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(state, "ledger.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	var pages int
	if err := db.QueryRow("PRAGMA freelist_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	return pages
}

var fragmentLedger = []string{
	"CREATE TABLE filler(data BLOB)",
	"INSERT INTO filler SELECT randomblob(4096) FROM (WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 256) SELECT i FROM n)",
	"DROP TABLE filler",
}

func initializeAgain(t *testing.T) ReviewPartyInitializationResult {
	t.Helper()
	result, err := InitializeReviewParty(ReviewPartyInitialization{Repository: testRepository(t)})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInitializingAfterAnInterruptedUpgradeFinishesItsMaintenance(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := initializeTestState(t)
	reviews := failedReviewsWithoutExpiry(t, state, retainedFailureEvidence+2)
	writeOrphanEvidence(t, state)
	execLedger(t, state, append(fragmentLedger, "INSERT INTO pending_maintenance VALUES(1)")...)

	if initializeAgain(t).AlreadyReady {
		t.Fatal("init reported a ledger with pending maintenance as already ready")
	}

	if pages := freeLedgerPages(t, state); pages != 0 {
		t.Fatalf("free ledger pages = %d, want the ledger compacted", pages)
	}
	if kept, want := reviewsWithEvidence(t, state), reviews[2:]; !slices.Equal(kept, want) {
		t.Fatalf("reviews with evidence = %v, want only the newest %v", kept, want)
	}
	if !initializeAgain(t).AlreadyReady {
		t.Fatal("maintenance is still pending after it completed")
	}
}

func TestInitializingAReadyLedgerLeavesItAlone(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := initializeTestState(t)
	reviews := failedReviewsWithoutExpiry(t, state, retainedFailureEvidence+2)
	orphan := writeOrphanEvidence(t, state)
	execLedger(t, state, fragmentLedger...)
	free := freeLedgerPages(t, state)

	if !initializeAgain(t).AlreadyReady {
		t.Fatal("init did not report the ready ledger as ready")
	}

	if pages := freeLedgerPages(t, state); pages != free || pages == 0 {
		t.Fatalf("free ledger pages = %d, want the %d left before init", pages, free)
	}
	want := append(slices.Clone(reviews), model.ReviewID(filepath.Base(filepath.Dir(orphan))))
	slices.Sort(want)
	if kept := reviewsWithEvidence(t, state); !slices.Equal(kept, want) {
		t.Fatalf("reviews with evidence = %v, want every recorded and unrecorded one %v", kept, want)
	}
}

func TestInitializingAfterAFailedSweepRetriesItsMaintenance(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := initializeTestState(t)
	orphan := writeOrphanEvidence(t, state)
	execLedger(t, state, "INSERT INTO pending_maintenance VALUES(1)")
	if err := os.Chmod(orphan, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeReviewParty(ReviewPartyInitialization{Repository: testRepository(t)}); err == nil {
		t.Fatal("init succeeded although the sweep could not remove an unreferenced file")
	}
	if err := os.Chmod(orphan, 0o700); err != nil {
		t.Fatal(err)
	}

	if initializeAgain(t).AlreadyReady {
		t.Fatal("a failed sweep cleared the pending maintenance")
	}

	if files := artifactFiles(t, state); len(files) != 0 {
		t.Fatalf("artifact files = %v, want the retried sweep to remove them", files)
	}
}

func TestFreshStateLeavesTheReplacedLedgersEvidenceInItsBackup(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprintf("interrupted=%t", interrupted), func(t *testing.T) {
			assertFreshStateLeavesEvidenceInBackup(t, interrupted)
		})
	}
}

// assertFreshStateLeavesEvidenceInBackup backs up an unusable ledger beside
// evidence. An interrupted backup moved the ledger but not the evidence.
func assertFreshStateLeavesEvidenceInBackup(t *testing.T, interrupted bool) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := initializeTestState(t)
	orphan := writeOrphanEvidence(t, state)
	writeTestFile(t, filepath.Join(state, "ledger.sqlite"), "not a ledger")
	repository := testRepository(t)

	backedUp, err := InitializeReviewParty(ReviewPartyInitialization{Repository: repository, BackupIncompatible: true})
	if err != nil {
		t.Fatal(err)
	}
	if interrupted {
		if err := os.Rename(filepath.Join(backedUp.Backup.Directory, artifact.Directory), filepath.Join(state, artifact.Directory)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := InitializeReviewParty(ReviewPartyInitialization{Repository: repository, Fresh: true}); err != nil {
		t.Fatal(err)
	}

	relative, err := filepath.Rel(state, orphan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backedUp.Backup.Directory, relative)); err != nil {
		t.Fatalf("the backup lost the replaced ledger's evidence: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(state, artifact.Directory)); !os.IsNotExist(err) {
		t.Fatalf("fresh state kept the replaced ledger's evidence: %v", err)
	}
}
