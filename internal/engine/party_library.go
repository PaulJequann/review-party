package engine

import (
	"sort"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

type PartySummary = model.PartySummary

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
