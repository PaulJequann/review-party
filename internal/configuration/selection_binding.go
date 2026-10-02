package configuration

import "fmt"

// ResolveSelectionTarget resolves one possibly qualified Profile or Party name
// to the selection group that owns it. An unqualified name prefers Repository
// Configuration before Global, the same precedence as an explicit run.
func (manager *Manager) ResolveSelectionTarget(repository Repository, kind AuthoredItemKind, value string) (Scope, SelectionItem, error) {
	resolver := selectionResolver{lookup: manager.selectionLookup(repository)}
	scope, name, err := resolver.definitionScope(repository, kind, value)
	if err != nil {
		return "", SelectionItem{}, err
	}
	if kind == ItemParty {
		return scope, SelectionItem{Party: name}, nil
	}
	return scope, SelectionItem{Profile: name}, nil
}

// SelectionBinding reports whether a repository declares a Review selection
// and which of its references this machine's configuration cannot resolve.
type SelectionBinding struct {
	Declared   bool
	Unresolved []UnresolvedReferenceError
}

// Ready reports whether the declared selection resolves completely.
func (binding SelectionBinding) Ready() bool {
	return binding.Declared && len(binding.Unresolved) == 0
}

// SelectionBinding enumerates every unresolved reference in the effective
// selection, including members of selected Parties. Each missing definition
// is reported once, at its first authored position.
func (manager *Manager) SelectionBinding(repository Repository) (SelectionBinding, error) {
	selection, value, err := manager.EffectiveReviewSelection(repository)
	if err != nil {
		return SelectionBinding{}, err
	}
	collector := unresolvedCollector{
		resolver: selectionResolver{lookup: manager.selectionLookup(repository)},
		seen:     make(map[selectionReference]struct{}),
	}
	binding := SelectionBinding{Declared: value.Authored && len(selection.Global)+len(selection.Repository) > 0}
	for _, group := range selectionGroups(selection) {
		for index, item := range group.items {
			if err := collector.item(group.scope, group.origin(index), item); err != nil {
				return SelectionBinding{}, err
			}
		}
	}
	binding.Unresolved = collector.unresolved
	return binding, nil
}

type unresolvedCollector struct {
	resolver   selectionResolver
	seen       map[selectionReference]struct{}
	unresolved []UnresolvedReferenceError
}

func (collector *unresolvedCollector) item(scope Scope, origin string, item SelectionItem) error {
	name, err := item.Name()
	if err != nil {
		return fmt.Errorf("%s: %w", origin, err)
	}
	if selectsProfile(item) {
		return collector.require(ItemProfile, scope, name, origin)
	}
	party, found, err := collector.resolver.lookup.partyAt(scope, name)
	if err != nil {
		return fmt.Errorf("%s: %w", origin, err)
	}
	if !found {
		collector.record(selectionReference{scope: scope, kind: ItemParty, name: name, selectedBy: origin})
		return nil
	}
	for index, member := range party.Profiles {
		if err := collector.require(ItemProfile, member.Scope, member.Profile, fmt.Sprintf("%s#%d", origin, index)); err != nil {
			return err
		}
	}
	return nil
}

func (collector *unresolvedCollector) require(kind AuthoredItemKind, scope Scope, name, origin string) error {
	found, err := collector.resolver.exists(kind, scope, name)
	if err != nil {
		return fmt.Errorf("%s: %w", origin, err)
	}
	if !found {
		collector.record(selectionReference{scope: scope, kind: kind, name: name, selectedBy: origin})
	}
	return nil
}

func (collector *unresolvedCollector) record(reference selectionReference) {
	key := reference
	key.selectedBy = ""
	if _, duplicate := collector.seen[key]; duplicate {
		return
	}
	collector.seen[key] = struct{}{}
	collector.unresolved = append(collector.unresolved, collector.resolver.unresolved(reference))
}
