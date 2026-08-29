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
	resolved, err := conductor.resolveProfile(profileRequest{repository: repository, name: selection.Profile})
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

type profileRequest struct {
	repository string
	name       string
}

func (conductor *Conductor) resolveProfile(request profileRequest) (resolvedProfile, error) {
	if conductor.configuration == nil {
		return resolvedProfile{}, errConfigurationNotConfigured
	}
	if err := validateAuthoredName(request.name); err != nil {
		return resolvedProfile{}, fmt.Errorf("profile name %q: %w", request.name, err)
	}
	effective, err := conductor.configuration.Resolve(configuration.Request{Repository: configuration.Repository(request.repository)})
	if err != nil {
		return resolvedProfile{}, err
	}
	profile, found, err := conductor.configuration.ResolveProfile(configuration.Repository(request.repository), request.name)
	if err != nil {
		return resolvedProfile{}, err
	}
	if !found {
		return resolvedProfile{}, UnknownProfileError{Name: request.name, Available: conductor.authoredProfileNames(request.repository)}
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

func (conductor *Conductor) authoredProfileNames(repository string) []string {
	return conductor.profileNames(repository, func(configuration.Definition[configuration.Profile]) bool { return true })
}

func (conductor *Conductor) profileNames(repository string, include profileInventoryFilter) []string {
	if conductor.configuration == nil {
		return nil
	}
	inventory, err := conductor.configuration.ProfileInventory(configuration.Repository(repository))
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
