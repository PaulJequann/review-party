package configuration

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SelectionKind records how one run chose its Profiles: the repository's
// saved roll-up, one explicit Profile, or one explicit Party.
type SelectionKind string

const (
	// SelectionRepositoryDefault is the repository-authored default roll-up.
	SelectionRepositoryDefault SelectionKind = "repository_default"
	// SelectionExplicitProfile is a single explicitly named Profile.
	SelectionExplicitProfile SelectionKind = "explicit_profile"
	// SelectionExplicitParty is a single explicitly named Party.
	SelectionExplicitParty SelectionKind = "explicit_party"
)

// LimitSource identifies which authoring supplied the Concurrency Limit of a
// resolved selection.
type LimitSource string

const (
	// LimitRepositorySelection is the repository selection's own limit.
	LimitRepositorySelection LimitSource = "repository_selection"
	// LimitParty is a named Party's Concurrency Limit.
	LimitParty LimitSource = "party"
	// LimitExplicitProfile is the sequential default of a one-Profile selection.
	LimitExplicitProfile LimitSource = "explicit_profile"
)

// AuthoredItemKind distinguishes the two selectable definition kinds.
type AuthoredItemKind string

const (
	ItemProfile AuthoredItemKind = "profile"
	ItemParty   AuthoredItemKind = "party"
)

// ErrNoRepositorySelection reports that a repository has no executable saved
// Review selection. Callers choose the follow-up guidance.
var ErrNoRepositorySelection = errors.New("repository has no executable review selection")

const (
	sourceExplicit = "explicit"
	originExplicit = "explicit"
)

// RunRequest describes one selection request: a repository plus at most one
// explicit definition. An empty choice resolves the repository's saved roll-up.
type RunRequest struct {
	Repository Repository
	Profile    string
	Party      string
}

// SelectedDefinition records one authored, ordered entry of the effective
// selection after scope resolution.
type SelectedDefinition struct {
	Kind  AuthoredItemKind `json:"kind"`
	Name  string           `json:"name"`
	Scope Scope            `json:"scope"`
}

// ExpandedProfile is one planned execution slot: an exact scoped identity plus
// the authored origin that selected it.
type ExpandedProfile struct {
	Scope   Scope  `json:"scope"`
	Profile string `json:"profile"`
	Origin  string `json:"origin"`
}

// SkippedProfile records one occurrence removed by exact-identity
// deduplication and where its first occurrence remains.
type SkippedProfile struct {
	Scope      Scope  `json:"scope"`
	Profile    string `json:"profile"`
	Origin     string `json:"origin"`
	KeptOrigin string `json:"kept_origin"`
}

// WarningCategory distinguishes resolver warning kinds.
type WarningCategory string

// WarningSameNameCrossScope fires when same-named Profiles from Global and
// Repository Configuration will both execute.
const WarningSameNameCrossScope WarningCategory = "same_name_cross_scope"

// ResolverWarning is one typed resolution warning with caller-ready text.
type ResolverWarning struct {
	Category WarningCategory `json:"category"`
	Name     string          `json:"name"`
	Message  string          `json:"message"`
}

// ResolvedReviews is the inspectable effective selection for one run request:
// authored entries, the expanded ordered execution list, deduplication facts,
// warnings, and the provenance of the Concurrency Limit.
type ResolvedReviews struct {
	Kind             SelectionKind        `json:"kind"`
	Source           string               `json:"source"`
	Authored         []SelectedDefinition `json:"authored"`
	Expanded         []ExpandedProfile    `json:"expanded"`
	Deduplicated     []SkippedProfile     `json:"deduplicated,omitempty"`
	Warnings         []ResolverWarning    `json:"warnings,omitempty"`
	ConcurrencyLimit int                  `json:"concurrency_limit"`
	LimitSource      LimitSource          `json:"limit_source"`
}

// ResolveRun expands one run request into the Effective Review Selection.
// Resolution never writes files and never launches work. Global selections
// expand before Repository selections in authored order, exact scoped
// identities deduplicate at their first occurrence, and missing required
// material fails closed before any caller plans execution.
func (manager *Manager) ResolveRun(request RunRequest) (ResolvedReviews, error) {
	loaded, err := manager.Load(request.Repository)
	if err != nil {
		return ResolvedReviews{}, err
	}
	return manager.ResolveRunLoaded(request, loaded)
}

// ResolveRunLoaded resolves one selection from an already loaded configuration
// snapshot. Definition files remain resolved through the manager so missing or
// malformed executable material still fails closed.
func (manager *Manager) ResolveRunLoaded(request RunRequest, loaded Loaded) (ResolvedReviews, error) {
	switch {
	case request.Profile != "" && request.Party != "":
		return ResolvedReviews{}, errors.New("an explicit Profile and an explicit Party cannot be selected together")
	case request.Profile != "":
		return manager.resolveExplicitProfile(request.Repository, request.Profile)
	case request.Party != "":
		return manager.resolveExplicitParty(request.Repository, request.Party)
	default:
		return manager.resolveDefaultSelection(request.Repository, loaded)
	}
}

// UnresolvedReferenceError reports a selection whose referenced definition is
// absent from the expected scope or, without an exact scope, from every scope.
type UnresolvedReferenceError struct {
	Kind       AuthoredItemKind
	Name       string
	Scope      Scope
	SelectedBy string
	Available  []string
}

func (failure UnresolvedReferenceError) Error() string {
	scoped := "in Global and Repository Configuration"
	if failure.Scope != "" {
		scoped = fmt.Sprintf("in %s Configuration", failure.Scope)
	}
	message := fmt.Sprintf("%s %q was not found %s", failure.Kind, failure.Name, scoped)
	if failure.SelectedBy != "" {
		message += fmt.Sprintf(", selected by %s", failure.SelectedBy)
	}
	if len(failure.Available) > 0 {
		message += "; available: " + strings.Join(failure.Available, ", ")
	}
	return message
}

func parseScopedReference(value string) (Scope, string) {
	globalPrefix, repositoryPrefix := string(ScopeGlobal)+":", string(ScopeRepository)+":"
	switch {
	case strings.HasPrefix(value, globalPrefix):
		return ScopeGlobal, strings.TrimPrefix(value, globalPrefix)
	case strings.HasPrefix(value, repositoryPrefix):
		return ScopeRepository, strings.TrimPrefix(value, repositoryPrefix)
	default:
		return "", value
	}
}

// ParseSelectionItem parses one raw selection reference while preserving the
// scope owned by the selected review group. Definition existence is validated
// later by Manager.Plan with the complete resulting document.
func ParseSelectionItem(scope Scope, profile, party string) (SelectionItem, error) {
	if profile == "" && party == "" {
		return SelectionItem{}, errors.New("choose exactly one of --profile or --party")
	}
	value := profile
	if value == "" {
		value = party
	}
	qualified, name := parseScopedReference(value)
	if qualified != "" && qualified != scope {
		return SelectionItem{}, fmt.Errorf("%s selection must reference a %s definition", value, scope)
	}
	if profile != "" {
		return SelectionItem{Profile: name}, nil
	}
	return SelectionItem{Party: name}, nil
}

// ParseProfileReferences parses raw Party member references with one shared
// scope grammar. Party creation validates the resulting references in its
// complete Plan.
func ParseProfileReferences(defaultScope Scope, values []string) ([]ProfileReference, error) {
	profiles := make([]ProfileReference, 0, len(values))
	for _, value := range values {
		scope, name := parseScopedReference(value)
		if scope == "" {
			scope = defaultScope
		}
		if scope != ScopeGlobal && scope != ScopeRepository {
			return nil, fmt.Errorf("Profile reference %q has an unknown scope", value)
		}
		profiles = append(profiles, ProfileReference{Scope: scope, Profile: name})
	}
	return profiles, nil
}

// scopedKey is one exact scoped identity used for deduplication.
type scopedKey struct {
	scope   Scope
	profile string
}

// authoredEntry carries one resolved selection entry together with the Party
// member references it expands into and the authored position it came from.
type authoredEntry struct {
	definition SelectedDefinition
	members    []ProfileReference
	hasMembers bool
	origin     string
}

// resolveExplicitProfile resolves --profile NAME to exactly one scoped
// identity. Unqualified names prefer Repository Configuration before Global.
func (manager *Manager) resolveExplicitProfile(repository Repository, value string) (ResolvedReviews, error) {
	qualified, unqualified := parseScopedReference(value)
	profileScope, err := manager.explicitProfileScope(repository, qualified, unqualified)
	if err != nil {
		return ResolvedReviews{}, err
	}
	resolved := ResolvedReviews{Kind: SelectionExplicitProfile, Source: sourceExplicit, ConcurrencyLimit: 1, LimitSource: LimitExplicitProfile}
	resolved.Authored = []SelectedDefinition{{Kind: ItemProfile, Name: unqualified, Scope: profileScope}}
	resolved.Expanded = []ExpandedProfile{{Scope: profileScope, Profile: unqualified, Origin: originExplicit}}
	return resolved, nil
}

// referenceLookup identifies one scoped definition a selection expects to
// exist, along with the authored position that selected it.
type referenceLookup struct {
	repository Repository
	scope      Scope
	kind       AuthoredItemKind
	name       string
	selectedBy string
}

// requireFound converts an absent lookup into a fail-closed resolution error.
func (lookup referenceLookup) requireFound(manager *Manager, found bool) error {
	if found {
		return nil
	}
	available := inventoryNames(manager, lookup.repository, lookup.scope, lookup.kind)
	return UnresolvedReferenceError{
		Kind: lookup.kind, Name: lookup.name, Scope: lookup.scope,
		SelectedBy: lookup.selectedBy, Available: available,
	}
}

func inventoryNames(manager *Manager, repository Repository, scope Scope, kind AuthoredItemKind) []string {
	switch kind {
	case ItemParty:
		definitions, _ := manager.PartyInventory(repository)
		names := inventoriedNames(scope, definitions, func(definition Definition[Party]) string { return definition.Name })
		return sortedKeySet(names)
	default:
		definitions, _ := manager.ProfileInventory(repository)
		names := inventoriedNames(scope, definitions, func(definition Definition[Profile]) string { return definition.Name })
		return sortedKeySet(names)
	}
}

// explicitProfileScope returns the winning scope for an explicit reference,
// loading the exact scoped name when qualified.
func (manager *Manager) explicitProfileScope(repository Repository, qualified Scope, unqualified string) (Scope, error) {
	if qualified != "" {
		_, found, err := manager.LoadProfile(qualified, repository, unqualified)
		if err != nil {
			return "", err
		}
		lookup := referenceLookup{repository: repository, scope: qualified, kind: ItemProfile, name: unqualified}
		if err := lookup.requireFound(manager, found); err != nil {
			return "", err
		}
		return qualified, nil
	}
	profile, found, err := manager.ResolveProfile(repository, unqualified)
	if err != nil {
		return "", err
	}
	lookup := referenceLookup{repository: repository, kind: ItemProfile, name: unqualified}
	if err := lookup.requireFound(manager, found); err != nil {
		return "", err
	}
	return profile.Scope, nil
}

// resolveExplicitParty resolves --party NAME and expands its flat member order.
func (manager *Manager) resolveExplicitParty(repository Repository, value string) (ResolvedReviews, error) {
	party, scope, err := manager.selectionParty(repository, value, "")
	if err != nil {
		return ResolvedReviews{}, err
	}
	origin := fmt.Sprintf("%s %s", ItemParty, party.Name)
	resolved := ResolvedReviews{Kind: SelectionExplicitParty, Source: sourceExplicit, ConcurrencyLimit: party.ConcurrencyLimit, LimitSource: LimitParty}
	expander := selectionExpander{}
	expander.expand(&resolved, repository, []authoredEntry{{
		definition: SelectedDefinition{Kind: ItemParty, Name: party.Name, Scope: scope}, members: party.Profiles, hasMembers: true, origin: origin,
	}})
	expander.finish(&resolved)
	return resolved, nil
}

// resolveDefaultSelection expands the repository's authored reviews roll-up:
// global entries first, then repository entries, preserving authored order.
func (manager *Manager) resolveDefaultSelection(repository Repository, loaded Loaded) (ResolvedReviews, error) {
	selection, value := manager.effectiveReviewSelection(loaded)
	if !value.Authored {
		return ResolvedReviews{}, ErrNoRepositorySelection
	}
	resolved := ResolvedReviews{Kind: SelectionRepositoryDefault, Source: value.Path}
	entries, err := manager.defaultEntries(repository, selection)
	if err != nil {
		return ResolvedReviews{}, err
	}
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

// defaultEntries resolves each authored group into ordered entries. A group
// name owns its members' scope: reviews.global selects Global definitions and
// reviews.repository selects Repository definitions. Parties load eagerly so a
// missing or invalid Party fails during resolution instead of during planning.
func (manager *Manager) defaultEntries(repository Repository, selection ReviewSelection) ([]authoredEntry, error) {
	entries := make([]authoredEntry, 0, len(selection.Global)+len(selection.Repository))
	groups := []struct {
		name  string
		scope Scope
		items []SelectionItem
	}{
		{"reviews.global", ScopeGlobal, selection.Global},
		{"reviews.repository", ScopeRepository, selection.Repository},
	}
	for _, group := range groups {
		for index, item := range group.items {
			entry, err := manager.selectedEntry(repository, group.scope, fmt.Sprintf("%s[%d]", group.name, index), item)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (manager *Manager) selectedEntry(repository Repository, scope Scope, origin string, item SelectionItem) (authoredEntry, error) {
	name, err := item.Name()
	if err != nil {
		return authoredEntry{}, fmt.Errorf("%s: %w", origin, err)
	}
	switch {
	case selectsProfile(item):
		return manager.profileSelectionEntry(repository, scope, origin, name)
	case selectsParty(item):
		party, _, err := manager.selectionPartyAt(repository, scope, name, origin)
		if err != nil {
			return authoredEntry{}, err
		}
		return partyEntry(name, scope, origin, party), nil
	default:
		return authoredEntry{}, fmt.Errorf("%s: must select exactly one profile or party", origin)
	}
}

func selectsProfile(item SelectionItem) bool {
	return item.Profile != "" && item.Party == ""
}

func selectsParty(item SelectionItem) bool {
	return item.Profile == "" && item.Party != ""
}

func (manager *Manager) profileSelectionEntry(repository Repository, scope Scope, origin, name string) (authoredEntry, error) {
	_, found, err := manager.LoadProfile(scope, repository, name)
	if err != nil {
		return authoredEntry{}, fmt.Errorf("%s: %w", origin, err)
	}
	lookup := referenceLookup{repository: repository, scope: scope, kind: ItemProfile, name: name, selectedBy: origin}
	if err := lookup.requireFound(manager, found); err != nil {
		return authoredEntry{}, err
	}
	return authoredEntry{definition: SelectedDefinition{Kind: ItemProfile, Name: name, Scope: scope}, origin: origin}, nil
}

func partyEntry(name string, scope Scope, origin string, party Party) authoredEntry {
	return authoredEntry{
		definition: SelectedDefinition{Kind: ItemParty, Name: name, Scope: scope},
		members:    party.Profiles, hasMembers: true, origin: origin,
	}
}

// selectionParty loads one possibly qualified Party for a run selection,
// preferring Repository Configuration before Global for unqualified names.
func (manager *Manager) selectionParty(repository Repository, value, origin string) (Party, Scope, error) {
	qualified, unqualified := parseScopedReference(value)
	if qualified != "" {
		return manager.selectionPartyAt(repository, qualified, unqualified, origin)
	}
	party, found, err := manager.ResolveParty(repository, unqualified)
	if err != nil {
		return Party{}, "", err
	}
	lookup := referenceLookup{repository: repository, kind: ItemParty, name: unqualified, selectedBy: origin}
	if err := lookup.requireFound(manager, found); err != nil {
		return Party{}, "", err
	}
	return party, party.Scope, nil
}

func (manager *Manager) selectionPartyAt(repository Repository, scope Scope, name, origin string) (Party, Scope, error) {
	party, found, err := manager.LoadParty(scope, repository, name)
	if err != nil {
		return Party{}, "", err
	}
	lookup := referenceLookup{repository: repository, scope: scope, kind: ItemParty, name: name, selectedBy: origin}
	if err := lookup.requireFound(manager, found); err != nil {
		return Party{}, "", err
	}
	return party, scope, nil
}

func inventoriedNames[T any](scope Scope, definitions []Definition[T], nameOf func(Definition[T]) string) map[string]struct{} {
	names := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if scope != "" && definition.Scope != scope {
			continue
		}
		names[nameOf(definition)] = struct{}{}
	}
	return names
}

func sortedKeySet(names map[string]struct{}) []string {
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

// selectionExpander turns authored entries into the ordered expanded
// execution list while recording exact-identity deduplication.
type selectionExpander struct {
	firstOrigins map[scopedKey]string
	order        []scopedKey
}

func (expander *selectionExpander) expand(resolved *ResolvedReviews, repository Repository, entries []authoredEntry) {
	for _, entry := range entries {
		resolved.Authored = append(resolved.Authored, entry.definition)
		references, origins := expansionReferences(entry)
		for index, reference := range references {
			key := scopedKey{scope: reference.Scope, profile: reference.Profile}
			if firstOrigin, duplicated := expander.firstOrigins[key]; duplicated {
				resolved.Deduplicated = append(resolved.Deduplicated, SkippedProfile{
					Scope: reference.Scope, Profile: reference.Profile, Origin: origins[index], KeptOrigin: firstOrigin,
				})
				continue
			}
			if expander.firstOrigins == nil {
				expander.firstOrigins = make(map[scopedKey]string)
			}
			expander.firstOrigins[key] = origins[index]
			expander.order = append(expander.order, key)
			resolved.Expanded = append(resolved.Expanded, ExpandedProfile{Scope: reference.Scope, Profile: reference.Profile, Origin: origins[index]})
		}
	}
}

// finish derives post-expansion warnings once the full ordered list exists.
func (expander *selectionExpander) finish(resolved *ResolvedReviews) {
	resolved.Warnings = crossScopeNameWarnings(resolved.Expanded)
}

// expansionReferences flattens one authored entry into its scoped references
// and per-reference origins. Party members carry their member ordinal.
func expansionReferences(entry authoredEntry) ([]ProfileReference, []string) {
	if !entry.hasMembers {
		reference := ProfileReference{Scope: entry.definition.Scope, Profile: entry.definition.Name}
		return []ProfileReference{reference}, []string{entry.origin}
	}
	references := make([]ProfileReference, 0, len(entry.members))
	origins := make([]string, 0, len(entry.members))
	for index, member := range entry.members {
		references = append(references, ProfileReference{Scope: member.Scope, Profile: member.Profile})
		origins = append(origins, fmt.Sprintf("%s#%d", entry.origin, index))
	}
	return references, origins
}

// crossScopeNameWarnings flags expanded Profiles whose name runs from both
// scopes. Same-named Profiles are distinct identities and both execute.
func crossScopeNameWarnings(expanded []ExpandedProfile) []ResolverWarning {
	scopesPerName := collectScopesByName(expanded)
	names := make([]string, 0, len(scopesPerName))
	for name := range scopesPerName {
		names = append(names, name)
	}
	sort.Strings(names)
	warnings := make([]ResolverWarning, 0, len(names))
	for _, name := range names {
		scopes := scopesPerName[name]
		_, globalScoped := scopes[ScopeGlobal]
		_, repositoryScoped := scopes[ScopeRepository]
		if !globalScoped || !repositoryScoped {
			continue
		}
		warnings = append(warnings, ResolverWarning{
			Category: WarningSameNameCrossScope,
			Name:     name,
			Message: fmt.Sprintf(
				"Profiles named %q from both Global and Repository Configuration will run; they are distinct Profiles",
				name,
			),
		})
	}
	return warnings
}

// collectScopesByName maps every expanded Profile name to the scopes it runs in.
func collectScopesByName(expanded []ExpandedProfile) map[string]map[Scope]struct{} {
	scopesPerName := make(map[string]map[Scope]struct{}, len(expanded))
	for _, slot := range expanded {
		scopes, exists := scopesPerName[slot.Profile]
		if !exists {
			scopes = make(map[Scope]struct{}, 2)
			scopesPerName[slot.Profile] = scopes
		}
		scopes[slot.Scope] = struct{}{}
	}
	return scopesPerName
}
