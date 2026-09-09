package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIncompatibleStateBackupPreservesLedgerAndSidecarsBeforeFreshInit(t *testing.T) {
	directory := t.TempDir()
	original := map[string][]byte{
		ledgerFilename:              []byte("retired-ledger-bytes"),
		ledgerFilename + "-wal":     []byte("retired-wal-bytes"),
		ledgerFilename + "-shm":     []byte("retired-shm-bytes"),
		ledgerFilename + "-journal": []byte("retired-journal-bytes"),
	}
	for name, payload := range original {
		if err := os.WriteFile(filepath.Join(directory, name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	backup, err := BackupIncompatibleReviewRecordState(directory)
	if err != nil {
		t.Fatal(err)
	}
	for name, wanted := range original {
		got, err := os.ReadFile(filepath.Join(backup.Directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wanted) {
			t.Fatalf("backup %s = %q, want %q", name, got, wanted)
		}
	}
	ready, err := ReviewRecordStatePrepared(directory)
	if err != nil || ready {
		t.Fatalf("state prepared after backup = %t, %v", ready, err)
	}
	pending, err := ReviewRecordStateRecoveryPending(directory)
	if err != nil || !pending {
		t.Fatalf("recovery pending = %t, %v", pending, err)
	}
	walBackup := filepath.Join(backup.Directory, ledgerFilename+"-wal")
	walState := filepath.Join(directory, ledgerFilename+"-wal")
	if err := os.Rename(walBackup, walState); err != nil {
		t.Fatal(err)
	}
	if err := PrepareFreshReviewRecordState(directory); err == nil {
		t.Fatal("fresh initialization accepted a sidecar outside backup")
	}
	if _, err := BackupIncompatibleReviewRecordState(directory); err != nil {
		t.Fatalf("resume backup: %v", err)
	}
	if err := PrepareFreshReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}
	pending, err = ReviewRecordStateRecoveryPending(directory)
	if err != nil || pending {
		t.Fatalf("recovery pending after fresh init = %t, %v", pending, err)
	}
}

func TestFreshPreparationRequiresPriorBackup(t *testing.T) {
	if err := PrepareFreshReviewRecordState(t.TempDir()); err == nil {
		t.Fatal("fresh initialization succeeded without backup")
	}
}

func TestFreshPreparationCompletesInterruptedState(t *testing.T) {
	directory := t.TempDir()
	original := map[string][]byte{
		ledgerFilename:          []byte("retired-ledger-bytes"),
		ledgerFilename + "-wal": []byte("retired-wal-bytes"),
	}
	for name, payload := range original {
		if err := os.WriteFile(filepath.Join(directory, name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := BackupIncompatibleReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the fresh ledger was prepared but before the
	// recovery marker was removed.
	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}
	if err := PrepareFreshReviewRecordState(directory); err != nil {
		t.Fatalf("retry after interrupted fresh initialization: %v", err)
	}
	pending, err := ReviewRecordStateRecoveryPending(directory)
	if err != nil || pending {
		t.Fatalf("recovery pending after completed fresh init = %t, %v", pending, err)
	}
	ready, err := ReviewRecordStatePrepared(directory)
	if err != nil || !ready {
		t.Fatalf("state prepared after completed fresh init = %t, %v", ready, err)
	}
}
