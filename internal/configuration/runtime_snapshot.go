package configuration

import (
	"errors"
	"fmt"
	"io/fs"
)

// runtimeSnapshot is the private implementation of RuntimeSnapshot. The
// selected Profiles are copied into the snapshot so callers cannot accidentally
// resolve the same definition again after the selection has been planned.
type runtimeSnapshot struct {
	selection ResolvedReviews
	effective Effective
	profiles  map[runtimeDefinitionKey]Profile
}

// Selection returns a copy of the resolved selection facts.
func (snapshot *runtimeSnapshot) Selection() ResolvedReviews {
	if snapshot == nil {
		return ResolvedReviews{}
	}
	return cloneResolvedReviews(snapshot.selection)
}

// Effective returns the effective reviewer policy captured with the selection.
func (snapshot *runtimeSnapshot) Effective() Effective {
	if snapshot == nil {
		return Effective{}
	}
	return snapshot.effective
}

// ProfileFor returns selected executable material without consulting storage.
func (snapshot *runtimeSnapshot) ProfileFor(slot ExpandedProfile) (Profile, bool) {
	if snapshot == nil {
		return Profile{}, false
	}
	profile, found := snapshot.profiles[runtimeDefinitionKey{scope: slot.Scope, name: slot.Profile}]
	return profile, found
}

func cloneResolvedReviews(selection ResolvedReviews) ResolvedReviews {
	selection.Authored = cloneSlice(selection.Authored)
	selection.Expanded = cloneSlice(selection.Expanded)
	selection.Deduplicated = cloneSlice(selection.Deduplicated)
	selection.Warnings = cloneSlice(selection.Warnings)
	return selection
}

func cloneSlice[T any](values []T) []T {
	if values == nil {
		return nil
	}
	clone := make([]T, len(values))
	copy(clone, values)
	return clone
}

type runtimeDefinitionKey struct {
	scope Scope
	name  string
}

type runtimeDefinitions struct {
	profiles     map[runtimeDefinitionKey]Definition[Profile]
	profileNames map[Scope]map[string]struct{}
	parties      map[runtimeDefinitionKey]Definition[Party]
	partyNames   map[Scope]map[string]struct{}
}

// ResolveRuntime captures the effective configuration, resolved selection, and
// executable Profile material for one run. A no-selection result returns the
// effective view alongside ErrNoRepositorySelection so inspection can still
// explain the configured state.
func (manager *Manager) ResolveRuntime(request RunRequest) (RuntimeSnapshot, error) {
	loaded, err := manager.Load(request.Repository)
	if err != nil {
		return nil, err
	}
	if err := validateRuntimeRequest(request); err != nil {
		return nil, err
	}

	effective, err := manager.ResolveLoaded(Request{Repository: request.Repository}, loaded)
	if err != nil {
		return nil, err
	}
	snapshot := &runtimeSnapshot{effective: effective}
	if noSelection, err := manager.noSelectionError(request, loaded); noSelection {
		return snapshot, err
	}

	definitions, err := manager.loadRuntimeDefinitions(request, loaded)
	if err != nil {
		return nil, err
	}
	selectionFacts, selectionValue := manager.effectiveReviewSelection(loaded)
	selection, err := definitions.resolve(request, selectionFacts, selectionValue)
	if err != nil {
		return nil, err
	}
	profiles, err := definitions.selectedProfiles(selection)
	if err != nil {
		return nil, err
	}
	snapshot.selection = selection
	snapshot.profiles = profiles
	return snapshot, nil
}

func validateRuntimeRequest(request RunRequest) error {
	if request.Profile != "" && request.Party != "" {
		return errors.New("an explicit Profile and an explicit Party cannot be selected together")
	}
	return nil
}

func (manager *Manager) noSelectionError(request RunRequest, loaded Loaded) (bool, error) {
	if request.Profile != "" || request.Party != "" {
		return false, nil
	}
	selection, value := manager.effectiveReviewSelection(loaded)
	if !value.Authored {
		return true, ErrNoRepositorySelection
	}
	if len(selection.Global) == 0 && len(selection.Repository) == 0 {
		return true, fmt.Errorf("%w: reviews selects nothing", ErrNoRepositorySelection)
	}
	return false, nil
}

func (manager *Manager) loadRuntimeDefinitions(request RunRequest, loaded Loaded) (runtimeDefinitions, error) {
	definitions := runtimeDefinitions{
		profiles:     make(map[runtimeDefinitionKey]Definition[Profile]),
		profileNames: make(map[Scope]map[string]struct{}),
		parties:      make(map[runtimeDefinitionKey]Definition[Party]),
		partyNames:   make(map[Scope]map[string]struct{}),
	}
	if err := manager.loadRuntimeProfiles(request.Repository, &definitions); err != nil {
		return runtimeDefinitions{}, err
	}
	if runtimeRequestNeedsParties(request, loaded) {
		if err := manager.loadRuntimeParties(request.Repository, &definitions); err != nil {
			return runtimeDefinitions{}, err
		}
	}
	return definitions, nil
}

func runtimeRequestNeedsParties(request RunRequest, loaded Loaded) bool {
	if request.Party != "" {
		return true
	}
	if request.Profile != "" {
		return false
	}
	return savedSelectionIncludesParty(loaded.Repository)
}

func savedSelectionIncludesParty(document LoadedDocument) bool {
	if !document.Present || document.Document.Reviews == nil {
		return false
	}
	selection := *document.Document.Reviews
	return selectionIncludesParty(selection.Global) || selectionIncludesParty(selection.Repository)
}

func selectionIncludesParty(items []SelectionItem) bool {
	for _, item := range items {
		if selectsParty(item) {
			return true
		}
	}
	return false
}

func (manager *Manager) loadRuntimeProfiles(repository Repository, definitions *runtimeDefinitions) error {
	return manager.loadRuntimeLayers(repository, runtimeLayerRequest{
		child: profileDirectoryName, description: "Profile library", definitions: definitions,
		capture: manager.captureRuntimeProfile,
	})
}

type runtimeDefinitionCapture func(Repository, configurationLayer, fs.DirEntry, *runtimeDefinitions)

type runtimeLayerRequest struct {
	child       string
	description string
	definitions *runtimeDefinitions
	capture     runtimeDefinitionCapture
}

func (manager *Manager) loadRuntimeLayers(repository Repository, request runtimeLayerRequest) error {
	entries, err := manager.definitionEntries(repository, request.child, request.description)
	if errors.Is(err, ErrGlobalRootUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, definitionEntry := range entries {
		request.capture(repository, definitionEntry.layer, definitionEntry.entry, request.definitions)
	}
	return nil
}

func (manager *Manager) captureRuntimeProfile(repository Repository, layer configurationLayer, entry fs.DirEntry, definitions *runtimeDefinitions) {
	name := entry.Name()
	authored := profileEntryFor(layer, name)
	key := runtimeDefinitionKey{scope: layer.scope, name: name}
	if entry.IsDir() {
		definitions.profiles[key] = manager.inventoryProfile(repository, authored)
		addRuntimeName(definitions.profileNames, layer.scope, name)
		return
	}
	profile, found, loadErr := manager.LoadProfile(layer.scope, repository, name)
	if loadErr != nil {
		definitions.profiles[key] = Definition[Profile]{
			Scope: layer.scope, Name: name, Path: authored.Path, Source: authored.Source,
			Value: profile, Err: loadErr,
		}
		return
	}
	if found {
		definitions.profiles[key] = Definition[Profile]{
			Scope: layer.scope, Name: name, Path: authored.Path, Source: authored.Source,
			Value: profile,
		}
	}
}

func (manager *Manager) loadRuntimeParties(repository Repository, definitions *runtimeDefinitions) error {
	return manager.loadRuntimeLayers(repository, runtimeLayerRequest{
		child: partyDirectoryName, description: "Party library", definitions: definitions,
		capture: manager.captureRuntimeParty,
	})
}

func (manager *Manager) captureRuntimeParty(repository Repository, layer configurationLayer, entry fs.DirEntry, definitions *runtimeDefinitions) {
	name, selected := selectPartyEntry(entry)
	if !selected {
		return
	}
	authored := partyEntryFor(layer, name)
	party, found, loadErr := manager.LoadParty(layer.scope, repository, name)
	if loadErr == nil && !found {
		loadErr = fmt.Errorf("Party %q is incomplete", name)
	}
	definitions.parties[runtimeDefinitionKey{scope: layer.scope, name: name}] = Definition[Party]{
		Scope: layer.scope, Name: name, Path: authored.Path, Source: authored.Source,
		Value: party, Err: loadErr,
	}
	addRuntimeName(definitions.partyNames, layer.scope, name)
}

func addRuntimeName(names map[Scope]map[string]struct{}, scope Scope, name string) {
	if names[scope] == nil {
		names[scope] = make(map[string]struct{})
	}
	names[scope][name] = struct{}{}
}

func (definitions runtimeDefinitions) resolve(request RunRequest, selection ReviewSelection, value Value[ReviewSelection]) (ResolvedReviews, error) {
	return selectionResolver{lookup: definitions.selectionLookup()}.
		resolve(request.Repository, request, selection, value)
}

func (definitions runtimeDefinitions) selectionLookup() selectionLookup {
	return selectionLookup{
		profileAt: func(scope Scope, name string) (Profile, bool, error) {
			return definitions.profile(scope, name)
		},
		partyAt: func(scope Scope, name string) (Party, bool, error) {
			return definitions.party(scope, name)
		},
		profileNames: func(scope Scope) []string {
			return availableRuntimeNames(definitions.profileNames, scope)
		},
		partyNames: func(scope Scope) []string {
			return availableRuntimeNames(definitions.partyNames, scope)
		},
	}
}

func (definitions runtimeDefinitions) profile(scope Scope, name string) (Profile, bool, error) {
	return findRuntimeDefinition(definitions.profiles, scope, name)
}

func (definitions runtimeDefinitions) party(scope Scope, name string) (Party, bool, error) {
	return findRuntimeDefinition(definitions.parties, scope, name)
}

func findRuntimeDefinition[T any](definitions map[runtimeDefinitionKey]Definition[T], scope Scope, name string) (T, bool, error) {
	var zero T
	definition, found := definitions[runtimeDefinitionKey{scope: scope, name: name}]
	if !found {
		return zero, false, nil
	}
	if definition.Err != nil {
		return zero, true, definition.Err
	}
	return definition.Value, true, nil
}

func availableRuntimeNames(names map[Scope]map[string]struct{}, scope Scope) []string {
	available := make(map[string]struct{})
	for candidateScope, scopedNames := range names {
		if scope != "" && candidateScope != scope {
			continue
		}
		for name := range scopedNames {
			available[name] = struct{}{}
		}
	}
	return sortedKeySet(available)
}

func (definitions runtimeDefinitions) selectedProfiles(selection ResolvedReviews) (map[runtimeDefinitionKey]Profile, error) {
	profiles := make(map[runtimeDefinitionKey]Profile, len(selection.Expanded))
	for _, slot := range selection.Expanded {
		profile, found, err := definitions.profile(slot.Scope, slot.Profile)
		if err != nil {
			return nil, fmt.Errorf("%s: %s Profile %q: %w", slot.Origin, slot.Scope, slot.Profile, err)
		}
		if !found {
			return nil, fmt.Errorf("%s: %s Profile %q was not found", slot.Origin, slot.Scope, slot.Profile)
		}
		profiles[runtimeDefinitionKey{scope: slot.Scope, name: slot.Profile}] = profile
	}
	return profiles, nil
}
