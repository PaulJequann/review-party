package configuration

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// LoadedDocument reports one scope's authored configuration. Present is false
// when no file exists; Path is empty in that case and no file is ever created
// by loading or resolving defaults.
type LoadedDocument struct {
	Scope    Scope
	Path     string
	Present  bool
	Document Document
}

// Loaded carries both configuration scopes for one repository request.
type Loaded struct {
	Personal   LoadedDocument
	Repository LoadedDocument
}

// Load reads the authored Personal Configuration and, when a repository is
// supplied, the authored Repository Configuration. Loading never creates a
// file. A malformed or semantically invalid document fails closed with an
// InvalidDocumentError naming the file.
func (manager *Manager) Load(repository Repository) (Loaded, error) {
	personal, err := manager.loadScope(ScopePersonal, repository)
	if err != nil {
		return Loaded{}, err
	}
	loaded := Loaded{Personal: personal}
	if repository != "" {
		repositoryDocument, err := manager.loadScope(ScopeRepository, repository)
		if err != nil {
			return Loaded{}, err
		}
		loaded.Repository = repositoryDocument
	}
	return loaded, nil
}

func (manager *Manager) loadScope(scope Scope, repository Repository) (LoadedDocument, error) {
	path, anchor, err := manager.configPathAndAnchor(scope, repository)
	if errors.Is(err, ErrPersonalRootUnavailable) {
		return LoadedDocument{Scope: scope, Document: Document{SchemaVersion: SchemaVersion}}, nil
	}
	if err != nil {
		return LoadedDocument{}, err
	}
	payload, found, err := readRegularFile(anchor, path, fmt.Sprintf("%s configuration", scope), maximumDocumentBytes)
	if err != nil {
		return LoadedDocument{}, err
	}
	if !found {
		return LoadedDocument{Scope: scope, Document: Document{SchemaVersion: SchemaVersion}}, nil
	}
	var document Document
	if err := strictDecode(payload, &document); err != nil {
		return LoadedDocument{}, invalid(scope, path, err)
	}
	if err := validateDocument(document, scope, manager); err != nil {
		return LoadedDocument{}, invalid(scope, path, err)
	}
	return LoadedDocument{Scope: scope, Path: path, Present: true, Document: document}, nil
}

// readRegularFile reads a regular file below anchor without following
// symlinks, reporting found=false for absent files.
func readRegularFile(anchor, path, description string, maximumBytes int64) ([]byte, bool, error) {
	relative, err := filepath.Rel(anchor, path)
	if err != nil {
		return nil, false, fmt.Errorf("resolve %s path %q: %w", description, path, err)
	}
	root, found, err := openConfigurationRoot(anchor, description)
	if err != nil || !found {
		return nil, found, err
	}
	defer root.Close()
	return readRootedRegularFile(root, fileReadRequest{
		relative: relative, path: path, description: description, maximumBytes: maximumBytes,
	})
}

func openConfigurationRoot(anchor, description string) (*os.Root, bool, error) {
	root, err := os.OpenRoot(anchor)
	if errors.Is(err, fs.ErrNotExist) {
		// A missing configuration home simply means nothing is authored.
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open %s root %q: %w", description, anchor, err)
	}
	return root, true, nil
}

type fileReadRequest struct {
	relative     string
	path         string
	description  string
	maximumBytes int64
}

func readRootedRegularFile(root *os.Root, request fileReadRequest) ([]byte, bool, error) {
	info, err := root.Lstat(request.relative)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect %s at %q: %w", request.description, request.path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s at %q must be a regular file, not a symlink or special file", request.description, request.path)
	}
	file, err := root.Open(request.relative)
	if err != nil {
		return nil, false, fmt.Errorf("open %s at %q: %w", request.description, request.path, err)
	}
	defer file.Close()
	return readBoundedPayload(file, request)
}

func readBoundedPayload(file *os.File, request fileReadRequest) ([]byte, bool, error) {
	payload, err := io.ReadAll(io.LimitReader(file, request.maximumBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("read %s at %q: %w", request.description, request.path, err)
	}
	if int64(len(payload)) > request.maximumBytes {
		return nil, false, fmt.Errorf("%s at %q exceeds %d bytes", request.description, request.path, request.maximumBytes)
	}
	return payload, true, nil
}
