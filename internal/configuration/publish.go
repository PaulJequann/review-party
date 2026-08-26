package configuration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	globalDirectoryPermissions     fs.FileMode = 0o700
	repositoryDirectoryPermissions fs.FileMode = 0o755
	globalFilePermissions          fs.FileMode = 0o600
	repositoryFilePermissions      fs.FileMode = 0o644
)

type publicationPlan struct {
	files    []pendingWrite
	profiles []pendingProfilePublication
}

type publicationModule struct {
	writeFile    func(*pendingWrite) error
	writeProfile func(*pendingProfilePublication) error
}

func newPublicationModule() *publicationModule {
	return &publicationModule{writeFile: writeAtomically, writeProfile: writeProfileAtomically}
}

type pendingProfilePublication struct {
	scope        Scope
	anchor       string
	directory    string
	metadata     []byte
	instructions []byte
	complete     bool
}

type pendingWrite struct {
	scope    Scope
	anchor   string
	path     string
	payload  []byte
	backup   []byte
	existed  bool
	mode     fs.FileMode
	complete bool
	skip     bool
}

// Publish writes a confirmed plan as one transaction. The private publication
// module owns stale checks, rooted staging, commit, synchronization, and rollback.
func (manager *Manager) Publish(plan Plan) error {
	if !plan.Valid() {
		return fmt.Errorf("refuse to publish an invalid change plan: %s", plan.Reason())
	}
	return manager.publication.publish(plan.state.publication)
}

type publicationAdapter[T any] struct {
	prepare     func(*T) error
	commit      func(*T) error
	rollback    func(T) error
	description func(T) string
}

func (module *publicationModule) publish(plan publicationPlan) error {
	files, err := publishBatch(plan.files, module.fileAdapter())
	if err != nil {
		return err
	}
	if _, err := publishBatch(plan.profiles, module.profileAdapter()); err != nil {
		return errors.Join(err, rollbackBatch(files, module.fileAdapter()))
	}
	return nil
}

func publishBatch[T any](planned []T, adapter publicationAdapter[T]) ([]T, error) {
	items := append([]T(nil), planned...)
	if err := prepareBatch(items, adapter); err != nil {
		return nil, err
	}
	return commitBatch(items, adapter)
}

func prepareBatch[T any](items []T, adapter publicationAdapter[T]) error {
	for index := range items {
		if err := adapter.prepare(&items[index]); err != nil {
			return err
		}
	}
	return nil
}

func commitBatch[T any](items []T, adapter publicationAdapter[T]) ([]T, error) {
	for index := range items {
		if err := adapter.commit(&items[index]); err != nil {
			failure := fmt.Errorf("publish %s: %w", adapter.description(items[index]), err)
			return nil, errors.Join(failure, rollbackBatch(items[:index+1], adapter))
		}
	}
	return items, nil
}

func rollbackBatch[T any](items []T, adapter publicationAdapter[T]) error {
	var failures []error
	for index := len(items) - 1; index >= 0; index-- {
		if err := adapter.rollback(items[index]); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (module *publicationModule) fileAdapter() publicationAdapter[pendingWrite] {
	return publicationAdapter[pendingWrite]{
		prepare: (*pendingWrite).prepare,
		commit: func(write *pendingWrite) error {
			if write.skip {
				return nil
			}
			return module.writeFile(write)
		},
		rollback: func(write pendingWrite) error {
			if write.skip || !write.complete {
				return nil
			}
			return restoreFile(write)
		},
		description: func(write pendingWrite) string { return fmt.Sprintf("configuration %q", write.path) },
	}
}

func (module *publicationModule) profileAdapter() publicationAdapter[pendingProfilePublication] {
	return publicationAdapter[pendingProfilePublication]{
		prepare: (*pendingProfilePublication).prepare,
		commit:  module.writeProfile,
		rollback: func(profile pendingProfilePublication) error {
			if !profile.complete {
				return nil
			}
			return removeProfilePublication(profile)
		},
		description: func(profile pendingProfilePublication) string { return fmt.Sprintf("Profile %q", profile.directory) },
	}
}

func (write *pendingWrite) prepare() error {
	current, mode, existed, err := currentPlanTarget(write.anchor, write.path, fileState{existed: write.existed, payload: write.backup})
	if err != nil {
		return err
	}
	write.mode = mode
	write.skip = existed && bytes.Equal(current, write.payload)
	return nil
}

func (profile *pendingProfilePublication) prepare() error {
	if _, err := os.Lstat(profile.directory); err == nil {
		return fmt.Errorf("refuse to publish stale change plan: Profile %q changed after planning", profile.directory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func currentPlanTarget(anchor, path string, baseline fileState) ([]byte, fs.FileMode, bool, error) {
	current, mode, existed, err := readCurrentContents(anchor, path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("refuse to publish stale change plan for configuration %q: %w", path, err)
	}
	if existed != baseline.existed || !bytes.Equal(current, baseline.payload) {
		return nil, 0, false, fmt.Errorf("refuse to publish stale change plan: configuration %q changed after planning", path)
	}
	return current, mode, existed, nil
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

func writeProfileAtomically(publication *pendingProfilePublication) error {
	root, err := os.OpenRoot(publication.anchor)
	if err != nil {
		return err
	}
	defer root.Close()
	relative, err := filepath.Rel(publication.anchor, publication.directory)
	if err != nil {
		return err
	}
	temporary, err := stageProfileDirectory(root, relative, publication)
	if err != nil {
		return err
	}
	defer root.RemoveAll(temporary)
	if err := root.Rename(temporary, relative); err != nil {
		return fmt.Errorf("publish Profile %q: %w", publication.directory, err)
	}
	publication.complete = true
	return syncRootedDirectory(root, filepath.Dir(relative))
}

func stageProfileDirectory(root *os.Root, relative string, publication *pendingProfilePublication) (string, error) {
	parent := filepath.Dir(relative)
	if err := root.MkdirAll(parent, directoryPermissions(publication.scope)); err != nil {
		return "", fmt.Errorf("create Profile parent %q: %w", filepath.Join(publication.anchor, parent), err)
	}
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	temporary := filepath.Join(parent, ".review-party-profile-"+suffix+".tmp")
	if err := root.Mkdir(temporary, directoryPermissions(publication.scope)); err != nil {
		return "", err
	}
	if err := writeRootedProfilePart(root, filepath.Join(temporary, "profile.json"), publication.metadata, publication.scope); err != nil {
		root.RemoveAll(temporary)
		return "", err
	}
	if err := writeRootedProfilePart(root, filepath.Join(temporary, "instructions.md"), publication.instructions, publication.scope); err != nil {
		root.RemoveAll(temporary)
		return "", err
	}
	return temporary, nil
}

func randomSuffix() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate temporary Profile name: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func writeRootedProfilePart(root *os.Root, path string, payload []byte, scope Scope) error {
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePermissions(scope))
	if err != nil {
		return err
	}
	return writeTemporaryPayload(file, payload, filePermissions(scope))
}

func syncRootedDirectory(root *os.Root, path string) error {
	directory, err := root.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func writeAtomically(write *pendingWrite) error {
	root, err := os.OpenRoot(write.anchor)
	if err != nil {
		return err
	}
	defer root.Close()
	relative, err := filepath.Rel(write.anchor, write.path)
	if err != nil {
		return err
	}
	directory := filepath.Dir(relative)
	if err := root.MkdirAll(directory, directoryPermissions(write.scope)); err != nil {
		return fmt.Errorf("create configuration directory %q: %w", filepath.Join(write.anchor, directory), err)
	}
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	temporary := filepath.Join(directory, ".review-party-config-"+suffix+".tmp")
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePermissions(write.scope))
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if err := writeTemporaryPayload(file, write.payload, filePermissions(write.scope)); err != nil {
		return err
	}
	if err := root.Rename(temporary, relative); err != nil {
		return fmt.Errorf("publish configuration %q: %w", write.path, err)
	}
	write.complete = true
	return syncRootedDirectory(root, directory)
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

func removeProfilePublication(publication pendingProfilePublication) error {
	root, err := os.OpenRoot(publication.anchor)
	if err != nil {
		return err
	}
	defer root.Close()
	relative, err := filepath.Rel(publication.anchor, publication.directory)
	if err != nil {
		return err
	}
	if err := root.RemoveAll(relative); err != nil {
		return err
	}
	return syncRootedDirectory(root, filepath.Dir(relative))
}

func restoreFile(write pendingWrite) error {
	root, err := os.OpenRoot(write.anchor)
	if err != nil {
		return err
	}
	defer root.Close()
	relative, err := filepath.Rel(write.anchor, write.path)
	if err != nil {
		return err
	}
	directory := filepath.Dir(relative)
	if !write.existed {
		return removeRootedWrite(root, relative, directory)
	}
	return restoreRootedWrite(root, relative, directory, write)
}

func removeRootedWrite(root *os.Root, relative, directory string) error {
	if err := root.Remove(relative); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncRootedDirectory(root, directory)
}

func restoreRootedWrite(root *os.Root, relative, directory string, write pendingWrite) error {
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	temporary := filepath.Join(directory, ".review-party-rollback-"+suffix+".tmp")
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, write.mode)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if err := writeTemporaryPayload(file, write.backup, write.mode); err != nil {
		return err
	}
	if err := root.Rename(temporary, relative); err != nil {
		return err
	}
	return syncRootedDirectory(root, directory)
}

func directoryPermissions(scope Scope) fs.FileMode {
	if scope == ScopeGlobal {
		return globalDirectoryPermissions
	}
	return repositoryDirectoryPermissions
}

func filePermissions(scope Scope) fs.FileMode {
	if scope == ScopeGlobal {
		return globalFilePermissions
	}
	return repositoryFilePermissions
}
