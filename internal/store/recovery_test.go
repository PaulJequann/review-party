package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testEvidenceDirectory = "artifacts"

func TestIncompatibleStateBackupPreservesLedgerAndSidecarsBeforeFreshInit(t *testing.T) {
	directory := t.TempDir()
	original := map[string][]byte{
		ledgerFilename:              []byte("retired-ledger-bytes"),
		ledgerFilename + "-wal":     []byte("retired-wal-bytes"),
		ledgerFilename + "-shm":     []byte("retired-shm-bytes"),
		ledgerFilename + "-journal": []byte("retired-journal-bytes"),
	}
	writeStateFiles(t, directory, original)

	backup, err := BackupIncompatibleReviewRecordState(directory, testEvidenceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	assertBackupHolds(t, backup, original)
	assertRecoveryState(t, directory, recoveryState{pending: true})
	walBackup := filepath.Join(backup.Directory, ledgerFilename+"-wal")
	walState := filepath.Join(directory, ledgerFilename+"-wal")
	if err := os.Rename(walBackup, walState); err != nil {
		t.Fatal(err)
	}
	if err := PrepareFreshReviewRecordState(directory, testEvidenceDirectory); err == nil {
		t.Fatal("fresh initialization accepted a sidecar outside backup")
	}
	if _, err := BackupIncompatibleReviewRecordState(directory, testEvidenceDirectory); err != nil {
		t.Fatalf("resume backup: %v", err)
	}
	if err := PrepareFreshReviewRecordState(directory, testEvidenceDirectory); err != nil {
		t.Fatal(err)
	}
	assertRecoveryState(t, directory, recoveryState{prepared: true})
}

func TestIncompatibleStateBackupTakesTheLedgersEvidence(t *testing.T) {
	directory := t.TempDir()
	evidence := filepath.Join(testEvidenceDirectory, "rp_1_retired", "1", "constructed-prompt.txt")
	original := map[string][]byte{ledgerFilename: []byte("retired-ledger-bytes"), evidence: []byte("retired-evidence-bytes")}
	writeStateFiles(t, directory, original)

	backup, err := BackupIncompatibleReviewRecordState(directory, testEvidenceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	assertEvidenceBackedUp(t, directory, backup, original)

	interrupted := filepath.Join(directory, testEvidenceDirectory)
	if err := os.Rename(filepath.Join(backup.Directory, testEvidenceDirectory), interrupted); err != nil {
		t.Fatal(err)
	}
	if backup, err = BackupIncompatibleReviewRecordState(directory, testEvidenceDirectory); err != nil {
		t.Fatalf("resume backup: %v", err)
	}
	assertEvidenceBackedUp(t, directory, backup, original)
}

func TestFreshPreparationFinishesAnInterruptedEvidenceMove(t *testing.T) {
	directory := t.TempDir()
	evidence := filepath.Join(testEvidenceDirectory, "rp_1_retired", "1", "assistant-text.txt")
	original := map[string][]byte{ledgerFilename: []byte("retired-ledger-bytes"), evidence: []byte("retired-evidence-bytes")}
	writeStateFiles(t, directory, original)
	backup, err := BackupIncompatibleReviewRecordState(directory, testEvidenceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(backup.Directory, testEvidenceDirectory), filepath.Join(directory, testEvidenceDirectory)); err != nil {
		t.Fatal(err)
	}

	if err := PrepareFreshReviewRecordState(directory, testEvidenceDirectory); err != nil {
		t.Fatal(err)
	}
	assertEvidenceBackedUp(t, directory, backup, original)
	assertRecoveryState(t, directory, recoveryState{prepared: true})
}

func TestIncompatibleStateBackupRestoresTheLedgerWhenTheEvidenceCannotMove(t *testing.T) {
	directory := t.TempDir()
	original := map[string][]byte{ledgerFilename: []byte("retired-ledger-bytes"), ledgerFilename + "-wal": []byte("retired-wal-bytes")}
	writeStateFiles(t, directory, original)
	evidence := filepath.Join(directory, testEvidenceDirectory)
	if err := os.Mkdir(evidence, 0o500); err != nil {
		t.Fatal(err)
	}

	if _, err := BackupIncompatibleReviewRecordState(directory, testEvidenceDirectory); err == nil {
		t.Fatal("backup succeeded although the evidence could not move")
	}
	for name, wanted := range original {
		if got, err := os.ReadFile(filepath.Join(directory, name)); err != nil || !bytes.Equal(got, wanted) {
			t.Fatalf("restored %s = %q, %v; want %q", name, got, err, wanted)
		}
	}
	if pending, err := ReviewRecordStateRecoveryPending(directory); err != nil || pending {
		t.Fatalf("recovery pending after a rolled-back backup = %t, %v", pending, err)
	}
}

type recoveryState struct {
	prepared bool
	pending  bool
}

func assertRecoveryState(t *testing.T, directory string, want recoveryState) {
	t.Helper()
	prepared, err := ReviewRecordStatePrepared(directory)
	if err != nil && !errors.Is(err, ErrReviewRecordStateRequiresPreparation) {
		t.Fatal(err)
	}
	pending, err := ReviewRecordStateRecoveryPending(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := (recoveryState{prepared: prepared, pending: pending}); got != want {
		t.Fatalf("recovery state = %+v, want %+v", got, want)
	}
}

func writeStateFiles(t *testing.T, directory string, files map[string][]byte) {
	t.Helper()
	for name, payload := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func assertBackupHolds(t *testing.T, backup StateBackup, files map[string][]byte) {
	t.Helper()
	for name, wanted := range files {
		got, err := os.ReadFile(filepath.Join(backup.Directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wanted) {
			t.Fatalf("backup %s = %q, want %q", name, got, wanted)
		}
	}
}

func assertEvidenceBackedUp(t *testing.T, directory string, backup StateBackup, original map[string][]byte) {
	t.Helper()
	assertBackupHolds(t, backup, original)
	if _, err := os.Lstat(filepath.Join(directory, testEvidenceDirectory)); !os.IsNotExist(err) {
		t.Fatalf("the backed-up ledger's evidence stayed in live state: %v", err)
	}
	if ledger := filepath.Join(backup.Directory, ledgerFilename); !slices.Contains(backup.Files, ledger) {
		t.Fatalf("backup files = %v, want the ledger %s", backup.Files, ledger)
	}
}

func TestFreshPreparationRequiresPriorBackup(t *testing.T) {
	if err := PrepareFreshReviewRecordState(t.TempDir(), testEvidenceDirectory); err == nil {
		t.Fatal("fresh initialization succeeded without backup")
	}
}

func TestFreshPreparationCompletesInterruptedState(t *testing.T) {
	directory := t.TempDir()
	original := map[string][]byte{
		ledgerFilename:          []byte("retired-ledger-bytes"),
		ledgerFilename + "-wal": []byte("retired-wal-bytes"),
	}
	writeStateFiles(t, directory, original)
	if _, err := BackupIncompatibleReviewRecordState(directory, testEvidenceDirectory); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the fresh ledger was prepared but before the
	// recovery marker was removed.
	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}
	if err := PrepareFreshReviewRecordState(directory, testEvidenceDirectory); err != nil {
		t.Fatalf("retry after interrupted fresh initialization: %v", err)
	}
	assertRecoveryState(t, directory, recoveryState{prepared: true})
}

func TestIncompatibleStateBackupRefusesUpgradableLedger(t *testing.T) {
	directory := t.TempDir()
	writeLedgerAtSchema(t, directory, 10)

	_, err := BackupIncompatibleReviewRecordState(directory, testEvidenceDirectory)
	if err == nil {
		t.Fatal("backup of an upgradable ledger succeeded")
	}
	if message := err.Error(); !strings.Contains(message, "compatible") {
		t.Fatalf("backup error = %v, want it to call the ledger compatible", err)
	}
	if message := err.Error(); !strings.Contains(message, "run review-party init") {
		t.Fatalf("backup error = %v, want it to name review-party init", err)
	}
	if pending, err := ReviewRecordStateRecoveryPending(directory); err != nil || pending {
		t.Fatalf("recovery pending after refused backup = %t, %v", pending, err)
	}
	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatalf("prepare after refused backup = %v", err)
	}
	if version := readSchemaVersion(t, directory); version != currentLedgerSchemaVersion {
		t.Fatalf("schema version after prepare = %d, want %d", version, currentLedgerSchemaVersion)
	}
}

func TestIsLedgerFileCoversWhatPreparationAndRecoveryLeave(t *testing.T) {
	prepared := t.TempDir()
	if err := PrepareReviewRecordState(prepared); err != nil {
		t.Fatal(err)
	}
	assertLedgerFiles(t, prepared)

	recovering := t.TempDir()
	for _, suffix := range ledgerSuffixes {
		if err := os.WriteFile(filepath.Join(recovering, ledgerFilename+suffix), []byte("retired"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := BackupIncompatibleReviewRecordState(recovering, testEvidenceDirectory); err != nil {
		t.Fatal(err)
	}
	assertLedgerFiles(t, recovering)
	if IsLedgerFile("ledger.sqlite.bak") || IsLedgerFile("notes.txt") {
		t.Fatal("IsLedgerFile accepted a name the ledger never writes")
	}
}

func assertLedgerFiles(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if want := entry.Name() != BackupDirectory; IsLedgerFile(entry.Name()) != want {
			t.Errorf("IsLedgerFile(%q) = %t, want %t", entry.Name(), !want, want)
		}
	}
}
