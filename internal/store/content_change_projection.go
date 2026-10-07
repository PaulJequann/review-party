package store

// Content change projection indexes each Review by the commit-free content
// transitions its Subject covers, one edge per path from the blob before to
// the blob after, so a Checkpoint can walk the reviewed states of a path.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"time"
)

// TransitionQuery names a Profile source and the paths whose recorded
// transitions a Checkpoint walks.
type TransitionQuery struct {
	ProfileSource string
	Paths         []string
}

// ContentTransition is one Review's edge on one path: the blob it reviewed
// the path from and the blob it reviewed it to.
type ContentTransition struct {
	Review    model.ReviewID
	Lifecycle model.Lifecycle
	CreatedAt time.Time
	Path      string
	Before    string
	After     string
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

const contentTransitionStatement = `SELECT r.id, r.lifecycle, r.created_at, c.path, c.before_blob, c.after_blob
	FROM review_content_changes c JOIN reviews r ON r.id = c.review_id
	WHERE json_extract(r.profile_revision,'$.source') = ?
	AND c.path IN (SELECT value FROM json_each(?))
	ORDER BY c.path, r.created_at, r.id`

// ContentTransitions returns every edge a Review of the Profile source
// recorded on any queried path, in any lifecycle, grouped by path and oldest
// first within a path.
func (s *LedgerRecordStore) ContentTransitions(query TransitionQuery) (transitions []ContentTransition, returnErr error) {
	if query.ProfileSource == "" {
		return nil, errors.New("transition query requires a Profile source")
	}
	if len(query.Paths) == 0 {
		return nil, errors.New("transition query requires at least one path")
	}
	paths, err := json.Marshal(query.Paths)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(contentTransitionStatement, query.ProfileSource, string(paths))
	if err != nil {
		return nil, fmt.Errorf("query content transitions: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	transitions = []ContentTransition{}
	for rows.Next() {
		var transition ContentTransition
		if err := rows.Scan(&transition.Review, &transition.Lifecycle, &transition.CreatedAt, &transition.Path, &transition.Before, &transition.After); err != nil {
			return nil, err
		}
		transitions = append(transitions, transition)
	}
	return transitions, rows.Err()
}

func (s *DeferredLedgerRecordStore) ContentTransitions(query TransitionQuery) ([]ContentTransition, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.ContentTransitions(query)
}
