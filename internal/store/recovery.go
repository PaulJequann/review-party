package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const stateBackupDirectory = "backups"
const recoveryMarker = ".recovery-pending"

type StateBackup struct {
	Directory string   `json:"directory"`
	Files     []string `json:"files"`
}

// BackupIncompatibleReviewRecordState moves an unusable ledger and every
// present SQLite sidecar together. It never prepares or migrates new state.
func BackupIncompatibleReviewRecordState(directory string) (StateBackup, error) {
	if pending, err := ReviewRecordStateRecoveryPending(directory); err != nil {
		return StateBackup{}, err
	} else if pending {
		return resumeStateBackup(directory)
	}
	ready, checkErr := ReviewRecordStatePrepared(directory)
	if ready {
		return StateBackup{}, errors.New("review ledger is compatible; backup recovery is not required")
	}
	ledger := filepath.Join(directory, ledgerFilename)
	if _, err := os.Lstat(ledger); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StateBackup{}, errors.New("incompatible review ledger was not found")
		}
		return StateBackup{}, err
	}
	if checkErr == nil {
		return StateBackup{}, errors.New("review ledger is not known to be incompatible")
	}
	if !recoverableIncompatibility(checkErr) {
		return StateBackup{}, checkErr
	}
	backup := filepath.Join(directory, stateBackupDirectory, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(backup, 0o700); err != nil {
		return StateBackup{}, fmt.Errorf("create review state backup: %w", err)
	}
	result := StateBackup{Directory: backup}
	marker := filepath.Join(directory, recoveryMarker)
	if err := os.WriteFile(marker, []byte(backup+"\n"), 0o600); err != nil {
		return StateBackup{}, fmt.Errorf("record pending fresh initialization: %w", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		name := ledgerFilename + suffix
		source := filepath.Join(directory, name)
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return StateBackup{}, rollbackStateBackup(directory, result, marker, fmt.Errorf("inspect review state file %q: %w", source, err))
		}
		target := filepath.Join(backup, name)
		if err := os.Rename(source, target); err != nil {
			return StateBackup{}, rollbackStateBackup(directory, result, marker, fmt.Errorf("back up review state file %q: %w", source, err))
		}
		result.Files = append(result.Files, target)
	}
	return result, nil
}

func resumeStateBackup(directory string) (StateBackup, error) {
	payload, err := os.ReadFile(filepath.Join(directory, recoveryMarker))
	if err != nil {
		return StateBackup{}, err
	}
	backup := strings.TrimSpace(string(payload))
	if filepath.Dir(filepath.Dir(backup)) != directory || filepath.Base(filepath.Dir(backup)) != stateBackupDirectory {
		return StateBackup{}, errors.New("invalid pending recovery backup path")
	}
	result := StateBackup{Directory: backup}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		name := ledgerFilename + suffix
		source, target := filepath.Join(directory, name), filepath.Join(backup, name)
		if _, err := os.Lstat(target); err == nil {
			result.Files = append(result.Files, target)
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return StateBackup{}, err
		}
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return StateBackup{}, err
		}
		if err := os.Rename(source, target); err != nil {
			return StateBackup{}, err
		}
		result.Files = append(result.Files, target)
	}
	if len(result.Files) == 0 {
		return StateBackup{}, errors.New("pending recovery contains no backed-up ledger files")
	}
	return result, nil
}

func recoverableIncompatibility(err error) bool {
	if errors.Is(err, errLedgerUpgradesInPlace) {
		return false
	}
	return errors.Is(err, ErrReviewRecordStateRequiresPreparation) ||
		strings.Contains(err.Error(), "newer than supported schema") ||
		strings.Contains(err.Error(), "is corrupt")
}

func rollbackStateBackup(directory string, backup StateBackup, marker string, cause error) error {
	var rollbackErrs []error
	for index := len(backup.Files) - 1; index >= 0; index-- {
		path := backup.Files[index]
		if err := os.Rename(path, filepath.Join(directory, filepath.Base(path))); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore %q: %w", path, err))
		}
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		rollbackErrs = append(rollbackErrs, err)
	}
	return errors.Join(append([]error{cause}, rollbackErrs...)...)
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

func PrepareFreshReviewRecordState(directory string) error {
	pending, err := ReviewRecordStateRecoveryPending(directory)
	if err != nil {
		return err
	}
	if !pending {
		return errors.New("fresh initialization requires a preceding incompatible-ledger backup")
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		path := filepath.Join(directory, ledgerFilename+suffix)
		if _, err := os.Lstat(path); err == nil {
			// A prepared state beside a pending marker is an interrupted
			// completion (prepared usable state, then exited before the
			// marker was dropped, including a failed marker removal). It
			// cannot be the backed-up original, which was unusable by
			// definition, so dropping the marker completes the retry.
			// Anything else could be unrestored originals: refuse and
			// direct the operator to --backup first.
			ready, prepErr := ReviewRecordStatePrepared(directory)
			if prepErr != nil {
				return fmt.Errorf("fresh initialization refused while review state file %q remains outside backup", path)
			}
			if !ready {
				return fmt.Errorf("fresh initialization refused while review state file %q remains outside backup", path)
			}
			if err := os.Remove(filepath.Join(directory, recoveryMarker)); err != nil {
				return fmt.Errorf("complete fresh initialization: %w", err)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := PrepareReviewRecordState(directory); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(directory, recoveryMarker)); err != nil {
		return fmt.Errorf("complete fresh initialization: %w", err)
	}
	return nil
}
