package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
)

// PartyDraft contains the complete authored Party.
type PartyDraft struct {
	Target           Scope
	Name             string
	Description      string
	ConcurrencyLimit int
	Profiles         []ProfileReference
}

// PlanPartyCreation validates and stages one flat Party definition.
func (manager *Manager) PlanPartyCreation(repository Repository, draft PartyDraft) (Plan, error) {
	party := Party{SchemaVersion: SchemaVersion, Name: draft.Name, Description: draft.Description, ConcurrencyLimit: draft.ConcurrencyLimit, Profiles: append([]ProfileReference(nil), draft.Profiles...)}
	plan := Plan{state: &planState{owner: manager}}
	if err := manager.validatePartyCreation(repository, draft.Target, party); err != nil {
		plan.state.reason = err.Error()
		return plan, nil
	}
	entry, anchor, err := manager.partyEntry(draft.Target, repository, party.Name)
	if err != nil {
		return Plan{}, err
	}
	if _, err := os.Lstat(entry.Path); err == nil {
		plan.state.reason = fmt.Sprintf("Party %q already exists", party.Name)
		return plan, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Plan{}, err
	}
	payload, err := renderParty(party)
	if err != nil {
		return Plan{}, err
	}
	if err := partyDefinitionPayload.validate(payload); err != nil {
		plan.state.reason = err.Error()
		return plan, nil
	}
	write := pendingWrite{scope: draft.Target, anchor: anchor, path: entry.Path, payload: payload}
	change := Change{
		Field: "parties." + party.Name, Scope: draft.Target, Path: entry.Path,
		After: fmt.Sprintf("profiles=%d concurrency=%d", len(party.Profiles), party.ConcurrencyLimit), HadAfter: true,
	}
	return newFilePlan(manager, draft.Target, change, []pendingWrite{write}), nil
}

func (manager *Manager) validatePartyCreation(repository Repository, scope Scope, party Party) error {
	if err := manager.validateParty(party, scope); err != nil {
		return err
	}
	return manager.validatePartyReferences(repository, party)
}

func renderParty(party Party) ([]byte, error) {
	party.Scope, party.Source = "", ""
	payload, err := json.MarshalIndent(party, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

// PlanProfileCopy stages an exact Profile copy in another scope. The copied
// Profile keeps its Template provenance and receives a new scoped source.
func (manager *Manager) PlanProfileCopy(repository Repository, source, target Scope, name string) (Plan, error) {
	profile, found, err := manager.LoadProfile(source, repository, name)
	if err != nil {
		return Plan{}, err
	}
	if !found {
		return Plan{}, fmt.Errorf("Profile %q does not exist in %s Configuration", name, source)
	}
	return manager.planProfileCopy(repository, target, profile)
}

// PlanProfileCopyFromReference stages a Profile copy after resolving one raw
// qualified or precedence-based reference inside the Configuration Manager.
func (manager *Manager) PlanProfileCopyFromReference(repository Repository, value string, target Scope) (Plan, error) {
	source, name := ParseScopedReference(value)
	var (
		profile Profile
		found   bool
		err     error
	)
	if source == "" {
		profile, found, err = manager.ResolveProfile(repository, name)
	} else {
		profile, found, err = manager.LoadProfile(source, repository, name)
	}
	if err != nil {
		return Plan{}, err
	}
	if !found {
		return Plan{}, fmt.Errorf("Profile %q was not found", name)
	}
	return manager.planProfileCopy(repository, target, profile)
}

func (manager *Manager) planProfileCopy(repository Repository, target Scope, profile Profile) (Plan, error) {
	return manager.planProfile(repository, target, profile, profile.Instructions)
}

// DefaultReviewSelection is the valid starting value for a repository's
// first saved selection mutation.
func DefaultReviewSelection() ReviewSelection {
	return ReviewSelection{ConcurrencyLimit: 1}
}

// AddReviewSelection returns a typed replacement intent with one item appended
// to its group. Adding an item the group already selects changes nothing.
func AddReviewSelection(current ReviewSelection, group Scope, item SelectionItem) SetReviewSelection {
	updated := cloneReviewSelection(current)
	items := selectionGroup(&updated, group)
	if !slices.Contains(*items, item) {
		*items = append(*items, item)
	}
	return SetReviewSelection{Selection: updated}
}

// RemoveReviewSelection returns a typed replacement intent without one item.
func RemoveReviewSelection(current ReviewSelection, group Scope, index int) (SetReviewSelection, error) {
	updated := cloneReviewSelection(current)
	items := selectionGroup(&updated, group)
	if index < 0 || index >= len(*items) {
		return SetReviewSelection{}, fmt.Errorf("review selection index %d is out of range", index)
	}
	*items = append((*items)[:index], (*items)[index+1:]...)
	return SetReviewSelection{Selection: updated}, nil
}

// MoveReviewSelection returns a typed replacement intent with one item moved.
func MoveReviewSelection(current ReviewSelection, group Scope, from, to int) (SetReviewSelection, error) {
	updated := cloneReviewSelection(current)
	items := selectionGroup(&updated, group)
	if err := validateSelectionMove(len(*items), from, to); err != nil {
		return SetReviewSelection{}, err
	}
	moveSelectionItem(*items, from, to)
	return SetReviewSelection{Selection: updated}, nil
}

func moveSelectionItem(items []SelectionItem, from, to int) {
	item := items[from]
	if from < to {
		copy(items[from:to], items[from+1:to+1])
	} else {
		copy(items[to+1:from+1], items[to:from])
	}
	items[to] = item
}

func validateSelectionMove(length, from, to int) error {
	if from < 0 || from >= length {
		return fmt.Errorf("review selection source index %d is out of range", from)
	}
	if to < 0 || to >= length {
		return fmt.Errorf("review selection destination index %d is out of range", to)
	}
	return nil
}

func cloneReviewSelection(selection ReviewSelection) ReviewSelection {
	selection.Global = cloneSelectionItems(selection.Global)
	selection.Repository = cloneSelectionItems(selection.Repository)
	return selection
}

func cloneSelectionItems(items []SelectionItem) []SelectionItem {
	clone := make([]SelectionItem, len(items))
	copy(clone, items)
	return clone
}

func selectionGroup(selection *ReviewSelection, group Scope) *[]SelectionItem {
	if group == ScopeGlobal {
		return &selection.Global
	}
	return &selection.Repository
}
