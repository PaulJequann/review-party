package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
)

type partiesOptions struct {
	repository    string
	format        string
	configuration string
}

func executeParties(ctx context.Context, options partiesOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	summaries, err := conductor.PartiesForRepository(options.repository)
	if err != nil {
		return printFailure(stderr, err)
	}
	if options.format != "json" && options.format != "human" {
		return printCommandError(stderr, usageExitCode, fmt.Errorf("unknown output format %q", options.format))
	}
	return printPartySummaries(stdout, summaries, options.format)
}

func printPartySummaries(stdout io.Writer, summaries []model.PartySummary, format string) int {
	if format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(summaries); err != nil {
			return 1
		}
		return 0
	}
	if err := writeCommandOutput(stdout, func(output *commandOutput) {
		printHumanPartySummaries(output, summaries)
	}); err != nil {
		return 1
	}
	return 0
}

func printHumanPartySummaries(stdout *commandOutput, summaries []model.PartySummary) {
	for _, summary := range summaries {
		description := summary.Description
		if summary.Error != "" {
			description = summary.Error
		}
		stdout.write("%s · %s · %s\n", summary.Name, summary.Source, description)
		stdout.write("  profiles: %s\n", strings.Join(partySummaryMembers(summary.Members), ", "))
	}
}

func partySummaryMembers(members []model.PartyMember) []string {
	formatted := make([]string, 0, len(members))
	for _, member := range members {
		formatted = append(formatted, member.Scope+":"+member.Profile)
	}
	return formatted
}

func printBundle(output io.Writer, bundle model.ReviewBundle, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(bundle)
	}
	if format != "human" {
		return errors.New("unknown output format " + format)
	}
	return writeCommandOutput(output, func(output *commandOutput) {
		printHumanBundle(output, bundle)
	})
}

func printHumanBundle(output *commandOutput, bundle model.ReviewBundle) {
	output.write("bundle %s\n", bundle.ID)
	output.write("%s · %d/%d review(s) completed\n", bundle.Lifecycle, completedBundleMembers(bundle), len(bundle.Members))
	if partyName := explicitPartyName(bundle); partyName != "" {
		output.write("party: %s\n", partyName)
	}
	output.write("revision: %s\n", bundle.Revision)
	printBundleSelection(output, bundle.Selection)
	printBundleWarnings(output, bundle.Warnings)
	printBundleDeduplication(output, bundle.Deduplicated)
	output.write("subject: %s %s\n", bundle.SubjectKind, shortIdentity(bundle.SubjectIdentity))
	for _, member := range bundle.Members {
		line := fmt.Sprintf("  %-16s %s", scopedMemberName(member), memberStatus(member))
		if member.Status != "" && member.ReviewID != "" {
			line += fmt.Sprintf(" (%d finding(s))", member.FindingCount)
		}
		output.write("%s\n", line)
	}
	if bundle.Termination != nil {
		output.write("incomplete: %s: %s\n", bundle.Termination.Category, bundle.Termination.Message)
	}
	output.write("inspect: review-party inspect %s\n", bundle.ID)
}

// explicitPartyName names the Party behind an explicit-party selection.
func explicitPartyName(bundle model.ReviewBundle) string {
	selection := bundle.Selection
	if selection == nil || selection.Kind != "explicit_party" {
		return ""
	}
	if len(selection.Authored) == 0 {
		return ""
	}
	return selection.Authored[0].Name
}

// printBundleSelection reports which authored choice produced this run.
func printBundleSelection(output *commandOutput, selection *model.BundleSelection) {
	if selection == nil || selection.Source == "" {
		return
	}
	output.write("selection: %s", selection.Kind)
	if selection.LimitSource != "" {
		output.write(" · limit %d (%s)", selection.ConcurrencyLimit, selection.LimitSource)
	}
	output.write("\n")
}

func printBundleWarnings(output *commandOutput, warnings []model.BundleWarning) {
	for _, warning := range warnings {
		output.write("warning: %s\n", warning.Message)
	}
}

// printBundleDeduplication explains every occurrence removed by exact-identity
// deduplication and where its first execution remains.
func printBundleDeduplication(output *commandOutput, duplicates []model.SkippedDuplicate) {
	for _, duplicate := range duplicates {
		output.write("deduplicated: %s:%s selected again by %s; first run kept at %s\n", duplicate.Scope, duplicate.Profile, duplicate.Origin, duplicate.KeptOrigin)
	}
}

func scopedMemberName(member model.BundleMember) string {
	return member.Scope + ":" + member.Profile
}

func completedBundleMembers(bundle model.ReviewBundle) int {
	completed := 0
	for _, member := range bundle.Members {
		if member.Lifecycle == model.LifecycleCompleted {
			completed++
		}
	}
	return completed
}

func memberStatus(member model.BundleMember) string {
	if member.ReviewID == "" {
		return "not started"
	}
	status := string(member.Lifecycle)
	if member.Status != "" && member.Lifecycle == model.LifecycleCompleted {
		status += " " + member.Status
	}
	return status + " " + string(member.ReviewID)
}

func shortIdentity(identity string) string {
	if len(identity) <= 16 {
		return identity
	}
	return identity[:16] + "…"
}
