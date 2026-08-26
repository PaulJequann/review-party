package engine

import (
	"sort"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

type profileLookup struct {
	repository string
	name       string
}

func (library profileLibrary) findProfile(lookup profileLookup) (resolvedProfile, error) {
	return library.resolve(profileRequest{repository: lookup.repository, name: lookup.name})
}

func (library profileLibrary) inventory(repository string) ([]configuration.Definition[configuration.Profile], error) {
	if library.manager() == nil {
		return nil, errProfileLibraryNotConfigured
	}
	return library.manager().ProfileInventory(configuration.Repository(repository))
}

func (library profileLibrary) list(repository string) ([]model.ProfileSummary, error) {
	inventory, err := library.inventory(repository)
	if err != nil {
		return nil, err
	}
	profiles := make([]model.ProfileSummary, 0, len(inventory))
	for _, definition := range inventory {
		profiles = append(profiles, profileInventorySummary(definition))
	}
	sort.SliceStable(profiles, func(left, right int) bool { return profiles[left].Name < profiles[right].Name })
	return profiles, nil
}

func profileInventorySummary(definition configuration.Definition[configuration.Profile]) model.ProfileSummary {
	summary := model.ProfileSummary{Name: definition.Name, Source: definition.Source, Path: definition.Path}
	if definition.Err != nil {
		summary.Error = definition.Err.Error()
		return summary
	}
	summary.Source = definition.Value.Source
	return summary
}
