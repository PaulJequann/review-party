package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"reviewparty/internal/model"
)

// RecordCheckpointWaiver stores one Checkpoint Waiver.
func (s *LedgerRecordStore) RecordCheckpointWaiver(waiver model.CheckpointWaiver) error {
	_, err := s.db.Exec(`INSERT INTO checkpoint_waivers(id,checkpoint,content_digest,repository,reason,waived_by,created_at) VALUES(?,?,?,?,?,?,?)`,
		waiver.ID, waiver.Key.Checkpoint, waiver.Key.ContentDigest, waiver.Repository, waiver.Reason, waiver.WaivedBy, waiver.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("record checkpoint waiver %q: %w", waiver.ID, err)
	}
	return nil
}

// CheckpointWaiver returns the Waiver recorded for exactly this key that the
// most policies accept: the newest terminal Waiver, else the newest of any
// kind. Every policy that accepts a non-interactive Waiver accepts a terminal
// one too.
func (s *LedgerRecordStore) CheckpointWaiver(key model.WaiverKey) (model.CheckpointWaiver, bool, error) {
	waiver := model.CheckpointWaiver{Key: key}
	err := s.db.QueryRow(`SELECT id,repository,reason,waived_by,created_at FROM checkpoint_waivers
		WHERE checkpoint = ? AND content_digest = ? ORDER BY waived_by = ? DESC, created_at DESC, id DESC LIMIT 1`,
		key.Checkpoint, key.ContentDigest, model.WaivedByTerminal).
		Scan(&waiver.ID, &waiver.Repository, &waiver.Reason, &waiver.WaivedBy, &waiver.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CheckpointWaiver{}, false, nil
	}
	if err != nil {
		return model.CheckpointWaiver{}, false, fmt.Errorf("find checkpoint waiver: %w", err)
	}
	return waiver, true, nil
}

// CheckpointWaiversSince lists the Waivers recorded from one repository root
// at or after since, newest first.
func (s *LedgerRecordStore) CheckpointWaiversSince(repository string, since time.Time) (_ []model.CheckpointWaiver, returnErr error) {
	rows, err := s.db.Query(`SELECT id,checkpoint,content_digest,reason,waived_by,created_at FROM checkpoint_waivers
		WHERE repository = ? AND created_at >= ? ORDER BY created_at DESC, id DESC`, repository, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("list checkpoint waivers: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	waivers := []model.CheckpointWaiver{}
	for rows.Next() {
		waiver := model.CheckpointWaiver{Repository: repository}
		if err := rows.Scan(&waiver.ID, &waiver.Key.Checkpoint, &waiver.Key.ContentDigest, &waiver.Reason, &waiver.WaivedBy, &waiver.CreatedAt); err != nil {
			return nil, fmt.Errorf("read checkpoint waiver: %w", err)
		}
		waivers = append(waivers, waiver)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list checkpoint waivers: %w", err)
	}
	return waivers, nil
}

func (s *DeferredLedgerRecordStore) RecordCheckpointWaiver(waiver model.CheckpointWaiver) error {
	ledger, err := s.openExisting()
	if err != nil {
		return err
	}
	return ledger.RecordCheckpointWaiver(waiver)
}

func (s *DeferredLedgerRecordStore) CheckpointWaiver(key model.WaiverKey) (model.CheckpointWaiver, bool, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return model.CheckpointWaiver{}, false, err
	}
	return ledger.CheckpointWaiver(key)
}

func (s *DeferredLedgerRecordStore) CheckpointWaiversSince(repository string, since time.Time) ([]model.CheckpointWaiver, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.CheckpointWaiversSince(repository, since)
}
