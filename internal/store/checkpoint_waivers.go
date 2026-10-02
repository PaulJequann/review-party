package store

import (
	"database/sql"
	"errors"
	"fmt"

	"reviewparty/internal/model"
)

// RecordCheckpointWaiver stores one Checkpoint Waiver.
func (s *LedgerRecordStore) RecordCheckpointWaiver(waiver model.CheckpointWaiver) error {
	_, err := s.db.Exec(`INSERT INTO checkpoint_waivers(id,checkpoint,content_digest,reason,waived_by,created_at) VALUES(?,?,?,?,?,?)`,
		waiver.ID, waiver.Key.Checkpoint, waiver.Key.ContentDigest, waiver.Reason, waiver.WaivedBy, waiver.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("record checkpoint waiver %q: %w", waiver.ID, err)
	}
	return nil
}

// CheckpointWaiver returns the newest Waiver recorded for exactly this key.
func (s *LedgerRecordStore) CheckpointWaiver(key model.WaiverKey) (model.CheckpointWaiver, bool, error) {
	waiver := model.CheckpointWaiver{Key: key}
	err := s.db.QueryRow(`SELECT id,reason,waived_by,created_at FROM checkpoint_waivers
		WHERE checkpoint = ? AND content_digest = ? ORDER BY created_at DESC, id DESC LIMIT 1`, key.Checkpoint, key.ContentDigest).
		Scan(&waiver.ID, &waiver.Reason, &waiver.WaivedBy, &waiver.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CheckpointWaiver{}, false, nil
	}
	if err != nil {
		return model.CheckpointWaiver{}, false, fmt.Errorf("find checkpoint waiver: %w", err)
	}
	return waiver, true, nil
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
