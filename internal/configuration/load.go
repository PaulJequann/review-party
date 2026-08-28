package configuration

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LoadedDocument reports one scope's authored configuration. Present is false
// when no file exists; Path is empty in that case and no file is ever created
// by loading or resolving defaults.
type LoadedDocument struct {
	Scope    Scope
	Path     string
	Present  bool
	Document Document

	payload []byte
}

// Payload returns a copy of the authored document bytes. It returns nil when
// the scope has no authored file.
func (document LoadedDocument) Payload() []byte {
	return append([]byte(nil), document.payload...)
}

// Loaded carries both configuration scopes for one repository request.
type Loaded struct {
	Global     LoadedDocument
	Repository LoadedDocument
}

// Load reads the authored Global Configuration and, when a repository is
// supplied, the authored Repository Configuration. Loading never creates a
// file. A malformed or semantically invalid document fails closed with an
// InvalidDocumentError naming the file.
func (manager *Manager) Load(repository Repository) (Loaded, error) {
	global, err := manager.loadScope(ScopeGlobal, repository)
	if err != nil {
		return Loaded{}, err
	}
	loaded := Loaded{Global: global}
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
	_, document, err := manager.readAuthoredFile(scope, repository)
	if err != nil {
		return LoadedDocument{}, err
	}
	return document, nil
}

func (manager *Manager) readAuthoredFile(scope Scope, repository Repository) (AuthoredFile, LoadedDocument, error) {
	file := AuthoredFile{Scope: scope}
	path, anchor, err := manager.configPathAndAnchor(scope, repository)
	if errors.Is(err, ErrGlobalRootUnavailable) {
		return file, emptyLoadedDocument(scope), nil
	}
	if err != nil {
		return file, LoadedDocument{}, err
	}
	file.Path = path
	payload, found, err := readRegularFile(anchor, path, fmt.Sprintf("%s configuration", scope), MaximumDocumentBytes)
	if err != nil {
		return file, LoadedDocument{}, err
	}
	if !found {
		return file, emptyLoadedDocument(scope), nil
	}
	file.Present = true
	var document Document
	if err := strictDecode(payload, &document); err != nil {
		return file, LoadedDocument{}, invalid(scope, path, err)
	}
	if err := validateDocument(document, scope, manager); err != nil {
		return file, LoadedDocument{}, invalid(scope, path, err)
	}
	file.payload = append([]byte(nil), payload...)
	file.trailingNewline = len(payload) > 0 && payload[len(payload)-1] == '\n'
	return file, LoadedDocument{Scope: scope, Path: path, Present: true, Document: document, payload: payload}, nil
}

func emptyLoadedDocument(scope Scope) LoadedDocument {
	return LoadedDocument{Scope: scope, Document: Document{SchemaVersion: SchemaVersion}}
}

// readRegularFile reads a regular file below anchor without following
// symlinks, reporting found=false for absent files.
func readRegularFile(anchor, path, description string, maximumBytes int64) ([]byte, bool, error) {
	return readRegularFileWith(anchor, path, description, maximumBytes, func(root *os.Root, name string) (*os.File, error) {
		return root.Open(name)
	})
}

func readRegularFileWith(anchor, path, description string, maximumBytes int64, open func(*os.Root, string) (*os.File, error)) ([]byte, bool, error) {
	relative, err := filepath.Rel(anchor, path)
	if err != nil {
		return nil, false, fmt.Errorf("resolve %s path %q: %w", description, path, err)
	}
	root, found, err := openConfigurationRoot(anchor, description)
	if err != nil || !found {
		return nil, found, err
	}
	defer root.Close()
	return readRootedRegularFileWith(root, fileReadRequest{
		relative: relative, path: path, description: description, maximumBytes: maximumBytes,
	}, open)
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

func readRootedRegularFileWith(root *os.Root, request fileReadRequest, open func(*os.Root, string) (*os.File, error)) ([]byte, bool, error) {
	info, err := inspectRootedPath(root, rootedPathRequest{
		relative: request.relative, path: request.path, description: request.description,
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect %s at %q: %w", request.description, request.path, err)
	}
	file, err := open(root, request.relative)
	if err != nil {
		return nil, false, fmt.Errorf("open %s at %q: %w", request.description, request.path, err)
	}
	defer file.Close()
	if err := verifyOpenedRegularFile(root, request, info, file); err != nil {
		return nil, false, err
	}
	return readBoundedPayload(file, request)
}

func verifyOpenedRegularFile(root *os.Root, request fileReadRequest, before fs.FileInfo, file *os.File) error {
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened %s at %q: %w", request.description, request.path, err)
	}
	after, err := inspectRootedPath(root, rootedPathRequest{
		relative: request.relative, path: request.path, description: request.description,
	})
	if err != nil {
		return err
	}
	if !sameRegularFile(before, opened, after) {
		return fmt.Errorf("%s at %q changed while it was being opened", request.description, request.path)
	}
	return nil
}

func sameRegularFile(before, opened, after fs.FileInfo) bool {
	return opened.Mode().IsRegular() && os.SameFile(before, opened) && os.SameFile(opened, after)
}

type directoryReadRequest struct {
	relative    string
	path        string
	description string
}

func readRootedDirectory(root *os.Root, request directoryReadRequest) ([]fs.DirEntry, bool, error) {
	if _, err := inspectRootedPath(root, rootedPathRequest{
		relative: request.relative, path: request.path, description: request.description, directory: true,
	}); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("inspect %s at %q: %w", request.description, request.path, err)
	}
	entries, err := fs.ReadDir(root.FS(), request.relative)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s at %q: %w", request.description, request.path, err)
	}
	return entries, true, nil
}

type rootedPathRequest struct {
	relative    string
	path        string
	description string
	directory   bool
}

func inspectRootedPath(root *os.Root, request rootedPathRequest) (fs.FileInfo, error) {
	clean := filepath.Clean(request.relative)
	if clean != "." {
		current := ""
		components := strings.Split(clean, string(filepath.Separator))
		for index, component := range components {
			current = filepath.Join(current, component)
			info, err := root.Lstat(current)
			if err != nil {
				return nil, err
			}
			if index == len(components)-1 {
				return validateRootedLeaf(info, request)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("%s at %q must not traverse symlink %q", request.description, request.path, current)
			}
			if !info.IsDir() {
				return nil, fmt.Errorf("%s at %q must traverse directories", request.description, request.path)
			}
		}
	}
	info, err := root.Lstat(clean)
	if err != nil {
		return nil, err
	}
	return validateRootedLeaf(info, request)
}

func validateRootedLeaf(info fs.FileInfo, request rootedPathRequest) (fs.FileInfo, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s at %q must be a %s, not a symlink or special file", request.description, request.path, rootedLeafName(request.directory))
	}
	if request.directory && !info.IsDir() {
		return nil, fmt.Errorf("%s at %q must be a directory, not a regular file or special file", request.description, request.path)
	}
	if !request.directory && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s at %q must be a regular file, not a symlink or special file", request.description, request.path)
	}
	return info, nil
}

func rootedLeafName(directory bool) string {
	if directory {
		return "directory"
	}
	return "regular file"
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
