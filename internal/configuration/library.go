package configuration

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// AuthoredEntry identifies one exact scoped configuration definition.
type AuthoredEntry struct {
	Scope  Scope
	Name   string
	Path   string
	Source string
}

// Definition is one inventoried scoped definition. Invalid or incomplete
// authored material remains visible through Err instead of disappearing from
// inventory; Value is populated only for executable definitions.
type Definition[T any] struct {
	Scope  Scope
	Name   string
	Path   string
	Source string
	Value  T
	Err    error
}

type configurationLayer struct {
	scope     Scope
	anchor    string
	directory string
}

func (manager *Manager) configurationLayers(repository Repository, child string) ([]configurationLayer, error) {
	layers := make([]configurationLayer, 0, 2)
	if repository != "" {
		layers = append(layers, configurationLayer{scope: ScopeRepository, anchor: string(repository), directory: filepath.Join(string(repository), ".reviewparty", child)})
	}
	root, err := manager.GlobalRoot()
	if err == nil {
		layers = append(layers, configurationLayer{scope: ScopeGlobal, anchor: filepath.Dir(root), directory: filepath.Join(root, child)})
	} else if repository == "" {
		return nil, err
	}
	return layers, nil
}

func readDefinitionLayer(layer configurationLayer, description string) ([]fs.DirEntry, error) {
	relative, err := filepath.Rel(layer.anchor, layer.directory)
	if err != nil {
		return nil, err
	}
	root, found, err := openConfigurationRoot(layer.anchor, description)
	if err != nil || !found {
		return nil, err
	}
	defer root.Close()
	entries, found, err := readRootedDirectory(root, directoryReadRequest{relative: relative, path: layer.directory, description: description})
	if err != nil || !found {
		return nil, err
	}
	return entries, nil
}

func validateDefinitionName(description, name string) error {
	switch name {
	case "", ".", "..":
		return invalidDefinitionName(description, name)
	}
	if strings.ContainsAny(name, `/\\`) {
		return invalidDefinitionName(description, name)
	}
	return nil
}

func invalidDefinitionName(description, name string) error {
	return fmt.Errorf("%s name %q must be a single file name", description, name)
}
