package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reviewparty/internal/model"
	"time"
)

// reviewRecordProjection owns the complete mapping between one public Review
// Record aggregate and its private relational representation.
type reviewRecordProjection struct {
	db *sql.DB
}

func (p reviewRecordProjection) save(record model.ReviewRecord) error {
	if record.SchemaVersion != model.CurrentReviewRecordSchemaVersion {
		return fmt.Errorf("save review record schema %d: current schema is %d", record.SchemaVersion, model.CurrentReviewRecordSchemaVersion)
	}
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("begin review ledger write: %w", err)
	}
	defer tx.Rollback()
	if err := writeReviewAggregate(tx, record); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit review ledger write: %w", err)
	}
	return nil
}

func writeReviewAggregate(tx *sql.Tx, record model.ReviewRecord) error {
	if err := writeReview(tx, record); err != nil {
		return fmt.Errorf("save review: %w", err)
	}
	if err := replaceReviewChildren(tx, record); err != nil {
		return fmt.Errorf("save review children: %w", err)
	}
	return nil
}

func writeReview(tx *sql.Tx, record model.ReviewRecord) error {
	values, err := projectionValues(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO reviews(id,lifecycle,subject,profile_revision,profile_snapshot,result_status,result_summary,result_raw,result_finding_count,termination,runtime,timings,incomplete_cause,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET lifecycle=excluded.lifecycle, subject=excluded.subject, profile_revision=excluded.profile_revision, profile_snapshot=excluded.profile_snapshot, result_status=excluded.result_status, result_summary=excluded.result_summary, result_raw=excluded.result_raw, result_finding_count=excluded.result_finding_count, termination=excluded.termination, runtime=excluded.runtime, timings=excluded.timings, incomplete_cause=excluded.incomplete_cause, updated_at=excluded.updated_at`, values...)
	return err
}

func projectionValues(record model.ReviewRecord) ([]any, error) {
	subject, err := json.Marshal(record.Subject)
	if err != nil {
		return nil, fmt.Errorf("encode review subject: %w", err)
	}
	profile, err := json.Marshal(record.ProfileRevision)
	if err != nil {
		return nil, fmt.Errorf("encode profile revision: %w", err)
	}
	snapshot, err := json.Marshal(record.ProfileSnapshot)
	if err != nil {
		return nil, fmt.Errorf("encode profile snapshot: %w", err)
	}
	termination, err := encodeNullable(record.Termination)
	if err != nil {
		return nil, fmt.Errorf("encode review termination: %w", err)
	}
	runtime, err := encodeNullable(record.Runtime)
	if err != nil {
		return nil, fmt.Errorf("encode runtime provenance: %w", err)
	}
	timings, err := encodeNullable(record.Timings)
	if err != nil {
		return nil, fmt.Errorf("encode review timings: %w", err)
	}
	status, summary, raw, count := projectedResult(record.Result)
	return []any{string(record.ID), record.Lifecycle, subject, profile, snapshot, status, summary, raw, count, termination, runtime, timings, record.IncompleteCause, record.CreatedAt, record.UpdatedAt}, nil
}

func encodeNullable(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func projectedResult(result *model.ReviewResult) (status, summary, raw any, count int) {
	if result == nil {
		return nil, nil, nil, 0
	}
	return result.Status, result.Summary, result.Raw, result.FindingCount()
}

func replaceReviewChildren(tx *sql.Tx, record model.ReviewRecord) error {
	for _, table := range []string{"artifacts", "findings", "attempts", "passes"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE review_id = ?", record.ID); err != nil {
			return err
		}
	}
	for passOrdinal, pass := range record.Passes {
		if err := insertPass(tx, record.ID, passOrdinal, pass); err != nil {
			return err
		}
	}
	if record.Result == nil {
		return nil
	}
	for _, finding := range record.Result.Findings {
		if err := insertFinding(tx, record.ID, finding); err != nil {
			return err
		}
	}
	return nil
}

func insertPass(tx *sql.Tx, id model.ReviewID, passOrdinal int, pass model.PassRecord) error {
	if _, err := tx.Exec("INSERT INTO passes(review_id,ordinal,name,required) VALUES(?,?,?,?)", id, passOrdinal, pass.Name, pass.Required); err != nil {
		return err
	}
	for attemptOrdinal, attempt := range pass.Attempts {
		if err := insertAttempt(tx, attemptIdentity{id, passOrdinal, attemptOrdinal}, attempt); err != nil {
			return err
		}
	}
	return nil
}

type attemptIdentity struct {
	reviewID                    model.ReviewID
	passOrdinal, attemptOrdinal int
}

func insertAttempt(tx *sql.Tx, identity attemptIdentity, attempt model.AttemptRecord) error {
	provenance, err := json.Marshal(attempt.Provenance)
	if err != nil {
		return fmt.Errorf("encode reviewer provenance: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO attempts(review_id,pass_ordinal,ordinal,number,outcome,provenance,diagnostic,raw_output,started_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?)", identity.reviewID, identity.passOrdinal, identity.attemptOrdinal, attempt.Number, attempt.Outcome, provenance, attempt.Diagnostic, attempt.RawOutput, attempt.StartedAt, attempt.CompletedAt); err != nil {
		return err
	}
	for artifactOrdinal, reference := range attempt.Artifacts {
		if err := insertArtifact(tx, identity, artifactOrdinal, reference); err != nil {
			return err
		}
	}
	return nil
}

func insertArtifact(tx *sql.Tx, identity attemptIdentity, ordinal int, reference model.ArtifactReference) error {
	_, err := tx.Exec("INSERT INTO artifacts(review_id,pass_ordinal,attempt_ordinal,ordinal,kind,path,size,digest,truncated) VALUES(?,?,?,?,?,?,?,?,?)", identity.reviewID, identity.passOrdinal, identity.attemptOrdinal, ordinal, reference.Kind, reference.Path, reference.Size, reference.Digest, reference.Truncated)
	return err
}

func insertFinding(tx *sql.Tx, id model.ReviewID, finding model.Finding) error {
	_, err := tx.Exec("INSERT INTO findings(review_id,ordinal,severity,category,location,failure,evidence,fix,test) VALUES(?,?,?,?,?,?,?,?,?)", id, finding.Ordinal, finding.Severity, finding.Category, finding.Location, finding.Failure, finding.Evidence, finding.Fix, finding.Test)
	return err
}

func (p reviewRecordProjection) load(id model.ReviewID) (model.ReviewRecord, error) {
	values, err := p.loadReviewValues(id)
	if err != nil {
		return model.ReviewRecord{}, err
	}
	record, err := values.record(id)
	if err != nil {
		return model.ReviewRecord{}, err
	}
	if err := p.loadPasses(&record); err != nil {
		return record, err
	}
	if record.Result != nil {
		if err := p.loadFindings(record.ID, record.Result); err != nil {
			return record, err
		}
	}
	return record, nil
}

type reviewValues struct {
	lifecycle                                                 model.Lifecycle
	subject, profile, snapshot, termination, runtime, timings []byte
	status, summary, raw                                      sql.NullString
	findingCount                                              int
	incompleteCause                                           string
	createdAt, updatedAt                                      time.Time
}

func (p reviewRecordProjection) loadReviewValues(id model.ReviewID) (reviewValues, error) {
	row := p.db.QueryRow("SELECT lifecycle,subject,profile_revision,profile_snapshot,result_status,result_summary,result_raw,result_finding_count,termination,runtime,timings,incomplete_cause,created_at,updated_at FROM reviews WHERE id = ?", id)
	var values reviewValues
	if err := row.Scan(&values.lifecycle, &values.subject, &values.profile, &values.snapshot, &values.status, &values.summary, &values.raw, &values.findingCount, &values.termination, &values.runtime, &values.timings, &values.incompleteCause, &values.createdAt, &values.updatedAt); err != nil {
		return values, fmt.Errorf("read review record %q: %w", id, err)
	}
	return values, nil
}

func (values reviewValues) record(id model.ReviewID) (model.ReviewRecord, error) {
	record := model.ReviewRecord{ID: id, SchemaVersion: model.CurrentReviewRecordSchemaVersion, Lifecycle: values.lifecycle, IncompleteCause: values.incompleteCause, CreatedAt: values.createdAt, UpdatedAt: values.updatedAt}
	for _, encoded := range []struct {
		payload []byte
		target  any
	}{
		{values.subject, &record.Subject},
		{values.profile, &record.ProfileRevision},
		{values.snapshot, &record.ProfileSnapshot},
		{values.termination, &record.Termination},
		{values.runtime, &record.Runtime},
		{values.timings, &record.Timings},
	} {
		if err := decodeProjectionValue(encoded.payload, encoded.target); err != nil {
			return record, err
		}
	}
	if values.status.Valid {
		record.Result = &model.ReviewResult{Status: model.ResultStatus(values.status.String), Summary: values.summary.String, Raw: values.raw.String, Findings: []model.Finding{}}
	}
	return record, nil
}

func decodeProjectionValue(payload []byte, target any) error {
	if len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode review ledger projection: %w", err)
	}
	return nil
}

func (p reviewRecordProjection) loadPasses(record *model.ReviewRecord) error {
	rows, err := p.db.Query("SELECT ordinal,name,required FROM passes WHERE review_id=? ORDER BY ordinal", record.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ordinal int
		var pass model.PassRecord
		if err := rows.Scan(&ordinal, &pass.Name, &pass.Required); err != nil {
			return err
		}
		attempts, err := p.loadAttempts(record.ID, ordinal)
		if err != nil {
			return err
		}
		pass.Attempts = attempts
		record.Passes = append(record.Passes, pass)
	}
	return rows.Err()
}

func (p reviewRecordProjection) loadAttempts(id model.ReviewID, passOrdinal int) ([]model.AttemptRecord, error) {
	rows, err := p.db.Query("SELECT ordinal,number,outcome,provenance,diagnostic,raw_output,started_at,completed_at FROM attempts WHERE review_id=? AND pass_ordinal=? ORDER BY ordinal", id, passOrdinal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attempts := []model.AttemptRecord{}
	for rows.Next() {
		var ordinal int
		var attempt model.AttemptRecord
		var provenance []byte
		if err := rows.Scan(&ordinal, &attempt.Number, &attempt.Outcome, &provenance, &attempt.Diagnostic, &attempt.RawOutput, &attempt.StartedAt, &attempt.CompletedAt); err != nil {
			return nil, err
		}
		if err := decodeProjectionValue(provenance, &attempt.Provenance); err != nil {
			return nil, err
		}
		artifacts, err := p.loadArtifacts(id, passOrdinal, ordinal)
		if err != nil {
			return nil, err
		}
		attempt.Artifacts = artifacts
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func (p reviewRecordProjection) loadArtifacts(id model.ReviewID, passOrdinal, attemptOrdinal int) ([]model.ArtifactReference, error) {
	rows, err := p.db.Query("SELECT kind,path,size,digest,truncated FROM artifacts WHERE review_id=? AND pass_ordinal=? AND attempt_ordinal=? ORDER BY ordinal", id, passOrdinal, attemptOrdinal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artifacts []model.ArtifactReference
	for rows.Next() {
		var artifact model.ArtifactReference
		if err := rows.Scan(&artifact.Kind, &artifact.Path, &artifact.Size, &artifact.Digest, &artifact.Truncated); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (p reviewRecordProjection) loadFindings(id model.ReviewID, result *model.ReviewResult) error {
	rows, err := p.db.Query("SELECT ordinal,severity,category,location,failure,evidence,fix,test FROM findings WHERE review_id=? ORDER BY ordinal", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var finding model.Finding
		if err := rows.Scan(&finding.Ordinal, &finding.Severity, &finding.Category, &finding.Location, &finding.Failure, &finding.Evidence, &finding.Fix, &finding.Test); err != nil {
			return err
		}
		result.Findings = append(result.Findings, finding)
	}
	return rows.Err()
}
