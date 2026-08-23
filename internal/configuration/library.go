package configuration

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// LibraryKind identifies a kind of authored definition stored below a
// Personal or Repository Configuration root.
type LibraryKind string

const (
	LibraryProfiles LibraryKind = "profiles"
	LibraryParties  LibraryKind = "parties"
)

type authoredName string

type authoredLibrarySpec struct {
	directory    string
	extension    string
	maximumBytes int64
	description  string
}

// AuthoredEntry identifies one regular-file candidate in an authored library.
// Entries are ordered by configuration precedence.
type AuthoredEntry struct {
	Scope  Scope
	Name   string
	Path   string
	Source string
}

// AuthoredLibrary owns the local filesystem mechanics for one authored
// definition kind across Personal and Repository Configuration.
type AuthoredLibrary struct {
	spec   authoredLibrarySpec
	layers []authoredLibraryLayer
}

type authoredLibraryLayer struct {
	scope     Scope
	anchor    string
	directory string
}

func authoredLibrarySpecFor(kind LibraryKind) (authoredLibrarySpec, bool) {
	switch kind {
	case LibraryProfiles:
		return authoredLibrarySpec{
			directory:    "profiles",
			extension:    ".md",
			maximumBytes: MaximumDocumentBytes,
			description:  "profile",
		}, true
	case LibraryParties:
		return authoredLibrarySpec{
			directory:    "parties",
			extension:    ".json",
			maximumBytes: maximumPartyBytes,
			description:  "party",
		}, true
	default:
		return authoredLibrarySpec{}, false
	}
}

// AuthoredLibrary constructs the Repository-then-Personal storage view for a
// definition kind. Repository entries take precedence over Personal entries.
func (manager *Manager) AuthoredLibrary(kind LibraryKind, repository Repository) (AuthoredLibrary, error) {
	spec, ok := authoredLibrarySpecFor(kind)
	if !ok {
		return AuthoredLibrary{}, fmt.Errorf("unknown authored library %q", kind)
	}

	library := AuthoredLibrary{spec: spec}
	if repository != "" {
		library.layers = append(library.layers, authoredLibraryLayer{
			scope:     ScopeRepository,
			anchor:    string(repository),
			directory: filepath.Join(string(repository), ".reviewparty", spec.directory),
		})
	}
	if root, err := manager.PersonalRoot(); err == nil {
		library.layers = append(library.layers, authoredLibraryLayer{
			scope:     ScopePersonal,
			anchor:    filepath.Dir(root),
			directory: filepath.Join(root, spec.directory),
		})
	}
	return library, nil
}

// Candidates returns the ordered authored locations for one logical name.
// It does not read the locations, allowing callers to preserve an invalid
// higher-precedence definition instead of silently falling back.
func (library AuthoredLibrary) Candidates(name string) ([]AuthoredEntry, error) {
	validated, err := parseAuthoredName(library.spec, name)
	if err != nil {
		return nil, err
	}
	entries := make([]AuthoredEntry, 0, len(library.layers))
	for _, layer := range library.layers {
		entries = append(entries, layer.entry(library.spec, validated))
	}
	return entries, nil
}

// Read returns the first present authored file in configuration precedence
// order, together with its provenance and bounded payload.
func (library AuthoredLibrary) Read(name string) (AuthoredEntry, []byte, bool, error) {
	entries, err := library.Candidates(name)
	if err != nil {
		return AuthoredEntry{}, nil, false, err
	}
	for _, entry := range entries {
		payload, found, err := library.ReadEntry(entry)
		if err != nil {
			return entry, nil, false, err
		}
		if found {
			return entry, payload, true, nil
		}
	}
	return AuthoredEntry{}, nil, false, nil
}

// Entries lists authored regular files in precedence order.
func (library AuthoredLibrary) Entries() ([]AuthoredEntry, error) {
	entries := make([]AuthoredEntry, 0)
	for _, layer := range library.layers {
		layerEntries, found, err := library.readLayerEntries(layer)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		entries = append(entries, layerEntries...)
	}
	return entries, nil
}

// ReadEntry reads exactly the supplied authored scope. It does not perform
// precedence lookup, which lets a listing handle a directory in a higher
// scope without accidentally hiding a lower-scope file.
func (library AuthoredLibrary) ReadEntry(entry AuthoredEntry) ([]byte, bool, error) {
	validated, err := parseAuthoredName(library.spec, entry.Name)
	if err != nil {
		return nil, false, err
	}
	var layer authoredLibraryLayer
	foundLayer := false
	for _, candidate := range library.layers {
		if candidate.scope == entry.Scope {
			layer = candidate
			foundLayer = true
			break
		}
	}
	if !foundLayer {
		return nil, false, fmt.Errorf("%s entry %q has unknown scope %q", library.spec.description, entry.Name, entry.Scope)
	}
	expected := layer.entry(library.spec, validated)
	return readRegularFile(
		layer.anchor,
		expected.Path,
		fmt.Sprintf("%s %s", library.spec.description, expected.Source),
		library.spec.maximumBytes,
	)
}

func (library AuthoredLibrary) readLayerEntries(layer authoredLibraryLayer) ([]AuthoredEntry, bool, error) {
	relative, err := filepath.Rel(layer.anchor, layer.directory)
	if err != nil {
		return nil, false, fmt.Errorf("resolve %s directory %q: %w", library.spec.description, layer.directory, err)
	}
	root, found, err := openConfigurationRoot(layer.anchor, fmt.Sprintf("%s library", library.spec.description))
	if err != nil || !found {
		return nil, found, err
	}
	defer root.Close()
	directoryEntries, found, err := readRootedDirectory(root, directoryReadRequest{
		relative:    relative,
		path:        layer.directory,
		description: fmt.Sprintf("%s library", library.spec.description),
	})
	if err != nil || !found {
		return nil, found, err
	}
	entries := make([]AuthoredEntry, 0, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		if entry, found := library.entryFromDirectory(layer, directoryEntry); found {
			entries = append(entries, entry)
		}
	}
	return entries, true, nil
}

func (library AuthoredLibrary) entryFromDirectory(layer authoredLibraryLayer, directoryEntry fs.DirEntry) (AuthoredEntry, bool) {
	if directoryEntry.IsDir() || filepath.Ext(directoryEntry.Name()) != library.spec.extension {
		return AuthoredEntry{}, false
	}
	name := authoredName(strings.TrimSuffix(directoryEntry.Name(), library.spec.extension))
	return layer.entry(library.spec, name), true
}

func (layer authoredLibraryLayer) entry(spec authoredLibrarySpec, name authoredName) AuthoredEntry {
	filename := string(name) + spec.extension
	return AuthoredEntry{
		Scope:  layer.scope,
		Name:   string(name),
		Path:   filepath.Join(layer.directory, filename),
		Source: authoredSource(layer.scope, spec, name),
	}
}

func authoredSource(scope Scope, spec authoredLibrarySpec, name authoredName) string {
	filename := string(name) + spec.extension
	switch scope {
	case ScopeRepository:
		return "repository:.reviewparty/" + spec.directory + "/" + filename
	case ScopePersonal:
		return "personal:" + spec.directory + "/" + filename
	default:
		return string(scope) + ":" + spec.directory + "/" + filename
	}
}

func parseAuthoredName(spec authoredLibrarySpec, name string) (authoredName, error) {
	switch name {
	case "", ".", "..":
		return "", invalidAuthoredName(spec, name)
	}
	if strings.ContainsAny(name, `/\\`) {
		return "", invalidAuthoredName(spec, name)
	}
	return authoredName(name), nil
}

func invalidAuthoredName(spec authoredLibrarySpec, name string) error {
	return fmt.Errorf("%s name %q must be a single file name", spec.description, name)
}
