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
	selection, err := definitions.resolve(manager, request, loaded)
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
	layers, err := manager.configurationLayers(repository, request.child)
	if errors.Is(err, ErrGlobalRootUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, layer := range layers {
		entries, err := readDefinitionLayer(layer, request.description)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			request.capture(repository, layer, entry, request.definitions)
		}
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

func (definitions runtimeDefinitions) resolve(manager *Manager, request RunRequest, loaded Loaded) (ResolvedReviews, error) {
	switch {
	case request.Profile != "":
		return definitions.resolveExplicitProfile(request.Repository, request.Profile)
	case request.Party != "":
		return definitions.resolveExplicitParty(request.Repository, request.Party)
	default:
		return definitions.resolveDefaultSelection(manager, request.Repository, loaded)
	}
}

func (definitions runtimeDefinitions) resolveExplicitProfile(repository Repository, value string) (ResolvedReviews, error) {
	qualified, name := parseScopedReference(value)
	scope, err := definitions.explicitProfileScope(repository, qualified, name)
	if err != nil {
		return ResolvedReviews{}, err
	}
	return ResolvedReviews{
		Kind: SelectionExplicitProfile, Source: sourceExplicit,
		Authored:         []SelectedDefinition{{Kind: ItemProfile, Name: name, Scope: scope}},
		Expanded:         []ExpandedProfile{{Scope: scope, Profile: name, Origin: originExplicit}},
		ConcurrencyLimit: 1, LimitSource: LimitExplicitProfile,
	}, nil
}

func (definitions runtimeDefinitions) explicitProfileScope(repository Repository, qualified Scope, name string) (Scope, error) {
	if qualified != "" {
		return definitions.qualifiedProfileScope(qualified, name)
	}
	return definitions.unqualifiedProfileScope(repository, name)
}

func (definitions runtimeDefinitions) qualifiedProfileScope(scope Scope, name string) (Scope, error) {
	_, found, err := definitions.profile(scope, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", definitions.unresolvedProfile(name, scope, "")
	}
	return scope, nil
}

func (definitions runtimeDefinitions) unqualifiedProfileScope(repository Repository, name string) (Scope, error) {
	if repository != "" {
		_, found, err := definitions.profile(ScopeRepository, name)
		if err != nil {
			return "", err
		}
		if found {
			return ScopeRepository, nil
		}
	}
	_, found, err := definitions.profile(ScopeGlobal, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", definitions.unresolvedProfile(name, "", "")
	}
	return ScopeGlobal, nil
}

func (definitions runtimeDefinitions) resolveExplicitParty(repository Repository, value string) (ResolvedReviews, error) {
	party, scope, err := definitions.selectionParty(repository, value, "")
	if err != nil {
		return ResolvedReviews{}, err
	}
	resolved := ResolvedReviews{
		Kind: SelectionExplicitParty, Source: sourceExplicit,
		ConcurrencyLimit: party.ConcurrencyLimit, LimitSource: LimitParty,
	}
	expander := selectionExpander{}
	expander.expand(&resolved, repository, []authoredEntry{{
		definition: SelectedDefinition{Kind: ItemParty, Name: party.Name, Scope: scope},
		members:    party.Profiles, hasMembers: true, origin: fmt.Sprintf("%s %s", ItemParty, party.Name),
	}})
	expander.finish(&resolved)
	return resolved, nil
}

func (definitions runtimeDefinitions) resolveDefaultSelection(manager *Manager, repository Repository, loaded Loaded) (ResolvedReviews, error) {
	selection, value := manager.effectiveReviewSelection(loaded)
	if !value.Authored {
		return ResolvedReviews{}, ErrNoRepositorySelection
	}
	entries, err := definitions.defaultEntries(repository, selection)
	if err != nil {
		return ResolvedReviews{}, err
	}
	resolved := ResolvedReviews{Kind: SelectionRepositoryDefault, Source: value.Path}
	expander := selectionExpander{}
	expander.expand(&resolved, repository, entries)
	expander.finish(&resolved)
	if len(resolved.Authored) == 0 && len(resolved.Expanded) == 0 {
		return ResolvedReviews{}, fmt.Errorf("%w: reviews selects nothing", ErrNoRepositorySelection)
	}
	resolved.ConcurrencyLimit = selection.ConcurrencyLimit
	resolved.LimitSource = LimitRepositorySelection
	return resolved, nil
}

func (definitions runtimeDefinitions) defaultEntries(repository Repository, selection ReviewSelection) ([]authoredEntry, error) {
	entries := make([]authoredEntry, 0, len(selection.Global)+len(selection.Repository))
	groups := []struct {
		name  string
		scope Scope
		items []SelectionItem
	}{
		{name: "reviews.global", scope: ScopeGlobal, items: selection.Global},
		{name: "reviews.repository", scope: ScopeRepository, items: selection.Repository},
	}
	for _, group := range groups {
		for index, item := range group.items {
			origin := fmt.Sprintf("%s[%d]", group.name, index)
			entry, err := definitions.selectedEntry(repository, group.scope, origin, item)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (definitions runtimeDefinitions) selectedEntry(repository Repository, scope Scope, origin string, item SelectionItem) (authoredEntry, error) {
	name, err := item.Name()
	if err != nil {
		return authoredEntry{}, fmt.Errorf("%s: %w", origin, err)
	}
	switch {
	case selectsProfile(item):
		_, found, err := definitions.profile(scope, name)
		if err != nil {
			return authoredEntry{}, fmt.Errorf("%s: %w", origin, err)
		}
		if !found {
			return authoredEntry{}, definitions.unresolvedProfile(name, scope, origin)
		}
		return authoredEntry{definition: SelectedDefinition{Kind: ItemProfile, Name: name, Scope: scope}, origin: origin}, nil
	case selectsParty(item):
		party, _, err := definitions.selectionPartyAt(repository, scope, name, origin)
		if err != nil {
			return authoredEntry{}, err
		}
		return authoredEntry{
			definition: SelectedDefinition{Kind: ItemParty, Name: name, Scope: scope},
			members:    party.Profiles, hasMembers: true, origin: origin,
		}, nil
	default:
		return authoredEntry{}, fmt.Errorf("%s: must select exactly one profile or party", origin)
	}
}

func (definitions runtimeDefinitions) selectionParty(repository Repository, value, origin string) (Party, Scope, error) {
	qualified, name := parseScopedReference(value)
	if qualified != "" {
		return definitions.selectionPartyAt(repository, qualified, name, origin)
	}
	if repository != "" {
		party, found, err := definitions.party(ScopeRepository, name)
		if err != nil {
			return Party{}, "", err
		}
		if found {
			return party, ScopeRepository, nil
		}
	}
	party, found, err := definitions.party(ScopeGlobal, name)
	if err != nil {
		return Party{}, "", err
	}
	if !found {
		return Party{}, "", definitions.unresolvedParty(name, "", origin)
	}
	return party, ScopeGlobal, nil
}

func (definitions runtimeDefinitions) selectionPartyAt(_ Repository, scope Scope, name, origin string) (Party, Scope, error) {
	party, found, err := definitions.party(scope, name)
	if err != nil {
		return Party{}, "", err
	}
	if !found {
		return Party{}, "", definitions.unresolvedParty(name, scope, origin)
	}
	return party, scope, nil
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

func (definitions runtimeDefinitions) unresolvedProfile(name string, scope Scope, selectedBy string) error {
	return UnresolvedReferenceError{
		Kind: ItemProfile, Name: name, Scope: scope, SelectedBy: selectedBy,
		Available: definitions.availableNames(definitions.profileNames, scope),
	}
}

func (definitions runtimeDefinitions) unresolvedParty(name string, scope Scope, selectedBy string) error {
	return UnresolvedReferenceError{
		Kind: ItemParty, Name: name, Scope: scope, SelectedBy: selectedBy,
		Available: definitions.availableNames(definitions.partyNames, scope),
	}
}

func (definitions runtimeDefinitions) availableNames(names map[Scope]map[string]struct{}, scope Scope) []string {
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
