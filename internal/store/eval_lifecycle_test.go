package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestEvalLifecycleMigrationRequiresPreparation(t *testing.T) {
	directory := t.TempDir()
	writeEvalSchemaV4(t, directory)
	_, err := ReviewRecordStatePrepared(directory)
	if !errors.Is(err, ErrReviewRecordStateRequiresPreparation) {
		t.Fatalf("error = %v", err)
	}
	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}
}

func TestEvalCheckpointRollsBackChildWhenParentUpdateFails(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer ledger.Close()
	suite, run := pendingEvalRecords(time.Now().UTC())
	if err := ledger.CreateEvalSuiteRun(suite, []model.EvalRun{run}); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.db.Exec(`CREATE TRIGGER fail_eval_suite_checkpoint BEFORE UPDATE ON eval_suite_runs BEGIN SELECT RAISE(ABORT, 'checkpoint failed'); END`); err != nil {
		t.Fatal(err)
	}
	run.ExecutionState = model.EvalRunning
	run.UpdatedAt = run.CreatedAt.Add(time.Second)
	suite.Lifecycle = model.LifecycleRunning
	if err := ledger.CheckpointEvalRun(suite, run); err == nil {
		t.Fatal("expected checkpoint failure")
	}
	assertPendingEvalCheckpoint(t, ledger, suite.ID, run.ID)
}

func writeEvalSchemaV4(t *testing.T, directory string) {
	t.Helper()
	ledger := newTestLedger(t, directory)
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(directory, ledgerFilename))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE adjudication_revisions; DROP TABLE eval_runs; DROP TABLE eval_suite_runs; ALTER TABLE attempts DROP COLUMN retry_after_ms; DELETE FROM schema_migrations WHERE version>=4"); err != nil {
		t.Fatal(err)
	}
	evalSchema, err := migrationFiles.ReadFile("migrations/004_eval_runs.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(evalSchema) + "; INSERT INTO schema_migrations(version) VALUES(4)"); err != nil {
		t.Fatal(err)
	}
}

func pendingEvalRecords(now time.Time) (model.EvalSuiteRun, model.EvalRun) {
	suite := model.EvalSuiteRun{
		ID: "esr_1723200000000_0123456789abcdef", Suite: "suite", SuiteRevision: "v1",
		SuiteDigest: "digest", Lifecycle: model.LifecyclePending,
		EvalRunIDs: []model.EvalRunID{"er_1723200000000_0123456789abcdef"}, StartedAt: now,
	}
	run := model.EvalRun{
		ID: suite.EvalRunIDs[0], SuiteRunID: suite.ID,
		Case:           model.EvalCaseRevision{ID: "case", SchemaVersion: 1, Digest: "digest"},
		ExecutionState: model.EvalPending, AdjudicationState: model.EvalAdjudicationNotReady, CreatedAt: now, UpdatedAt: now,
	}
	return suite, run
}

func assertPendingEvalCheckpoint(t *testing.T, ledger *LedgerRecordStore, suiteID model.EvalSuiteRunID, runID model.EvalRunID) {
	t.Helper()
	run, err := ledger.LoadEvalRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	suite, err := ledger.LoadEvalSuiteRun(suiteID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionState != model.EvalPending || suite.Lifecycle != model.LifecyclePending {
		t.Fatalf("checkpoint partially committed: child=%s parent=%s", run.ExecutionState, suite.Lifecycle)
	}
}
