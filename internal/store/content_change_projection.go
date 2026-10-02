package store

// Content change projection indexes each Review by the commit-free content
// change set its Subject covers, so a Checkpoint can find Reviews of exactly
// the content it is about to accept.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"time"
)

type CoverageQuery struct {
	ProfileSource string
	Changes       []model.ContentChange
}

type CoverageCandidate struct {
	ID        model.ReviewID
	Lifecycle model.Lifecycle
	CreatedAt time.Time
}

func replaceContentChanges(tx *sql.Tx, id model.ReviewID, changes []model.ContentChange) error {
	if _, err := tx.Exec("DELETE FROM review_content_changes WHERE review_id = ?", id); err != nil {
		return err
	}
	for _, change := range changes {
		if _, err := tx.Exec("INSERT INTO review_content_changes(review_id,path,before_blob,after_blob) VALUES(?,?,?,?)", id, change.Path, change.Before, change.After); err != nil {
			return fmt.Errorf("project content change %q: %w", change.Path, err)
		}
	}
	return nil
}

// coverageStatement matches Reviews whose stored set equals the query set: as
// many rows as the query, every one of them in the query. The first change
// narrows candidates through the entry index before the counts run.
const coverageStatement = `WITH wanted AS (
		SELECT json_extract(value,'$.path') AS path, json_extract(value,'$.before') AS before_blob, json_extract(value,'$.after') AS after_blob
		FROM json_each(?))
	SELECT r.id, r.lifecycle, r.created_at FROM reviews r
	WHERE json_extract(r.profile_revision,'$.source') = ?
	AND r.id IN (SELECT review_id FROM review_content_changes WHERE path = ? AND before_blob = ? AND after_blob = ?)
	AND (SELECT count(*) FROM review_content_changes c WHERE c.review_id = r.id) = ?
	AND (SELECT count(*) FROM review_content_changes c JOIN wanted w
		ON c.path = w.path AND c.before_blob = w.before_blob AND c.after_blob = w.after_blob
		WHERE c.review_id = r.id) = ?
	ORDER BY r.created_at DESC, r.id DESC`

// ContentChangeCoverage returns every Review, in any lifecycle and newest
// first, whose Profile source matches and whose content change set is exactly
// the queried set.
func (s *LedgerRecordStore) ContentChangeCoverage(query CoverageQuery) (candidates []CoverageCandidate, returnErr error) {
	if err := validateCoverageQuery(query); err != nil {
		return nil, err
	}
	wanted, err := json.Marshal(query.Changes)
	if err != nil {
		return nil, err
	}
	first := query.Changes[0]
	count := len(query.Changes)
	rows, err := s.db.Query(coverageStatement, string(wanted), query.ProfileSource, first.Path, first.Before, first.After, count, count)
	if err != nil {
		return nil, fmt.Errorf("query content change coverage: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	candidates = []CoverageCandidate{}
	for rows.Next() {
		var candidate CoverageCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Lifecycle, &candidate.CreatedAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func validateCoverageQuery(query CoverageQuery) error {
	if query.ProfileSource == "" {
		return errors.New("coverage query requires a Profile source")
	}
	if len(query.Changes) == 0 {
		return errors.New("coverage query requires at least one content change")
	}
	paths := make(map[string]bool, len(query.Changes))
	for _, change := range query.Changes {
		if paths[change.Path] {
			return fmt.Errorf("coverage query repeats path %q", change.Path)
		}
		paths[change.Path] = true
	}
	return nil
}

func (s *DeferredLedgerRecordStore) ContentChangeCoverage(query CoverageQuery) ([]CoverageCandidate, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.ContentChangeCoverage(query)
}
