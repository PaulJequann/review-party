// Package artifact stores review artifacts.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reviewparty/internal/model"
)

// Directory is the state subdirectory that holds published artifacts.
const Directory = "artifacts"

// temporaryPattern names an artifact being written. One left behind is a
// partial write from a process that died before publishing it.
const temporaryPattern = ".artifact-*.tmp"

// IsTemporary reports whether entry is an unpublished artifact write.
func IsTemporary(entry fs.DirEntry) bool {
	matched, err := filepath.Match(temporaryPattern, entry.Name())
	return err == nil && matched && entry.Type().IsRegular()
}

// The kinds of evidence an Attempt may publish.
const (
	AssistantText = "assistant-text"
	ReviewerNoise = "reviewer-noise"
)

// Evidence is one stream of an Attempt's raw output, already bounded by the
// caller, which reports whether it cut the stream short.
type Evidence struct {
	Kind      string
	Contents  []byte
	Truncated bool
}

type Store struct{ root string }

func NewStore(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("artifact root is required")
	}
	return &Store{root: filepath.Clean(root)}, nil
}

func (store *Store) Publish(reviewID model.ReviewID, attempt int, evidence Evidence) (reference model.ArtifactReference, returnErr error) {
	kind, contents := evidence.Kind, evidence.Contents
	if attempt < 1 || !validKind(kind) {
		return model.ArtifactReference{}, errors.New("invalid artifact identity")
	}
	relative := filepath.Join(Directory, string(reviewID), fmt.Sprintf("%d", attempt), kind+".txt")
	path, err := store.resolve(relative)
	if err != nil {
		return model.ArtifactReference{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return model.ArtifactReference{}, fmt.Errorf("create artifact directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), temporaryPattern)
	if err != nil {
		return model.ArtifactReference{}, fmt.Errorf("create temporary artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		returnErr = errors.Join(returnErr, removeTemporaryArtifact(temporaryPath))
	}()
	if err := writeTemporaryArtifact(temporary, contents); err != nil {
		return model.ArtifactReference{}, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return model.ArtifactReference{}, fmt.Errorf("publish artifact: %w", err)
	}
	digest := sha256.Sum256(contents)
	return model.ArtifactReference{Kind: kind, Path: relative, Size: int64(len(contents)), Digest: hex.EncodeToString(digest[:]), Truncated: evidence.Truncated}, nil
}

func writeTemporaryArtifact(file *os.File, contents []byte) error {
	if err := file.Chmod(0o600); err != nil {
		return errors.Join(fmt.Errorf("restrict temporary artifact: %w", err), file.Close())
	}
	if _, err := file.Write(contents); err != nil {
		return errors.Join(fmt.Errorf("write temporary artifact: %w", err), file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync temporary artifact: %w", err), file.Close())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary artifact: %w", err)
	}
	return nil
}

func removeTemporaryArtifact(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove temporary artifact: %w", err)
	}
	return nil
}

func (store *Store) Read(reference model.ArtifactReference) ([]byte, error) {
	path, err := store.resolve(reference.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read artifact %q: %w", reference.Path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact %q is not a regular file", reference.Path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read artifact %q: %w", reference.Path, err)
	}
	digest := sha256.Sum256(contents)
	if int64(len(contents)) != reference.Size || hex.EncodeToString(digest[:]) != reference.Digest {
		return nil, fmt.Errorf("artifact %q failed integrity check", reference.Path)
	}
	return contents, nil
}

// Remove deletes an artifact and then each directory above it, up to the
// artifacts directory, that the removal left empty.
func (store *Store) Remove(reference model.ArtifactReference) error {
	path, err := store.resolve(reference.Path)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove artifact %q: %w", reference.Path, err)
	}
	return store.pruneEmptyDirectories(reference)
}

func (store *Store) pruneEmptyDirectories(reference model.ArtifactReference) error {
	for directory := filepath.Dir(reference.Path); directory != Directory && directory != "."; directory = filepath.Dir(directory) {
		path := filepath.Join(store.root, directory)
		entries, err := os.ReadDir(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || len(entries) > 0 {
			return err
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove empty artifact directory: %w", err)
		}
	}
	return nil
}

func (store *Store) resolve(relative string) (string, error) {
	if !filepath.IsLocal(relative) {
		return "", fmt.Errorf("artifact path %q escapes root", relative)
	}
	return filepath.Join(store.root, relative), nil
}

func validKind(kind string) bool {
	return kind == AssistantText || kind == ReviewerNoise
}
