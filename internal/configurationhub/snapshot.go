package configurationhub

import (
	"fmt"

	"reviewparty/internal/configuration"
)

// buildSnapshot adapts the Manager's configuration read model for the Hub.
// The Bubble Tea model consumes this immutable view without owning storage.
func buildSnapshot(manager *configuration.Manager, repository configuration.Repository) (Snapshot, error) {
	snapshot := Snapshot{Repository: string(repository)}
	for _, template := range manager.Templates() {
		snapshot.Items = append(snapshot.Items, Item{Scope: "template", Kind: itemTemplate, Name: template.ID, Detail: "Review Profile Template " + template.Revision})
	}
	profiles, err := manager.ProfileInventory(repository)
	if err != nil {
		return snapshot, err
	}
	appendHubProfiles(&snapshot, profiles)
	parties, err := manager.PartyInventory(repository)
	if err != nil {
		return snapshot, err
	}
	appendHubParties(&snapshot, parties)
	selection, source, err := manager.EffectiveReviewSelection(repository)
	if err != nil {
		return snapshot, err
	}
	snapshot.Overview = append(snapshot.Overview,
		fmt.Sprintf("Global: %d Profiles, %d Parties", countHubProfiles(profiles, configuration.ScopeGlobal), countHubParties(parties, configuration.ScopeGlobal)),
		fmt.Sprintf("Repository: %d Profiles, %d Parties", countHubProfiles(profiles, configuration.ScopeRepository), countHubParties(parties, configuration.ScopeRepository)),
	)
	if !source.Authored {
		snapshot.Overview = append(snapshot.Overview, "Repository Reviews: not configured")
		return snapshot, nil
	}
	snapshot.Overview = append(snapshot.Overview, fmt.Sprintf("Repository Reviews: %d selected · concurrency %d (%s)", len(selection.Global)+len(selection.Repository), selection.ConcurrencyLimit, source.Path))
	appendHubReviews(&snapshot, "global", selection.Global)
	appendHubReviews(&snapshot, "repository", selection.Repository)
	appendResolvedPreview(manager, repository, &snapshot)
	return snapshot, nil
}

func appendResolvedPreview(manager *configuration.Manager, repository configuration.Repository, snapshot *Snapshot) {
	resolved, err := manager.ResolveRun(configuration.RunRequest{Repository: repository})
	if err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "effective selection: "+err.Error())
		return
	}
	snapshot.Overview = append(snapshot.Overview, configuration.RenderResolvedReviewLinesHuman(resolved)...)
	for _, warning := range resolved.Warnings {
		snapshot.Warnings = append(snapshot.Warnings, warning.Message)
	}
}

func appendHubProfiles(snapshot *Snapshot, profiles []configuration.Definition[configuration.Profile]) {
	for _, profile := range profiles {
		detail := profile.Value.Reviewer + " / " + profile.Value.Model
		if profile.Err != nil {
			detail = "invalid: " + profile.Err.Error()
		}
		snapshot.Items = append(snapshot.Items, Item{Scope: ItemScope(profile.Scope), Kind: itemProfile, Name: profile.Name, Detail: detail})
	}
}

func appendHubParties(snapshot *Snapshot, parties []configuration.Definition[configuration.Party]) {
	for _, party := range parties {
		detail := fmt.Sprintf("%d Profiles · concurrency %d", len(party.Value.Profiles), party.Value.ConcurrencyLimit)
		if party.Err != nil {
			detail = "invalid: " + party.Err.Error()
		}
		snapshot.Items = append(snapshot.Items, Item{Scope: ItemScope(party.Scope), Kind: itemParty, Name: party.Name, Detail: detail})
	}
}

func appendHubReviews(snapshot *Snapshot, scope string, items []configuration.SelectionItem) {
	for _, item := range items {
		name, err := item.Name()
		if err != nil {
			snapshot.Warnings = append(snapshot.Warnings, err.Error())
			continue
		}
		detail := "Profile"
		if item.Party != "" {
			detail = "Party"
		}
		snapshot.Items = append(snapshot.Items, Item{Scope: ItemScope(scope), Kind: itemReview, Name: name, Detail: detail})
	}
}

func countHubProfiles(values []configuration.Definition[configuration.Profile], scope configuration.Scope) int {
	count := 0
	for _, value := range values {
		if value.Scope == scope {
			count++
		}
	}
	return count
}

func countHubParties(values []configuration.Definition[configuration.Party], scope configuration.Scope) int {
	count := 0
	for _, value := range values {
		if value.Scope == scope {
			count++
		}
	}
	return count
}
