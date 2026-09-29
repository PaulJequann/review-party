package engine

import (
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
	return conductor.compileProfile(profileCompileRequest{profile: resolved.configurationProfile(), effective: resolved.effective, selection: selection, deadline: resolved.deadline})
}

func (conductor *Conductor) compileResolvedProfile(resolved resolvedProfile) (compiledProfile, error) {
	selection := model.ProfileSelection{Profile: resolved.name, Reviewer: resolved.reviewer, Model: resolved.model, Effort: resolved.effort}
	return conductor.compileProfile(profileCompileRequest{profile: resolved.configurationProfile(), effective: resolved.effective, selection: selection, deadline: resolved.deadline})
}

type profileRequest struct {
	repository string
	name       string
}

func (conductor *Conductor) resolveProfile(request profileRequest) (resolvedProfile, error) {
	if conductor.configuration == nil {
		return resolvedProfile{}, errConfigurationNotConfigured
	}
	scope, name, err := conductor.configuration.ParseProfileReference(request.name)
	if err != nil {
		return resolvedProfile{}, err
	}
	effective, err := conductor.configuration.Resolve(configuration.Request{Repository: configuration.Repository(request.repository)})
	if err != nil {
		return resolvedProfile{}, err
	}
	profile, found, err := conductor.loadProfile(request.repository, scope, name)
	if err != nil {
		return resolvedProfile{}, err
	}
	if !found {
		return resolvedProfile{}, UnknownProfileError{Name: name, Scope: scope, Available: conductor.profileNamesInScope(request.repository, scope)}
	}
	return resolvedFromProfile(profile, effective)
}

func (conductor *Conductor) loadProfile(repository string, scope configuration.Scope, name string) (configuration.Profile, bool, error) {
	if scope == "" {
		return conductor.configuration.ResolveProfile(configuration.Repository(repository), name)
	}
	return conductor.configuration.ResolveProfileReference(configuration.Repository(repository), configuration.ProfileReference{Scope: scope, Profile: name})
}

func resolvedFromProfile(profile configuration.Profile, effective configuration.Effective) (resolvedProfile, error) {
	deadline, err := time.ParseDuration(profile.AttemptDeadline)
	if err != nil {
		return resolvedProfile{}, err
	}
	return resolvedProfile{name: profile.Name, instructions: profile.Instructions, source: profile.Source, digest: profile.SourceDigest, reviewer: profile.Reviewer, model: profile.Model, effort: profile.ReasoningEffort, deadline: deadline, effective: effective}, nil
}

func (profile resolvedProfile) configurationProfile() configuration.Profile {
	return configuration.Profile{Name: profile.name, Reviewer: profile.reviewer, Model: profile.model, ReasoningEffort: profile.effort, Instructions: profile.instructions, Source: profile.source, SourceDigest: profile.digest}
}

type profileInventoryFilter func(configuration.Definition[configuration.Profile]) bool

func (conductor *Conductor) profileNamesInScope(repository string, scope configuration.Scope) []string {
	return conductor.profileNames(repository, func(definition configuration.Definition[configuration.Profile]) bool {
		return scope == "" || definition.Scope == scope
	})
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
