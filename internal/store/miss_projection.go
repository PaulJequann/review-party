package store

import (
	"database/sql"
	"errors"
	"fmt"
	"reviewparty/internal/model"
	"strings"
	"time"
)

type MissStore interface {
	RecordMisses(records []MissRecord) ([]model.Miss, error)
	ListMisses(query MissQuery) ([]model.Miss, error)
	RemoveMisses(ids []model.MissID, removal model.MissRemoval) ([]MissRemovalOutcome, error)
}

type MissRecord struct {
	ID         model.MissID
	ReviewID   model.ReviewID
	Report     model.MissReport
	RecordedAt time.Time
}

type MissQuery struct {
	Repository     string
	Profile        string
	ReviewID       model.ReviewID
	IncludeRemoved bool
}

type MissRemovalStatus string

const (
	MissRemoved        MissRemovalStatus = "removed"
	MissAlreadyRemoved MissRemovalStatus = "already-removed"
)

type MissRemovalOutcome struct {
	ID      model.MissID      `json:"id"`
	Status  MissRemovalStatus `json:"status"`
	Removal model.MissRemoval `json:"removal"`
}

type missProjection struct{ db *sql.DB }

const missSelect = `SELECT m.id,m.review_id,
	json_extract(r.subject,'$.repository'),json_extract(r.subject,'$.kind'),json_extract(r.subject,'$.identity'),
	json_extract(r.profile_revision,'$.name'),
	m.path,m.line,m.source,m.description,m.recorded_by,m.recorded_at,
	m.removed_at,m.removed_by,m.removal_reason
	FROM misses m JOIN reviews r ON r.id=m.review_id`

func (p missProjection) record(records []MissRecord) (misses []model.Miss, returnErr error) {
	tx, err := p.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackTransaction(tx)) }()
	misses = make([]model.Miss, 0, len(records))
	for _, record := range records {
		miss, err := insertMiss(tx, record)
		if err != nil {
			return nil, err
		}
		misses = append(misses, miss)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return misses, nil
}

func insertMiss(tx *sql.Tx, record MissRecord) (model.Miss, error) {
	var line any
	if record.Report.Location.Line > 0 {
		line = record.Report.Location.Line
	}
	report := record.Report
	result, err := tx.Exec(`INSERT INTO misses(id,review_id,path,line,source,description,recorded_by,recorded_at)
		SELECT ?,id,?,?,?,?,?,? FROM reviews WHERE id=?`,
		record.ID, report.Location.Path, line, report.Source, report.Description, report.RecordedBy, record.RecordedAt.UTC(), record.ReviewID)
	if err != nil {
		return model.Miss{}, fmt.Errorf("record miss %q: %w", record.ID, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return model.Miss{}, err
	}
	if inserted == 0 {
		return model.Miss{}, fmt.Errorf("record miss %q: no review with id %q", record.ID, record.ReviewID)
	}
	miss, err := scanMiss(tx.QueryRow(missSelect+" WHERE m.id=?", record.ID))
	if err != nil {
		return model.Miss{}, fmt.Errorf("read recorded miss %q: %w", record.ID, err)
	}
	return miss, nil
}

func (p missProjection) list(query MissQuery) (misses []model.Miss, returnErr error) {
	statement, arguments := buildMissListQuery(query)
	rows, err := p.db.Query(statement, arguments...)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	misses = []model.Miss{}
	for rows.Next() {
		miss, err := scanMiss(rows)
		if err != nil {
			return nil, err
		}
		misses = append(misses, miss)
	}
	return misses, rows.Err()
}

func buildMissListQuery(query MissQuery) (string, []any) {
	var predicates []string
	var arguments []any
	for _, filter := range []queryFilter{
		{"json_extract(r.subject,'$.repository') = ?", query.Repository, query.Repository != ""},
		{"json_extract(r.profile_revision,'$.name') = ?", query.Profile, query.Profile != ""},
		{"m.review_id = ?", query.ReviewID, query.ReviewID != ""},
	} {
		if filter.enabled {
			predicates = append(predicates, filter.predicate)
			arguments = append(arguments, filter.value)
		}
	}
	if !query.IncludeRemoved {
		predicates = append(predicates, "m.removed_at IS NULL")
	}
	statement := missSelect
	if len(predicates) > 0 {
		statement += " WHERE " + strings.Join(predicates, " AND ")
	}
	return statement + " ORDER BY m.recorded_at,m.rowid", arguments
}

func (p missProjection) remove(ids []model.MissID, removal model.MissRemoval) (outcomes []MissRemovalOutcome, returnErr error) {
	tx, err := p.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackTransaction(tx)) }()
	outcomes = make([]MissRemovalOutcome, 0, len(ids))
	for _, id := range ids {
		outcome, err := tombstoneMiss(tx, id, removal)
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return outcomes, nil
}

func tombstoneMiss(tx *sql.Tx, id model.MissID, removal model.MissRemoval) (MissRemovalOutcome, error) {
	miss, err := scanMiss(tx.QueryRow(missSelect+" WHERE m.id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return MissRemovalOutcome{}, fmt.Errorf("no miss with id %q", id)
	}
	if err != nil {
		return MissRemovalOutcome{}, fmt.Errorf("read miss %q: %w", id, err)
	}
	if miss.Removal != nil {
		return MissRemovalOutcome{ID: id, Status: MissAlreadyRemoved, Removal: *miss.Removal}, nil
	}
	removal.RemovedAt = removal.RemovedAt.UTC()
	if _, err := tx.Exec(`UPDATE misses SET removed_at=?,removed_by=?,removal_reason=? WHERE id=?`, removal.RemovedAt, removal.RemovedBy, removal.Reason, id); err != nil {
		return MissRemovalOutcome{}, fmt.Errorf("remove miss %q: %w", id, err)
	}
	return MissRemovalOutcome{ID: id, Status: MissRemoved, Removal: removal}, nil
}

type missScanner interface {
	Scan(destinations ...any) error
}

func scanMiss(row missScanner) (model.Miss, error) {
	var miss model.Miss
	var source string
	var line sql.NullInt64
	var removedAt sql.NullTime
	var removedBy, removalReason sql.NullString
	if err := row.Scan(&miss.ID, &miss.ReviewID, &miss.Repository, &miss.SubjectKind, &miss.SubjectIdentity, &miss.Profile,
		&miss.Location.Path, &line, &source, &miss.Description, &miss.RecordedBy, &miss.RecordedAt,
		&removedAt, &removedBy, &removalReason); err != nil {
		return model.Miss{}, err
	}
	parsed, err := model.ParseMissSource(source)
	if err != nil {
		return model.Miss{}, fmt.Errorf("read miss %q: %w", miss.ID, err)
	}
	miss.Source = parsed
	miss.Location.Line = int(line.Int64)
	if removedAt.Valid {
		miss.Removal = &model.MissRemoval{Reason: removalReason.String, RemovedBy: removedBy.String, RemovedAt: removedAt.Time}
	}
	return miss, nil
}
