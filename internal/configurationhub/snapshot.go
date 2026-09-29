package configurationhub

import (
	"fmt"
	"strings"

	"reviewparty/internal/configuration"
)

// ProfileReceipt is one profile's execution aggregate for display. The Hub
// never queries run storage itself; the caller supplies these read-only.
type ProfileReceipt struct {
	Runs           int
	MedianDuration string
	TotalFindings  int
	IncompleteRuns int
}

// ReceiptProvider returns the receipt for one recorded profile name.
// The second result is false when no runs are recorded.
type ReceiptProvider func(profile string) (ProfileReceipt, bool)

// attachReceipts annotates profile inventory details with execution receipts.
// Entries without a receipt render exactly as before.
func attachReceipts(snapshot *Snapshot, receipts ReceiptProvider) {
	if receipts == nil {
		return
	}
	for index := range snapshot.Items {
		item := &snapshot.Items[index]
		if item.Kind != itemProfile {
			continue
		}
		receipt, found := receipts(item.Name)
		if !found {
			continue
		}
		item.Detail += " · " + FormatReceipt(receipt)
	}
}

func FormatReceipt(receipt ProfileReceipt) string {
	parts := []string{"last " + formatReceiptRuns(receipt.Runs)}
	if receipt.IncompleteRuns > 0 {
		parts = append(parts, formatReceiptIncomplete(receipt.IncompleteRuns))
	}
	if receipt.MedianDuration != "" {
		parts = append(parts, "med "+receipt.MedianDuration)
	}
	parts = append(parts, formatReceiptFindings(receipt.TotalFindings))
	return strings.Join(parts, " · ")
}

func formatReceiptIncomplete(count int) string {
	if count == 1 {
		return "1 incomplete"
	}
	return fmt.Sprintf("%d incomplete", count)
}

func formatReceiptRuns(runs int) string {
	if runs == 1 {
		return "1 run"
	}
	return fmt.Sprintf("%d runs", runs)
}

func formatReceiptFindings(findings int) string {
	if findings == 1 {
		return "1 finding"
	}
	return fmt.Sprintf("%d findings", findings)
}
func buildSnapshot(manager *configuration.Manager, repository configuration.Repository) (Snapshot, error) {
	snapshot := Snapshot{Repository: string(repository), Templates: manager.Templates()}
	profiles, err := manager.ProfileInventory(repository)
	if err != nil {
		return snapshot, err
	}
	appendHubProfiles(&snapshot, profiles)
	drift := manager.TemplateDriftForProfiles(profiles)
	appendTemplateDrift(&snapshot, drift)
	for _, skipped := range manager.SkippedTemplates() {
		snapshot.Warnings = append(snapshot.Warnings, fmt.Sprintf("Template %s skipped: %s", skipped.TemplateID, skipped.Reason))
	}
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
		fmt.Sprintf("Global: %s, %s", pluralizedCount("Profile", countHubProfiles(profiles, configuration.ScopeGlobal)), pluralizedCount("Party", countHubParties(parties, configuration.ScopeGlobal))),
		fmt.Sprintf("Repository: %s, %s", pluralizedCount("Profile", countHubProfiles(profiles, configuration.ScopeRepository)), pluralizedCount("Party", countHubParties(parties, configuration.ScopeRepository))),
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

func appendTemplateDrift(snapshot *Snapshot, drift []configuration.TemplateDrift) {
	for _, item := range drift {
		snapshot.Warnings = append(snapshot.Warnings, templateDriftMessage(item))
		markDriftedProfile(snapshot, item)
	}
}

func templateDriftMessage(item configuration.TemplateDrift) string {
	if item.Status == configuration.TemplateSourceUnavailable {
		return fmt.Sprintf("%s Profile %q: Template %s source unavailable; saved instructions still run", item.Scope, item.Profile, item.TemplateID)
	}
	message := fmt.Sprintf("%s Profile %q: Template %s drift (%s → %s)", item.Scope, item.Profile, item.TemplateID, item.TemplateRevision, item.AvailableRevision)
	if item.Customized {
		message += "; updating replaces customized instructions"
	}
	return message
}

func templateDriftDetail(item configuration.TemplateDrift) string {
	if item.Status == configuration.TemplateSourceUnavailable {
		return " · Template source unavailable"
	}
	return " · Template update available"
}

func markDriftedProfile(snapshot *Snapshot, item configuration.TemplateDrift) {
	for index := range snapshot.Items {
		candidate := &snapshot.Items[index]
		if candidate.Kind != itemProfile {
			continue
		}
		if candidate.Scope != ItemScope(item.Scope) {
			continue
		}
		if candidate.Name != item.Profile {
			continue
		}
		candidate.Detail += templateDriftDetail(item)
	}
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
		detail := fmt.Sprintf("%s · concurrency %d", pluralizedCount("Profile", len(party.Value.Profiles)), party.Value.ConcurrencyLimit)
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

// pluralizedCount renders one count with its noun, using the singular only
// for exactly one so counts like "1 Parties" never appear. Nouns ending in a
// consonant + y pluralize as -ies (Party becomes Parties).
func pluralizedCount(noun string, count int) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	if endsInConsonantY(noun) {
		return fmt.Sprintf("%d %sies", count, noun[:len(noun)-1])
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func endsInConsonantY(noun string) bool {
	if len(noun) < 2 || noun[len(noun)-1] != 'y' {
		return false
	}
	previous := rune(noun[len(noun)-2])
	return !strings.ContainsRune("aeiouAEIOU", previous)
}
