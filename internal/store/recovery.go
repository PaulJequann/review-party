package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type StateBackup struct {
	Directory string   `json:"directory"`
	Files     []string `json:"files"`
}

// BackupIncompatibleReviewRecordState moves an unusable ledger, every present
// SQLite sidecar, and evidence, the state subdirectory holding the files that
// ledger references, together. It never prepares or migrates new state.
func BackupIncompatibleReviewRecordState(directory, evidence string) (StateBackup, error) {
	recovery := stateRecovery{directory: directory, evidence: evidence}
	if pending, err := ReviewRecordStateRecoveryPending(directory); err != nil {
		return StateBackup{}, err
	} else if pending {
		return recovery.resume()
	}
	if err := requireRecoverableLedger(directory); err != nil {
		return StateBackup{}, err
	}
	return recovery.begin()
}

type stateRecovery struct {
	directory string
	evidence  string
}

func requireRecoverableLedger(directory string) error {
	ready, checkErr := ReviewRecordStatePrepared(directory)
	if ready {
		return errors.New("review ledger is compatible; backup recovery is not required")
	}
	if _, err := os.Lstat(filepath.Join(directory, ledgerFilename)); errors.Is(err, os.ErrNotExist) {
		return errors.New("incompatible review ledger was not found")
	} else if err != nil {
		return err
	}
	if checkErr == nil {
		return errors.New("review ledger is not known to be incompatible")
	}
	if !recoverableIncompatibility(checkErr) {
		return checkErr
	}
	return nil
}

func (recovery stateRecovery) begin() (StateBackup, error) {
	backup := StateBackup{Directory: filepath.Join(recovery.directory, BackupDirectory, time.Now().UTC().Format("20060102T150405.000000000Z"))}
	if err := os.MkdirAll(backup.Directory, 0o700); err != nil {
		return StateBackup{}, fmt.Errorf("create review state backup: %w", err)
	}
	if err := os.WriteFile(recovery.marker(), []byte(backup.Directory+"\n"), 0o600); err != nil {
		return StateBackup{}, fmt.Errorf("record pending fresh initialization: %w", err)
	}
	if err := recovery.collect(&backup); err != nil {
		return StateBackup{}, recovery.rollback(backup, err)
	}
	return backup, nil
}

func (recovery stateRecovery) marker() string {
	return filepath.Join(recovery.directory, recoveryMarker)
}

// collect moves the ledger, its sidecars, and the evidence into the backup.
// An entry already in the backup is one an interrupted backup moved.
func (recovery stateRecovery) collect(backup *StateBackup) error {
	var names []string
	for _, suffix := range ledgerSuffixes {
		names = append(names, ledgerFilename+suffix)
	}
	for _, name := range append(names, recovery.evidence) {
		source, target := filepath.Join(recovery.directory, name), filepath.Join(backup.Directory, name)
		if _, err := os.Lstat(target); err == nil {
			backup.Files = append(backup.Files, target)
			continue
		}
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Rename(source, target); err != nil {
			return fmt.Errorf("back up review state file %q: %w", source, err)
		}
		backup.Files = append(backup.Files, target)
	}
	return nil
}

func (recovery stateRecovery) resume() (StateBackup, error) {
	payload, err := os.ReadFile(recovery.marker())
	if err != nil {
		return StateBackup{}, err
	}
	backup := StateBackup{Directory: strings.TrimSpace(string(payload))}
	if filepath.Dir(filepath.Dir(backup.Directory)) != recovery.directory || filepath.Base(filepath.Dir(backup.Directory)) != BackupDirectory {
		return StateBackup{}, errors.New("invalid pending recovery backup path")
	}
	if err := recovery.collect(&backup); err != nil {
		return StateBackup{}, err
	}
	if len(backup.Files) == 0 {
		return StateBackup{}, errors.New("pending recovery contains no backed-up ledger files")
	}
	return backup, nil
}

func (recovery stateRecovery) rollback(backup StateBackup, cause error) error {
	var rollbackErrs []error
	for index := len(backup.Files) - 1; index >= 0; index-- {
		path := backup.Files[index]
		if err := os.Rename(path, filepath.Join(recovery.directory, filepath.Base(path))); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore %q: %w", path, err))
		}
	}
	if err := os.Remove(recovery.marker()); err != nil && !errors.Is(err, os.ErrNotExist) {
		rollbackErrs = append(rollbackErrs, err)
	}
	return errors.Join(append([]error{cause}, rollbackErrs...)...)
}

func recoverableIncompatibility(err error) bool {
	if errors.Is(err, errLedgerUpgradesInPlace) {
		return false
	}
	return errors.Is(err, ErrReviewRecordStateRequiresPreparation) ||
		strings.Contains(err.Error(), "newer than supported schema") ||
		strings.Contains(err.Error(), "is corrupt")
}

func ReviewRecordStateRecoveryPending(directory string) (bool, error) {
	_, err := os.Lstat(filepath.Join(directory, recoveryMarker))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func PrepareFreshReviewRecordState(directory, evidence string) error {
	pending, err := ReviewRecordStateRecoveryPending(directory)
	if err != nil {
		return err
	}
	if !pending {
		return errors.New("fresh initialization requires a preceding incompatible-ledger backup")
	}
	if err := (stateRecovery{directory: directory, evidence: evidence}).prepareFreshLedger(); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(directory, recoveryMarker)); err != nil {
		return fmt.Errorf("complete fresh initialization: %w", err)
	}
	return nil
}

// prepareFreshLedger prepares a ledger where the backup left none. A prepared
// ledger beside a pending marker is an interrupted completion (prepared usable
// state, then exited before the marker was dropped, including a failed marker
// removal). It cannot be the backed-up original, which was unusable by
// definition, so it is kept. Any other ledger file could be an unrestored
// original: refuse and direct the operator to --backup first. With no ledger
// file left, the backup is resumed first, because an interrupted backup can
// leave the evidence behind and a fresh ledger must not start beside files
// only the backed-up ledger references.
func (recovery stateRecovery) prepareFreshLedger() error {
	for _, suffix := range ledgerSuffixes {
		path := filepath.Join(recovery.directory, ledgerFilename+suffix)
		if _, err := os.Lstat(path); err == nil {
			if ready, err := ReviewRecordStatePrepared(recovery.directory); err != nil || !ready {
				return fmt.Errorf("fresh initialization refused while review state file %q remains outside backup", path)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if _, err := recovery.resume(); err != nil {
		return err
	}
	return PrepareReviewRecordState(recovery.directory)
}
