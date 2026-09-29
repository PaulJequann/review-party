package store

import (
	"database/sql"
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"time"
)

type VerdictStore interface {
	RecordVerdicts(batch VerdictBatch) (VerdictTally, error)
	ListVerdicts(query VerdictQuery) ([]model.FindingVerdict, error)
}

// VerdictBatch is every Judgment a Caller records against one review at once.
// The batch commits whole or not at all.
type VerdictBatch struct {
	ReviewID   model.ReviewID
	Judgments  []model.Judgment
	RecordedBy string
	RecordedAt time.Time
}

// VerdictTally counts a batch's Judgments. Recorded counts new ledger rows,
// Changed counts the recorded ones that superseded a different current Verdict,
// and Unchanged counts repeats of the current Verdict, which write nothing.
type VerdictTally struct {
	Recorded  int `json:"recorded"`
	Unchanged int `json:"unchanged"`
	Changed   int `json:"changed"`
}

type VerdictQuery struct {
	Repository string
	Profile    string
	ReviewIDs  []model.ReviewID
}

// MissingFindingError reports a Judgment whose ordinal names no Finding of its
// review. Finding ordinals always run 1 to Findings.
type MissingFindingError struct {
	Review    model.ReviewID
	Lifecycle model.Lifecycle
	Findings  int
	Ordinal   int
}

func (e MissingFindingError) Error() string {
	review := fmt.Sprintf("review %s (%s)", e.Review, e.Lifecycle)
	switch e.Findings {
	case 0:
		return review + " has no findings"
	case 1:
		return fmt.Sprintf("%s has finding 1, not %d", review, e.Ordinal)
	}
	return fmt.Sprintf("%s has findings 1-%d, not %d", review, e.Findings, e.Ordinal)
}

type verdictProjection struct{ db *sql.DB }

// judged is one ledger row's Verdict, compared to spot an identical repeat.
type judged struct {
	verdict model.Verdict
	reason  string
}

// verdictRow is one ledger row a batch adds.
type verdictRow struct {
	ordinal int
	digest  string
	judged
}

// judgedReview is a review as a batch finds it: the digest of each Finding it
// holds now and the current Verdict on it, by ordinal.
type judgedReview struct {
	id        model.ReviewID
	lifecycle model.Lifecycle
	digests   map[int]string
	current   map[int]judged
}

func (p verdictProjection) record(batch VerdictBatch) (tally VerdictTally, returnErr error) {
	tx, err := p.db.Begin()
	if err != nil {
		return VerdictTally{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackTransaction(tx)) }()
	review, err := readJudgedReview(tx, batch.ReviewID)
	if err != nil {
		return VerdictTally{}, err
	}
	rows, tally, err := review.plan(batch.Judgments)
	if err != nil {
		return VerdictTally{}, err
	}
	for _, row := range rows {
		if _, err := tx.Exec(`INSERT INTO finding_verdicts(review_id,ordinal,finding_digest,verdict,reason,recorded_by,recorded_at) VALUES(?,?,?,?,?,?,?)`,
			batch.ReviewID, row.ordinal, row.digest, row.verdict, row.reason, batch.RecordedBy, batch.RecordedAt.UTC()); err != nil {
			return VerdictTally{}, fmt.Errorf("record verdict on finding %d of review %q: %w", row.ordinal, batch.ReviewID, err)
		}
	}
	return tally, tx.Commit()
}

func readJudgedReview(tx *sql.Tx, id model.ReviewID) (judgedReview, error) {
	review := judgedReview{id: id}
	if err := tx.QueryRow("SELECT lifecycle FROM reviews WHERE id=?", id).Scan(&review.lifecycle); err != nil {
		return judgedReview{}, notFoundAs(string(id), err)
	}
	var err error
	if review.digests, err = liveDigests(tx, id); err != nil {
		return judgedReview{}, err
	}
	if review.current, err = currentJudgments(tx, id, review.digests); err != nil {
		return judgedReview{}, err
	}
	return review, nil
}

// plan returns the rows judgments add to the ledger and their tally. A repeat
// of the current Verdict adds no row, and a Judgment naming no Finding refuses
// the whole batch.
func (r judgedReview) plan(judgments []model.Judgment) ([]verdictRow, VerdictTally, error) {
	var rows []verdictRow
	var tally VerdictTally
	for _, judgment := range judgments {
		digest, ok := r.digests[judgment.Ordinal]
		if !ok {
			return nil, VerdictTally{}, MissingFindingError{Review: r.id, Lifecycle: r.lifecycle, Findings: len(r.digests), Ordinal: judgment.Ordinal}
		}
		next := judged{judgment.Verdict, judgment.Reason.String()}
		previous, hasCurrent := r.current[judgment.Ordinal]
		if hasCurrent && previous == next {
			tally.Unchanged++
			continue
		}
		rows = append(rows, verdictRow{judgment.Ordinal, digest, next})
		tally.Recorded++
		if hasCurrent {
			tally.Changed++
		}
	}
	return rows, tally, nil
}

// liveDigests reads the digest of every Finding a review holds now, by ordinal.
func liveDigests(tx *sql.Tx, review model.ReviewID) (digests map[int]string, returnErr error) {
	rows, err := tx.Query("SELECT ordinal,severity,category,location,failure,evidence,fix,test FROM findings WHERE review_id=?", review)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	digests = map[int]string{}
	for rows.Next() {
		var finding model.Finding
		if err := rows.Scan(&finding.Ordinal, &finding.Severity, &finding.Category, &finding.Location, &finding.Failure, &finding.Evidence, &finding.Fix, &finding.Test); err != nil {
			return nil, err
		}
		digests[finding.Ordinal] = model.FindingDigest(finding)
	}
	return digests, rows.Err()
}

// currentJudgments reads the current Verdict per ordinal: the latest one
// recorded against the text the Finding holds now.
func currentJudgments(tx *sql.Tx, review model.ReviewID, digests map[int]string) (current map[int]judged, returnErr error) {
	rows, err := tx.Query("SELECT ordinal,finding_digest,verdict,reason FROM finding_verdicts WHERE review_id=? ORDER BY seq", review)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	current = map[int]judged{}
	for rows.Next() {
		var ordinal int
		var digest string
		var row judged
		if err := rows.Scan(&ordinal, &digest, &row.verdict, &row.reason); err != nil {
			return nil, err
		}
		if digests[ordinal] == digest {
			current[ordinal] = row
		}
	}
	return current, rows.Err()
}

const verdictSelect = `SELECT v.review_id,json_extract(r.subject,'$.repository'),json_extract(r.profile_revision,'$.name'),
	v.ordinal,v.finding_digest,v.verdict,v.reason,v.recorded_by,v.recorded_at,
	f.severity,f.category,f.location,f.failure,f.evidence,f.fix,f.test
	FROM finding_verdicts v JOIN reviews r ON r.id=v.review_id
	LEFT JOIN findings f ON f.review_id=v.review_id AND f.ordinal=v.ordinal`

// list returns one FindingVerdict per judged Finding: its current Verdict, or,
// when no Verdict matches the Finding's text now, its latest Verdict marked Stale.
func (p verdictProjection) list(query VerdictQuery) (verdicts []model.FindingVerdict, returnErr error) {
	reviews, err := reviewIDsFilter("v.review_id", query.ReviewIDs)
	if err != nil {
		return nil, err
	}
	where, arguments := whereClause([]queryFilter{
		{"json_extract(r.subject,'$.repository') = ?", query.Repository, query.Repository != ""},
		{"json_extract(r.profile_revision,'$.name') = ?", query.Profile, query.Profile != ""},
		reviews,
	})
	rows, err := p.db.Query(verdictSelect+where+" ORDER BY r.created_at,v.review_id,v.ordinal,v.seq", arguments...)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	verdicts = []model.FindingVerdict{}
	for rows.Next() {
		verdict, err := scanVerdict(rows)
		if err != nil {
			return nil, err
		}
		verdicts = keepCurrent(verdicts, verdict)
	}
	return verdicts, rows.Err()
}

// keepCurrent adds a ledger Verdict, read in recorded order per Finding, to
// verdicts, which keep one per Finding: a later Verdict replaces the kept one
// unless the later one is Stale and the kept one is not.
func keepCurrent(verdicts []model.FindingVerdict, verdict model.FindingVerdict) []model.FindingVerdict {
	last := len(verdicts) - 1
	if last < 0 || !sameFinding(verdicts[last], verdict) {
		return append(verdicts, verdict)
	}
	if !verdict.Stale || verdicts[last].Stale {
		verdicts[last] = verdict
	}
	return verdicts
}

func sameFinding(a, b model.FindingVerdict) bool {
	return a.ReviewID == b.ReviewID && a.Ordinal == b.Ordinal
}

// scanVerdict reads one ledger Verdict, Stale unless it judged the text its
// Finding holds now. The Verdict carries the live Finding's location.
func scanVerdict(row missScanner) (model.FindingVerdict, error) {
	var verdict model.FindingVerdict
	var digest string
	var text [7]sql.NullString
	if err := row.Scan(&verdict.ReviewID, &verdict.Repository, &verdict.Profile,
		&verdict.Ordinal, &digest, &verdict.Verdict, &verdict.Reason, &verdict.RecordedBy, &verdict.RecordedAt,
		&text[0], &text[1], &text[2], &text[3], &text[4], &text[5], &text[6]); err != nil {
		return model.FindingVerdict{}, err
	}
	live := model.Finding{
		Severity: text[0].String, Category: text[1].String, Location: text[2].String,
		Failure: text[3].String, Evidence: text[4].String, Fix: text[5].String, Test: text[6].String,
	}
	verdict.Location = live.Location
	verdict.Stale = !text[0].Valid || digest != model.FindingDigest(live)
	return verdict, nil
}
