package configuration

import (
	"fmt"
	"path/filepath"
)

const profileDirectoryName = "profiles"

// ResolveProfile returns the first complete Profile in Repository-then-Global order.
func (manager *Manager) ResolveProfile(repository Repository, name string) (Profile, bool, error) {
	if repository != "" {
		profile, found, err := manager.LoadProfile(ScopeRepository, repository, name)
		if err != nil || found {
			return profile, found, err
		}
	}
	return manager.LoadProfile(ScopeGlobal, repository, name)
}

// ResolveProfileReference returns one exact scoped Profile.
func (manager *Manager) ResolveProfileReference(repository Repository, reference ProfileReference) (Profile, bool, error) {
	return manager.LoadProfile(reference.Scope, repository, reference.Profile)
}

// ProfileInventory returns complete and invalid authored Profiles in
// Repository-then-Global order. Each definition is read exactly once.
func (manager *Manager) ProfileInventory(repository Repository) ([]Definition[Profile], error) {
	entries, err := manager.definitionEntries(repository, profileDirectoryName, "Profile library")
	if err != nil {
		return nil, err
	}
	inventory := make([]Definition[Profile], 0)
	for _, definitionEntry := range entries {
		if definitionEntry.entry.IsDir() {
			inventory = append(inventory, manager.inventoryProfile(repository, profileEntryFor(definitionEntry.layer, definitionEntry.entry.Name())))
		}
	}
	return inventory, nil
}

func (manager *Manager) inventoryProfile(repository Repository, entry AuthoredEntry) Definition[Profile] {
	profile, found, err := manager.LoadProfile(entry.Scope, repository, entry.Name)
	if err == nil && !found {
		err = fmt.Errorf("Profile %q is incomplete", entry.Name)
	}
	return Definition[Profile]{Scope: entry.Scope, Name: entry.Name, Path: entry.Path, Source: entry.Source, Value: profile, Err: err}
}

func (manager *Manager) profileEntry(scope Scope, repository Repository, name string) (AuthoredEntry, string, error) {
	if err := validateDefinitionName("profile", name); err != nil {
		return AuthoredEntry{}, "", err
	}
	layers, err := manager.configurationLayers(repository, profileDirectoryName)
	if err != nil {
		return AuthoredEntry{}, "", err
	}
	for _, layer := range layers {
		if layer.scope == scope {
			return profileEntryFor(layer, name), layer.anchor, nil
		}
	}
	return AuthoredEntry{}, "", fmt.Errorf("%s Configuration is unavailable", scope)
}

func profileEntryFor(layer configurationLayer, name string) AuthoredEntry {
	path := filepath.Join(layer.directory, name, "instructions.md")
	source := "global:profiles/" + name
	if layer.scope == ScopeRepository {
		source = "repository:.reviewparty/profiles/" + name
	}
	return AuthoredEntry{Scope: layer.scope, Name: name, Path: path, Source: source}
}
