package store

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func ensureStateDirectory(directory string, prepare bool) error {
	info, err := os.Stat(directory)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("review state path %q is not a directory", directory)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("access review state directory %q: %w", directory, err)
	}
	if !prepare {
		return fmt.Errorf("%w at %q", ErrReviewRecordStateNotInitialized, directory)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create review state directory: %w", err)
	}
	return nil
}

func validateStateDirectory(directory string) error {
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("access review state directory %q: %w", directory, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("review state path %q is not a directory", directory)
	}
	return nil
}

func validateExistingLedger(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open review ledger %q: %w", path, err)
	}
	defer file.Close()
	header := make([]byte, 16)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("review ledger %q is corrupt: %w", path, err)
	}
	if string(header) != "SQLite format 3\x00" {
		return fmt.Errorf("review ledger %q is corrupt: invalid SQLite header", path)
	}
	return nil
}

func prepareLedgerDirectory(directory string, prepare bool) error {
	ledgerPath := filepath.Join(directory, ledgerFilename)
	if err := rejectSymlinkedStatePath(ledgerPath); err != nil {
		return err
	}
	if err := ensureStateDirectory(directory, prepare); err != nil {
		return err
	}
	if err := rejectSymlinkedStatePath(ledgerPath); err != nil {
		return err
	}
	return validateStateDirectory(directory)
}

func rejectSymlinkedStatePath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve review state path %q: %w", path, err)
	}
	for candidate := absolute; ; candidate = filepath.Dir(candidate) {
		if err := inspectStatePathComponent(candidate); err != nil {
			return err
		}
		if filepath.Dir(candidate) == candidate {
			return nil
		}
	}
}

func inspectStatePathComponent(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("review state path %q must not contain symlinks", path)
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil
	}
	return fmt.Errorf("inspect review state path %q: %w", path, err)
}
