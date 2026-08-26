package configuration

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

const partyDirectoryName = "parties"

// ResolveParty returns the first complete Party in Repository-then-Global order.
func (manager *Manager) ResolveParty(repository Repository, name string) (Party, bool, error) {
	if repository != "" {
		party, found, err := manager.LoadParty(ScopeRepository, repository, name)
		if err != nil || found {
			return party, found, err
		}
	}
	return manager.LoadParty(ScopeGlobal, repository, name)
}

// PartyInventory returns complete and invalid authored Parties in
// Repository-then-Global order. Each definition is read exactly once.
func (manager *Manager) PartyInventory(repository Repository) ([]Definition[Party], error) {
	layers, err := manager.configurationLayers(repository, partyDirectoryName)
	if err != nil {
		return nil, err
	}
	inventory := make([]Definition[Party], 0)
	for _, layer := range layers {
		definitions, readErr := manager.partyInventoryLayer(repository, layer)
		if readErr != nil {
			return nil, readErr
		}
		inventory = append(inventory, definitions...)
	}
	return inventory, nil
}

func (manager *Manager) partyInventoryLayer(repository Repository, layer configurationLayer) ([]Definition[Party], error) {
	entries, err := readDefinitionLayer(layer, "Party library")
	if err != nil {
		return nil, err
	}
	inventory := make([]Definition[Party], 0, len(entries))
	for _, entry := range entries {
		if name, selected := selectPartyEntry(entry); selected {
			inventory = append(inventory, manager.inventoryParty(repository, partyEntryFor(layer, name)))
		}
	}
	return inventory, nil
}

func (manager *Manager) inventoryParty(repository Repository, entry AuthoredEntry) Definition[Party] {
	party, found, err := manager.LoadParty(entry.Scope, repository, entry.Name)
	if err == nil && !found {
		err = fmt.Errorf("Party %q is incomplete", entry.Name)
	}
	return Definition[Party]{Scope: entry.Scope, Name: entry.Name, Path: entry.Path, Source: entry.Source, Value: party, Err: err}
}

func selectPartyEntry(entry fs.DirEntry) (string, bool) {
	if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
		return "", false
	}
	return entry.Name()[:len(entry.Name())-len(".json")], true
}

func (manager *Manager) partyEntry(scope Scope, repository Repository, name string) (AuthoredEntry, string, error) {
	if err := validateDefinitionName("party", name); err != nil {
		return AuthoredEntry{}, "", err
	}
	layers, err := manager.configurationLayers(repository, partyDirectoryName)
	if err != nil {
		return AuthoredEntry{}, "", err
	}
	for _, layer := range layers {
		if layer.scope == scope {
			return partyEntryFor(layer, name), layer.anchor, nil
		}
	}
	return AuthoredEntry{}, "", fmt.Errorf("%s Configuration is unavailable", scope)
}

func partyEntryFor(layer configurationLayer, name string) AuthoredEntry {
	path := filepath.Join(layer.directory, name+".json")
	source := "global:parties/" + name + ".json"
	if layer.scope == ScopeRepository {
		source = "repository:.reviewparty/parties/" + name + ".json"
	}
	return AuthoredEntry{Scope: layer.scope, Name: name, Path: path, Source: source}
}
