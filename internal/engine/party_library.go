package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

type PartySummary = model.PartySummary

type partyLookup struct {
	repository string
	name       string
}

func (conductor *Conductor) resolveParty(lookup partyLookup) (model.PartyDefinition, string, error) {
	if conductor.configuration == nil {
		return model.PartyDefinition{}, "", errors.New("party library requires a configuration manager")
	}
	party, found, err := conductor.loadParty(lookup)
	if err != nil {
		return model.PartyDefinition{}, "", err
	}
	if !found {
		return model.PartyDefinition{}, "", UnknownPartyError{Name: lookup.name, Available: conductor.partyNames(lookup.repository)}
	}
	return partyDefinition(party), party.Source, nil
}

func (conductor *Conductor) loadParty(lookup partyLookup) (configuration.Party, bool, error) {
	return conductor.configuration.ResolveParty(configuration.Repository(lookup.repository), lookup.name)
}

func partyDefinition(party configuration.Party) model.PartyDefinition {
	definition := model.PartyDefinition{SchemaVersion: party.SchemaVersion, Name: party.Name, Description: party.Description, ConcurrencyLimit: party.ConcurrencyLimit}
	for _, reference := range party.Profiles {
		definition.Profiles = append(definition.Profiles, model.PartyMember{Scope: string(reference.Scope), Profile: reference.Profile})
	}
	return definition
}

type UnknownPartyError struct {
	Name      string
	Available []string
}

func (failure UnknownPartyError) Error() string {
	return fmt.Sprintf("unknown review party %q; expected %s", failure.Name, strings.Join(failure.Available, ", "))
}

type InvalidPartyDefinitionError struct {
	Name   string
	Reason string
}

func (failure InvalidPartyDefinitionError) Error() string {
	return fmt.Sprintf("invalid party definition %q: %s", failure.Name, failure.Reason)
}

func (conductor *Conductor) partyNames(repository string) []string {
	inventory, err := conductor.configuration.PartyInventory(configuration.Repository(repository))
	if err != nil {
		return nil
	}
	names := make(map[string]struct{}, len(inventory))
	for _, definition := range inventory {
		if definition.Err == nil {
			names[definition.Name] = struct{}{}
		}
	}
	available := make([]string, 0, len(names))
	for name := range names {
		available = append(available, name)
	}
	sort.Strings(available)
	return available
}

func (conductor *Conductor) PartiesForRepository(repository string) ([]PartySummary, error) {
	root, err := resolvePartyRepositoryRoot(repository)
	if err != nil {
		return nil, err
	}
	inventory, err := conductor.configuration.PartyInventory(configuration.Repository(root))
	if err != nil {
		return nil, err
	}
	summaries := make([]PartySummary, 0, len(inventory))
	for _, definition := range inventory {
		summaries = append(summaries, partyInventorySummary(definition))
	}
	sort.SliceStable(summaries, func(left, right int) bool { return summaries[left].Name < summaries[right].Name })
	return summaries, nil
}

func partyInventorySummary(definition configuration.Definition[configuration.Party]) PartySummary {
	summary := PartySummary{Name: definition.Name, Source: definition.Source}
	if definition.Err != nil {
		summary.Error = definition.Err.Error()
		return summary
	}
	party := definition.Value
	summary.Description, summary.Source = party.Description, party.Source
	for _, member := range party.Profiles {
		summary.Members = append(summary.Members, model.PartyMember{Scope: string(member.Scope), Profile: member.Profile})
	}
	return summary
}

func resolvePartyRepositoryRoot(repository string) (string, error) {
	if repository == "" {
		return "", nil
	}
	return subject.ResolveRepositoryRoot(repository)
}
