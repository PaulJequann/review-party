package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"time"
)

// evalProjection owns the relational mapping and transactional writes for
// Eval Suite Run and Eval Run aggregates.
type evalProjection struct{ db *sql.DB }

func nullableEvalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func (p evalProjection) loadEvalRun(id model.EvalRunID) (model.EvalRun, error) {
	var run model.EvalRun
	var casePayload []byte
	err := p.db.QueryRow(`SELECT id,suite_run_id,case_revision,COALESCE(review_id,''),execution_state,adjudication_state,created_at,updated_at FROM eval_runs WHERE id=?`, id).Scan(&run.ID, &run.SuiteRunID, &casePayload, &run.ReviewID, &run.ExecutionState, &run.AdjudicationState, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return model.EvalRun{}, err
	}
	if err := json.Unmarshal(casePayload, &run.Case); err != nil {
		return model.EvalRun{}, err
	}
	return run, nil
}

type statementExecutor interface {
	Exec(string, ...any) (sql.Result, error)
}

func insertEvalRun(executor statementExecutor, run model.EvalRun) error {
	casePayload, err := json.Marshal(run.Case)
	if err != nil {
		return err
	}
	var reviewID any
	if run.ReviewID != "" {
		reviewID = run.ReviewID
	}
	_, err = executor.Exec(`INSERT INTO eval_runs(id,suite_run_id,case_id,case_schema_version,case_digest,case_revision,review_id,execution_state,adjudication_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.SuiteRunID, run.Case.ID, run.Case.SchemaVersion, run.Case.Digest, casePayload, reviewID, run.ExecutionState, run.AdjudicationState, run.CreatedAt.UTC(), run.UpdatedAt.UTC())
	return err
}

func saveEvalSuiteRun(executor statementExecutor, run model.EvalSuiteRun) error {
	experiment, err := json.Marshal(run.Experiment)
	if err != nil {
		return err
	}
	runIDs, err := json.Marshal(run.EvalRunIDs)
	if err != nil {
		return err
	}
	termination, err := json.Marshal(run.Termination)
	if err != nil {
		return err
	}
	_, err = executor.Exec(`INSERT INTO eval_suite_runs(id,suite,suite_revision,suite_digest,experiment,eval_run_ids,lifecycle,termination,completed_clean_count,completed_findings_count,incomplete_count,started_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET eval_run_ids=excluded.eval_run_ids,lifecycle=excluded.lifecycle,termination=excluded.termination,completed_clean_count=excluded.completed_clean_count,completed_findings_count=excluded.completed_findings_count,incomplete_count=excluded.incomplete_count,completed_at=excluded.completed_at`, run.ID, run.Suite, run.SuiteRevision, run.SuiteDigest, experiment, runIDs, run.Lifecycle, termination, run.CompletedCleanCount, run.CompletedFindingCount, run.IncompleteCount, run.StartedAt.UTC(), nullableEvalTime(run.CompletedAt))
	return err
}

func (p evalProjection) createSuiteRun(run model.EvalSuiteRun, evalRuns []model.EvalRun) (returnErr error) {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackTransaction(tx)) }()
	if err := saveEvalSuiteRun(tx, run); err != nil {
		return err
	}
	for _, evalRun := range evalRuns {
		if err := insertEvalRun(tx, evalRun); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (p evalProjection) checkpoint(run model.EvalSuiteRun, evalRun model.EvalRun) (returnErr error) {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackTransaction(tx)) }()
	var reviewID any
	if evalRun.ReviewID != "" {
		reviewID = evalRun.ReviewID
	}
	result, err := tx.Exec(`UPDATE eval_runs SET review_id=?,execution_state=?,adjudication_state=?,updated_at=? WHERE id=? AND suite_run_id=?`, reviewID, evalRun.ExecutionState, evalRun.AdjudicationState, evalRun.UpdatedAt.UTC(), evalRun.ID, run.ID)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("checkpoint Eval Run %q: expected one row, updated %d", evalRun.ID, updated)
	}
	if err := saveEvalSuiteRun(tx, run); err != nil {
		return err
	}
	return tx.Commit()
}

func (p evalProjection) terminateSuiteRun(run model.EvalSuiteRun) error {
	if run.Lifecycle != model.LifecycleIncomplete {
		return fmt.Errorf("terminate Eval Suite Run %q: lifecycle must be incomplete", run.ID)
	}
	if run.Termination == nil {
		return fmt.Errorf("terminate Eval Suite Run %q: termination is required", run.ID)
	}
	if run.CompletedAt.IsZero() {
		return fmt.Errorf("terminate Eval Suite Run %q: completion time is required", run.ID)
	}
	return saveEvalSuiteRun(p.db, run)
}

func (p evalProjection) loadSuiteRun(id model.EvalSuiteRunID) (model.EvalSuiteRun, error) {
	var run model.EvalSuiteRun
	var experiment, runIDs, termination []byte
	var completedAt sql.NullTime
	err := p.db.QueryRow(`SELECT id,suite,suite_revision,suite_digest,experiment,eval_run_ids,lifecycle,termination,completed_clean_count,completed_findings_count,incomplete_count,started_at,completed_at FROM eval_suite_runs WHERE id=?`, id).Scan(&run.ID, &run.Suite, &run.SuiteRevision, &run.SuiteDigest, &experiment, &runIDs, &run.Lifecycle, &termination, &run.CompletedCleanCount, &run.CompletedFindingCount, &run.IncompleteCount, &run.StartedAt, &completedAt)
	if err != nil {
		return model.EvalSuiteRun{}, err
	}
	if err := json.Unmarshal(experiment, &run.Experiment); err != nil {
		return model.EvalSuiteRun{}, err
	}
	if err := json.Unmarshal(runIDs, &run.EvalRunIDs); err != nil {
		return model.EvalSuiteRun{}, err
	}
	if len(termination) > 0 && string(termination) != "null" {
		if err := json.Unmarshal(termination, &run.Termination); err != nil {
			return model.EvalSuiteRun{}, err
		}
	}
	if completedAt.Valid {
		run.CompletedAt = completedAt.Time
	}
	return run, nil
}
