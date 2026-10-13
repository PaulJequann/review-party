package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"reviewparty/internal/model"
)

// Stats reports usage of the Reviews, Review Bundles, and Checkpoint Waivers
// in one window. It reads inside one read-only transaction, so every section
// describes the same ledger state.
func (s *LedgerRecordStore) Stats(query StatsQuery) (_ Stats, returnErr error) {
	if !query.Since.Before(query.Now) {
		return Stats{}, errors.New("stats window must start before it ends")
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Stats{}, fmt.Errorf("begin stats read: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackTransaction(tx)) }()
	rows, err := readStatsRows(tx, query)
	if err != nil {
		return Stats{}, fmt.Errorf("read stats: %w", err)
	}
	return summarizeStats(query, rows), nil
}

func (s *DeferredLedgerRecordStore) Stats(query StatsQuery) (Stats, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return Stats{}, err
	}
	return ledger.Stats(query)
}

// reviewWindow restricts a statement that names the reviews table r.
func reviewWindow(query StatsQuery) (string, []any) {
	return whereClause([]queryFilter{
		{"r.created_at >= ?", query.Since.UTC(), true},
		{"json_extract(r.subject,'$.repository') = ?", query.Repository, query.Repository != ""},
	})
}

func readStatsRows(tx *sql.Tx, query StatsQuery) (rows statsRows, err error) {
	selections, err := readBundleSelections(tx, query)
	if err != nil {
		return statsRows{}, err
	}
	if rows.reviews, err = readStatsReviews(tx, query, selections); err != nil {
		return statsRows{}, err
	}
	if rows.failures, err = readStatsFailures(tx, query); err != nil {
		return statsRows{}, err
	}
	if rows.severities, err = readCounts[string](tx, query, `SELECT lower(f.severity),COUNT(*) FROM findings f JOIN reviews r ON r.id=f.review_id`, "GROUP BY 1"); err != nil {
		return statsRows{}, err
	}
	if rows.verdicts, err = readCounts[model.Verdict](tx, query, `SELECT v.verdict,COUNT(*) FROM finding_verdicts v JOIN reviews r ON r.id=v.review_id
		JOIN findings f ON f.review_id=v.review_id AND f.ordinal=v.ordinal
		AND v.seq=(SELECT MAX(seq) FROM finding_verdicts c WHERE c.review_id=v.review_id AND c.ordinal=v.ordinal)`, "GROUP BY 1"); err != nil {
		return statsRows{}, err
	}
	if rows.waivers, err = readWaiverCounts(tx, query); err != nil {
		return statsRows{}, err
	}
	rows.bundles, err = readStatsBundles(tx, query)
	return rows, err
}

func readStatsReviews(tx *sql.Tx, query StatsQuery, selections map[model.ReviewID]string) (_ []statsReview, returnErr error) {
	where, arguments := reviewWindow(query)
	result, err := tx.Query(`SELECT r.id,json_extract(r.subject,'$.repository'),json_extract(r.profile_revision,'$.name'),
		COALESCE(json_extract(r.profile_revision,'$.reviewer_id'),''),r.lifecycle,COALESCE(r.result_status,''),
		COALESCE(json_extract(r.termination,'$.category'),''),COALESCE(json_extract(r.timings,'$.total_ms'),0),r.created_at
		FROM reviews r`+where, arguments...)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, result.Close()) }()
	reviews := []statsReview{}
	for result.Next() {
		var id model.ReviewID
		var review statsReview
		if err := result.Scan(&id, &review.repository, &review.profile, &review.reviewer, &review.lifecycle, &review.status, &review.termination, &review.totalMS, &review.createdAt); err != nil {
			return nil, err
		}
		review.selection = selections[id]
		if review.selection == "" {
			review.selection = unbundledSelection
		}
		reviews = append(reviews, review)
	}
	return reviews, result.Err()
}

// readBundleSelections maps each member Review to how its Bundle chose
// Profiles. A Bundle is created just before its members, so the lookup
// reaches one day before the window to find the Bundles of its first Reviews.
func readBundleSelections(tx *sql.Tx, query StatsQuery) (_ map[model.ReviewID]string, returnErr error) {
	result, err := tx.Query(`SELECT COALESCE(json_extract(selection,'$.kind'),'unknown'),members FROM review_bundles WHERE created_at >= ?`, query.Since.Add(-24*time.Hour).UTC())
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, result.Close()) }()
	selections := map[model.ReviewID]string{}
	for result.Next() {
		var kind string
		var encoded []byte
		if err := result.Scan(&kind, &encoded); err != nil {
			return nil, err
		}
		if err := addBundleSelection(selections, kind, encoded); err != nil {
			return nil, err
		}
	}
	return selections, result.Err()
}

func addBundleSelection(selections map[model.ReviewID]string, kind string, encodedMembers []byte) error {
	var members []model.BundleMember
	if err := json.Unmarshal(encodedMembers, &members); err != nil {
		return fmt.Errorf("decode review bundle members: %w", err)
	}
	for _, member := range members {
		selections[member.ReviewID] = kind
	}
	delete(selections, "")
	return nil
}

func readStatsFailures(tx *sql.Tx, query StatsQuery) (_ []statsFailure, returnErr error) {
	where, arguments := reviewWindow(query)
	result, err := tx.Query(`SELECT a.outcome,a.diagnostic,a.completed_at FROM attempts a JOIN reviews r ON r.id=a.review_id`+where+` AND a.outcome <> ?`,
		append(arguments, model.AttemptCompleted)...)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, result.Close()) }()
	failures := []statsFailure{}
	for result.Next() {
		var failure statsFailure
		if err := result.Scan(&failure.outcome, &failure.diagnostic, &failure.completedAt); err != nil {
			return nil, err
		}
		failures = append(failures, failure)
	}
	return failures, result.Err()
}

// readCounts runs a two-column name/count statement over the Reviews in the
// window; the statement must join reviews as r.
func readCounts[Name ~string](tx *sql.Tx, query StatsQuery, statement, suffix string) (map[Name]int, error) {
	where, arguments := reviewWindow(query)
	return scanCounts[Name](tx, strings.Join([]string{statement, where, suffix}, " "), arguments)
}

func readWaiverCounts(tx *sql.Tx, query StatsQuery) (map[string]int, error) {
	where, arguments := whereClause([]queryFilter{
		{"created_at >= ?", query.Since.UTC(), true},
		{"repository = ?", query.Repository, query.Repository != ""},
	})
	return scanCounts[string](tx, "SELECT waived_by,COUNT(*) FROM checkpoint_waivers"+where+" GROUP BY 1", arguments)
}

func scanCounts[Name ~string](tx *sql.Tx, statement string, arguments []any) (_ map[Name]int, returnErr error) {
	result, err := tx.Query(statement, arguments...)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, result.Close()) }()
	counts := map[Name]int{}
	for result.Next() {
		var name Name
		var count int
		if err := result.Scan(&name, &count); err != nil {
			return nil, err
		}
		counts[name] = count
	}
	return counts, result.Err()
}

func readStatsBundles(tx *sql.Tx, query StatsQuery) (_ []statsBundle, returnErr error) {
	where, arguments := whereClause([]queryFilter{
		{"created_at >= ?", query.Since.UTC(), true},
		{"repository = ?", query.Repository, query.Repository != ""},
	})
	result, err := tx.Query("SELECT lifecycle FROM review_bundles"+where, arguments...)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, result.Close()) }()
	bundles := []statsBundle{}
	for result.Next() {
		var bundle statsBundle
		if err := result.Scan(&bundle.lifecycle); err != nil {
			return nil, err
		}
		bundles = append(bundles, bundle)
	}
	return bundles, result.Err()
}
