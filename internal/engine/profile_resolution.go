package engine

import (
	"fmt"
	"sort"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func profileSelectionHasExecutionOverrides(selection model.ProfileSelection) bool {
	return selection.Reviewer != "" || selection.Model != "" || selection.Effort != ""
}

func (conductor *Conductor) compileExperimentProfile(selection model.ProfileSelection, repository string) (compiledProfile, error) {
	resolved, err := conductor.profiles.resolve(profileRequest{repository: repository, name: selection.Profile})
	if err != nil {
		return compiledProfile{}, err
	}
	return conductor.compileResolvedExperimentProfile(selection, resolved)
}

func (conductor *Conductor) compileResolvedExperimentProfile(selection model.ProfileSelection, resolved resolvedProfile) (compiledProfile, error) {
	resolved.deadline = conductor.evalDefaultDeadline
	if selection.Reviewer == "" {
		selection.Reviewer = resolved.reviewer
	}
	if selection.Model == "" {
		selection.Model = resolved.model
	}
	if selection.Effort == "" {
		selection.Effort = resolved.effort
	}
	return conductor.compileProfile(selection, resolved)
}

func (conductor *Conductor) compileResolvedProfile(resolved resolvedProfile) (compiledProfile, error) {
	selection := model.ProfileSelection{Profile: resolved.name, Reviewer: resolved.reviewer, Model: resolved.model, Effort: resolved.effort}
	return conductor.compileProfile(selection, resolved)
}

func (library profileLibrary) resolveConfiguration(request profileRequest) (configuration.Effective, string, error) {
	if library.manager() == nil {
		return configuration.Effective{}, "", errProfileLibraryNotConfigured
	}
	if err := validateAuthoredName(request.name); err != nil {
		return configuration.Effective{}, "", fmt.Errorf("profile name %q: %w", request.name, err)
	}
	effective, err := library.manager().Resolve(configuration.Request{Repository: configuration.Repository(request.repository)})
	return effective, request.name, err
}

func (library profileLibrary) resolve(request profileRequest) (resolvedProfile, error) {
	effective, name, err := library.resolveConfiguration(request)
	if err != nil {
		return resolvedProfile{}, err
	}
	profile, found, err := library.manager().ResolveProfile(configuration.Repository(request.repository), name)
	if err != nil {
		return resolvedProfile{}, err
	}
	if !found {
		return resolvedProfile{}, UnknownProfileError{Name: name, Available: library.authoredProfileNames(request.repository)}
	}
	return resolvedFromProfile(profile, effective)
}

func resolvedFromProfile(profile configuration.Profile, effective configuration.Effective) (resolvedProfile, error) {
	deadline, err := time.ParseDuration(profile.AttemptDeadline)
	if err != nil {
		return resolvedProfile{}, err
	}
	return resolvedProfile{name: profile.Name, instructions: profile.Instructions, source: profile.Source, digest: profile.SourceDigest, reviewer: profile.Reviewer, model: profile.Model, effort: profile.ReasoningEffort, deadline: deadline, effective: effective}, nil
}

type profileInventoryFilter func(configuration.Definition[configuration.Profile]) bool

func (library profileLibrary) authoredProfileNames(repository string) []string {
	return library.profileNames(repository, func(configuration.Definition[configuration.Profile]) bool { return true })
}

func (library profileLibrary) executableProfileNames(repository string) []string {
	return library.profileNames(repository, func(definition configuration.Definition[configuration.Profile]) bool {
		return definition.Err == nil
	})
}

func (library profileLibrary) profileNames(repository string, include profileInventoryFilter) []string {
	inventory, err := library.inventory(repository)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(inventory))
	seen := make(map[string]struct{}, len(inventory))
	for _, definition := range inventory {
		if !include(definition) {
			continue
		}
		if _, exists := seen[definition.Name]; exists {
			continue
		}
		seen[definition.Name] = struct{}{}
		names = append(names, definition.Name)
	}
	sort.Strings(names)
	return names
}
