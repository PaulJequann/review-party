package store

// Content change projection indexes each Review by the commit-free content
// change set its Subject covers, so a Checkpoint can find the Reviews that
// might cover the content it is about to accept.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"time"
)

// CoverageQuery names a Profile source and the content change set a
// Checkpoint must see covered.
type CoverageQuery struct {
	ProfileSource string
	Changes       []model.ContentChange
}

// CoverageCandidate is a Review that shares at least one content change entry
// with a CoverageQuery. Changes is the Review's full recorded set, sorted by
// path, so the caller decides whether it covers.
type CoverageCandidate struct {
	ID        model.ReviewID
	Lifecycle model.Lifecycle
	CreatedAt time.Time
	Changes   []model.ContentChange
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

// coverageCandidateStatement finds the Profile's Reviews holding any queried
// entry through the entry index, and returns each one's complete set.
const coverageCandidateStatement = `WITH wanted AS (
		SELECT json_extract(value,'$.path') AS path, json_extract(value,'$.before') AS before_blob, json_extract(value,'$.after') AS after_blob
		FROM json_each(?))
	SELECT r.id, r.lifecycle, r.created_at,
		(SELECT json_group_array(json_object('path',c.path,'before',c.before_blob,'after',c.after_blob))
			FROM review_content_changes c WHERE c.review_id = r.id)
	FROM reviews r
	WHERE json_extract(r.profile_revision,'$.source') = ?
	AND r.id IN (SELECT c.review_id FROM wanted w JOIN review_content_changes c
		ON c.path = w.path AND c.before_blob = w.before_blob AND c.after_blob = w.after_blob)
	ORDER BY r.created_at DESC, r.id DESC`

// CoverageCandidates returns every Review, in any lifecycle and newest first,
// whose Profile source matches and whose content change set shares at least
// one entry with the queried set.
func (s *LedgerRecordStore) CoverageCandidates(query CoverageQuery) (candidates []CoverageCandidate, returnErr error) {
	if err := validateCoverageQuery(query); err != nil {
		return nil, err
	}
	wanted, err := json.Marshal(query.Changes)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(coverageCandidateStatement, string(wanted), query.ProfileSource)
	if err != nil {
		return nil, fmt.Errorf("query coverage candidates: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	candidates = []CoverageCandidate{}
	for rows.Next() {
		var candidate CoverageCandidate
		var changes string
		if err := rows.Scan(&candidate.ID, &candidate.Lifecycle, &candidate.CreatedAt, &changes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(changes), &candidate.Changes); err != nil {
			return nil, fmt.Errorf("decode content changes of review %q: %w", candidate.ID, err)
		}
		model.SortContentChanges(candidate.Changes)
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

func (s *DeferredLedgerRecordStore) CoverageCandidates(query CoverageQuery) ([]CoverageCandidate, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.CoverageCandidates(query)
}
