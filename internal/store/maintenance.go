package store

import (
	"database/sql"
	"fmt"
)

// recordPendingMaintenance marks, inside the upgrade transaction, the work an
// in-place upgrade leaves for init: compaction and the evidence sweep.
func recordPendingMaintenance(tx *sql.Tx) error {
	if _, err := tx.Exec("INSERT OR IGNORE INTO pending_maintenance VALUES(1)"); err != nil {
		return fmt.Errorf("record review ledger maintenance: %w", err)
	}
	return nil
}

func (s *LedgerRecordStore) requireNoPendingMaintenance() error {
	var pending bool
	if err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM pending_maintenance)").Scan(&pending); err != nil {
		return fmt.Errorf("read review ledger maintenance: %w", err)
	}
	if pending {
		return fmt.Errorf("%w: review ledger upgrade to schema %d has unfinished maintenance; run review-party init", errLedgerUpgradesInPlace, currentLedgerSchemaVersion)
	}
	return nil
}

// CompleteMaintenance returns the pages an in-place upgrade freed to the
// filesystem and then clears the pending marker, so a run interrupted before
// the marker is gone repeats the whole maintenance. VACUUM cannot run inside
// the migration transaction, and it rewrites the database through the
// write-ahead log, so the log is truncated last.
func (s *LedgerRecordStore) CompleteMaintenance() error {
	for _, statement := range []string{"VACUUM", "DELETE FROM pending_maintenance", "PRAGMA wal_checkpoint(TRUNCATE)"} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("complete review ledger maintenance: %w", err)
		}
	}
	return nil
}
