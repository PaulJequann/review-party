package configuration

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	personalDirectoryPermissions   fs.FileMode = 0o700
	repositoryDirectoryPermissions fs.FileMode = 0o755
	personalFilePermissions        fs.FileMode = 0o600
	repositoryFilePermissions      fs.FileMode = 0o644
)

type pendingWrite struct {
	scope    Scope
	path     string
	payload  []byte
	backup   []byte
	existed  bool
	mode     fs.FileMode
	complete bool
}

// Publish writes every staged document of a confirmed plan atomically.
// Personal files are private (0600, directories 0700). If any file fails to
// publish, the previously published files are restored to their pre-save
// contents and an error is returned; a partially accepted configuration is
// never reported as success.
func (manager *Manager) Publish(plan Plan) error {
	if !plan.Valid() {
		return fmt.Errorf("refuse to publish an invalid change plan: %s", plan.Reason())
	}
	writes, err := manager.prepareWrites(plan)
	if err != nil {
		return err
	}
	if len(writes) == 0 {
		return nil
	}
	for index := range writes {
		if err := manager.publishWrite(&writes[index]); err != nil {
			err = fmt.Errorf("publish configuration %q: %w", writes[index].path, err)
			return errors.Join(err, rollbackCompleted(writes[:index+1]))
		}
	}
	return nil
}

func (manager *Manager) prepareWrites(plan Plan) ([]pendingWrite, error) {
	writes := make([]pendingWrite, 0, len(plan.state.staged))
	for _, document := range plan.state.staged {
		payload, err := renderDocument(document.document)
		if err != nil {
			return nil, err
		}
		current, mode, existed, err := readCurrentContents(document.anchor, document.path)
		if err != nil {
			return nil, fmt.Errorf("refuse to publish stale change plan for configuration %q: %w", document.path, err)
		}
		if existed != document.baseline.existed || !bytes.Equal(current, document.baseline.payload) {
			return nil, fmt.Errorf("refuse to publish stale change plan: configuration %q changed after planning", document.path)
		}
		if existed && bytes.Equal(current, payload) {
			continue
		}
		writes = append(writes, pendingWrite{scope: document.scope, path: document.path, payload: payload, backup: current, existed: existed, mode: mode})
	}
	return writes, nil
}

func readCurrentContents(anchor, path string) ([]byte, fs.FileMode, bool, error) {
	payload, existed, err := readRegularFile(anchor, path, "configuration", MaximumDocumentBytes)
	if err != nil {
		return nil, 0, false, err
	}
	if !existed {
		return nil, 0, false, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("inspect permissions for configuration %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, 0, false, fmt.Errorf("configuration %q changed while its publication was being prepared", path)
	}
	return payload, info.Mode().Perm(), true, nil
}

func writeAtomically(write *pendingWrite) error {
	directory := filepath.Dir(write.path)
	if err := os.MkdirAll(directory, directoryPermissions(write.scope)); err != nil {
		return fmt.Errorf("create configuration directory %q: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, ".review-party-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	defer os.Remove(temporary.Name())
	if err := writeTemporaryPayload(temporary, write.payload, filePermissions(write.scope)); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), write.path); err != nil {
		return fmt.Errorf("publish configuration %q: %w", write.path, err)
	}
	write.complete = true
	return syncDirectory(directory)
}

func writeTemporaryPayload(file *os.File, payload []byte, permissions fs.FileMode) error {
	if err := file.Chmod(permissions); err != nil {
		file.Close()
		return fmt.Errorf("restrict temporary configuration: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	return file.Close()
}

// rollbackCompleted restores the pre-save state after a failed publication.
func rollbackCompleted(completed []pendingWrite) error {
	var failures []error
	for index := len(completed) - 1; index >= 0; index-- {
		write := completed[index]
		if !write.complete {
			continue
		}
		if err := restoreFile(write); err != nil {
			failures = append(failures, fmt.Errorf("restore previous configuration %q: %w", write.path, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("configuration publication failed and rollback was incomplete; manual recovery is required: %w", errors.Join(failures...))
	}
	return nil
}

func restoreFile(write pendingWrite) error {
	if !write.existed {
		if err := os.Remove(write.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(write.path))
	}
	temporary, err := os.CreateTemp(filepath.Dir(write.path), ".review-party-rollback-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err := writeTemporaryPayload(temporary, write.backup, write.mode); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), write.path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(write.path))
}

func directoryPermissions(scope Scope) fs.FileMode {
	if scope == ScopePersonal {
		return personalDirectoryPermissions
	}
	return repositoryDirectoryPermissions
}

func filePermissions(scope Scope) fs.FileMode {
	if scope == ScopePersonal {
		return personalFilePermissions
	}
	return repositoryFilePermissions
}

func syncDirectory(directory string) error {
	opened, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open configuration directory for sync: %w", err)
	}
	defer opened.Close()
	if err := opened.Sync(); err != nil {
		return fmt.Errorf("sync configuration directory: %w", err)
	}
	return nil
}
