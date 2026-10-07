package main

// Doctor's repository findings report what this clone and this Caller still
// lack for the configuration the repository declares. Each finding carries
// the command that closes it. Doctor only reports, so findings never change
// its exit code; the Caller and the repository's instructions decide whether
// one blocks anything.

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

// recentWaiverWindow is how far back doctor lists Waivers.
const recentWaiverWindow = 30 * 24 * time.Hour

// unresolvedName is a definition the repository selects that this Caller's
// configuration does not define.
type unresolvedName struct {
	Kind       configuration.AuthoredItemKind `json:"kind"`
	Name       string                         `json:"name"`
	Scope      configuration.Scope            `json:"scope"`
	SelectedBy string                         `json:"selected_by"`
	Fix        string                         `json:"fix"`
	label      string
}

// exemptionConflict is a Checkpoint that exempts the Markdown files a
// selected documentation Profile reviews, so documentation changes pass it
// without that Review.
type exemptionConflict struct {
	Checkpoint  configuration.CheckpointName `json:"checkpoint"`
	ExemptPaths []string                     `json:"exempt_paths"`
	Profiles    []string                     `json:"profiles"`
	Fix         string                       `json:"fix"`
}

// waiversUnread reports a ledger doctor cannot read until state preparation
// upgrades it.
type waiversUnread struct {
	Reason string `json:"reason"`
	Fix    string `json:"fix"`
}

// doctorRepository is the resolved repository root the findings describe,
// with the --config path their fixes carry.
type doctorRepository struct {
	manager       *configuration.Manager
	root          string
	configuration string
}

// addFindings fills the repository findings. Unresolved names are read even
// from an invalid configuration, since they are often why it is invalid;
// the rest need a configuration that validates.
func (repository doctorRepository) addFindings(result *doctorResult) error {
	result.UnresolvedNames = repository.unresolvedNames()
	if !result.Valid {
		return nil
	}
	gaps, err := planFloorGaps(repository.manager, repository.root, repository.configuration)
	if err != nil {
		return err
	}
	result.IntegrationGaps = append(result.IntegrationGaps, gaps...)
	conflicts, err := repository.exemptionConflicts()
	if err != nil {
		return err
	}
	result.ExemptionConflicts = append(result.ExemptionConflicts, conflicts...)
	return repository.addRecentWaivers(result)
}

func (repository doctorRepository) unresolvedNames() []unresolvedName {
	names := []unresolvedName{}
	binding, err := repository.manager.SelectionBinding(configuration.Repository(repository.root))
	if err != nil {
		return names
	}
	report := initReport{manager: repository.manager, repository: configuration.Repository(repository.root), config: configurationArgument(repository.configuration)}
	for _, missing := range binding.Unresolved {
		label, create := report.definitionFix(missing)
		names = append(names, unresolvedName{Kind: missing.Kind, Name: missing.Name, Scope: missing.Scope, SelectedBy: missing.SelectedBy, Fix: create, label: label})
	}
	return names
}

func (repository doctorRepository) exemptionConflicts() ([]exemptionConflict, error) {
	declared, err := repository.manager.Checkpoints(configuration.Repository(repository.root))
	if err != nil {
		return nil, err
	}
	profiles, err := repository.manager.SelectedDocumentationProfiles(configuration.Repository(repository.root))
	if err != nil || len(profiles) == 0 {
		return nil, err
	}
	var conflicts []exemptionConflict
	for _, name := range configuration.SortedCheckpointNames(declared) {
		checkpoint := declared[name]
		markdown := checkpoint.MarkdownExemptions()
		if len(markdown) == 0 {
			continue
		}
		checkpoint.ExemptPaths = slices.DeleteFunc(slices.Clone(checkpoint.ExemptPaths), func(pattern string) bool { return slices.Contains(markdown, pattern) })
		conflicts = append(conflicts, exemptionConflict{Checkpoint: name, ExemptPaths: markdown, Profiles: profiles, Fix: repository.checkpointSet(name, checkpoint)})
	}
	return conflicts, nil
}

// checkpointSet is the command that declares the Checkpoint exactly as
// given. A set replaces the whole declaration, so every non-default value is
// repeated.
func (repository doctorRepository) checkpointSet(name configuration.CheckpointName, checkpoint configuration.Checkpoint) string {
	command := []string{"review-party config checkpoint set", string(name), "--repo", shellWord(repository.root)}
	if checkpoint.Requirement != configuration.RequirementReviewed {
		command = append(command, "--requirement", string(checkpoint.Requirement))
	}
	for _, pattern := range checkpoint.ExemptPaths {
		command = append(command, "--exempt", shellWord(pattern))
	}
	if checkpoint.UnreviewedLines != configuration.DefaultUnreviewedLines {
		command = append(command, "--unreviewed-lines", strconv.Itoa(checkpoint.UnreviewedLines))
	}
	if checkpoint.ReviewBudget != configuration.DefaultReviewBudget {
		command = append(command, "--review-budget", strconv.Itoa(checkpoint.ReviewBudget))
	}
	if checkpoint.Waivers != configuration.WaiversHuman {
		command = append(command, "--waivers", string(checkpoint.Waivers))
	}
	for _, integration := range checkpoint.Integrations {
		command = append(command, "--integration", string(integration))
	}
	return strings.Join(command, " ") + configurationArgument(repository.configuration)
}

// addRecentWaivers lists the last month's Waivers from the ledger, which
// reading never creates.
func (repository doctorRepository) addRecentWaivers(result *doctorResult) error {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: repository.configuration})
	if err != nil {
		return err
	}
	waivers, err := conductor.RecentCheckpointWaivers(repository.root, recentWaiverWindow)
	if errors.Is(err, store.ErrReviewRecordStateRequiresPreparation) {
		result.WaiversUnread = &waiversUnread{Reason: "the review ledger needs an upgrade before doctor can read it", Fix: "review-party init --repo " + shellWord(repository.root) + configurationArgument(repository.configuration)}
		return nil
	}
	if err != nil {
		return err
	}
	result.RecentWaivers = waivers
	return nil
}

// findingLines renders one line per finding, each ending in its fix. A
// Waiver is a record, not a gap, so its line has none.
func (result doctorResult) findingLines() []string {
	var lines []string
	for _, name := range result.UnresolvedNames {
		lines = append(lines, name.label+", selected by "+name.SelectedBy+"; fix: "+name.Fix)
	}
	for _, gap := range result.IntegrationGaps {
		lines = append(lines, gap.summary()+"; fix: "+gap.Fix)
	}
	for _, conflict := range result.ExemptionConflicts {
		lines = append(lines, fmt.Sprintf("Checkpoint %s exempts %s, so changes the documentation Profile %s reviews pass it unreviewed; fix: %s",
			conflict.Checkpoint, strings.Join(conflict.ExemptPaths, ", "), strings.Join(conflict.Profiles, ", "), conflict.Fix))
	}
	if result.WaiversUnread != nil {
		lines = append(lines, "Recent Waivers unread: "+result.WaiversUnread.Reason+"; fix: "+result.WaiversUnread.Fix)
	}
	for _, waiver := range result.RecentWaivers {
		lines = append(lines, waiverLine(waiver))
	}
	return lines
}

// waiverLine renders one Waiver on one line, folding the line breaks a
// reason may carry.
func waiverLine(waiver model.CheckpointWaiver) string {
	return fmt.Sprintf("Waiver %s waived %s on %s (%s): %s", waiver.ID, waiver.Key.Checkpoint, waiver.CreatedAt.UTC().Format(time.RFC3339), waiver.WaivedBy, strings.Join(strings.Fields(waiver.Reason), " "))
}
