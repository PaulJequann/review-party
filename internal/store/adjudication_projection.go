package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reviewparty/internal/model"
)

// adjudicationProjection owns immutable Adjudication Revision persistence.
type adjudicationProjection struct{ db *sql.DB }

func (p adjudicationProjection) publish(revision model.AdjudicationRevision) (published model.AdjudicationRevision, returnErr error) {
	document, err := json.Marshal(revision.Document)
	if err != nil {
		return model.AdjudicationRevision{}, err
	}
	score, err := json.Marshal(revision.Score)
	if err != nil {
		return model.AdjudicationRevision{}, err
	}
	tx, err := p.db.Begin()
	if err != nil {
		return model.AdjudicationRevision{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackTransaction(tx)) }()
	if err := tx.QueryRow(`SELECT COALESCE(MAX(revision_number),0)+1 FROM adjudication_revisions WHERE suite_run_id=?`, revision.SuiteRunID).Scan(&revision.RevisionNumber); err != nil {
		return model.AdjudicationRevision{}, err
	}
	if _, err := tx.Exec(`INSERT INTO adjudication_revisions(id,suite_run_id,revision_number,document,score,created_at) VALUES(?,?,?,?,?,?)`, revision.ID, revision.SuiteRunID, revision.RevisionNumber, document, score, revision.CreatedAt.UTC()); err != nil {
		return model.AdjudicationRevision{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdjudicationRevision{}, err
	}
	return revision, nil
}

func (p adjudicationProjection) load(id model.AdjudicationRevisionID) (model.AdjudicationRevision, error) {
	var revision model.AdjudicationRevision
	var document, score []byte
	if err := p.db.QueryRow(`SELECT id,suite_run_id,revision_number,document,score,created_at FROM adjudication_revisions WHERE id=?`, id).Scan(&revision.ID, &revision.SuiteRunID, &revision.RevisionNumber, &document, &score, &revision.CreatedAt); err != nil {
		return model.AdjudicationRevision{}, err
	}
	if err := json.Unmarshal(document, &revision.Document); err != nil {
		return model.AdjudicationRevision{}, err
	}
	if err := json.Unmarshal(score, &revision.Score); err != nil {
		return model.AdjudicationRevision{}, err
	}
	return revision, nil
}
