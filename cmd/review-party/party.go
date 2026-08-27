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
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	summaries, err := conductor.PartiesForRepository(options.repository)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if options.format != "json" && options.format != "human" {
		fmt.Fprintf(stderr, "review-party: unknown output format %q\n", options.format)
		return usageExitCode
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
	printHumanPartySummaries(stdout, summaries)
	return 0
}

func printHumanPartySummaries(stdout io.Writer, summaries []model.PartySummary) {
	for _, summary := range summaries {
		description := summary.Description
		if summary.Error != "" {
			description = summary.Error
		}
		fmt.Fprintf(stdout, "%s · %s · %s\n", summary.Name, summary.Source, description)
		fmt.Fprintf(stdout, "  profiles: %s\n", strings.Join(partySummaryMembers(summary.Members), ", "))
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
	printHumanBundle(output, bundle)
	return nil
}

func printHumanBundle(output io.Writer, bundle model.ReviewBundle) {
	fmt.Fprintf(output, "bundle %s\n", bundle.ID)
	fmt.Fprintf(output, "%s · %d/%d review(s) completed\n", bundle.Lifecycle, completedBundleMembers(bundle), len(bundle.Members))
	if partyName := explicitPartyName(bundle); partyName != "" {
		fmt.Fprintf(output, "party: %s\n", partyName)
	}
	fmt.Fprintf(output, "revision: %s\n", bundle.Revision)
	printBundleSelection(output, bundle.Selection)
	printBundleWarnings(output, bundle.Warnings)
	printBundleDeduplication(output, bundle.Deduplicated)
	fmt.Fprintf(output, "subject: %s %s\n", bundle.SubjectKind, shortIdentity(bundle.SubjectIdentity))
	for _, member := range bundle.Members {
		line := fmt.Sprintf("  %-16s %s", scopedMemberName(member), memberStatus(member))
		if member.Status != "" && member.ReviewID != "" {
			line += fmt.Sprintf(" (%d finding(s))", member.FindingCount)
		}
		fmt.Fprintln(output, line)
	}
	if bundle.Termination != nil {
		fmt.Fprintf(output, "incomplete: %s: %s\n", bundle.Termination.Category, bundle.Termination.Message)
	}
	fmt.Fprintf(output, "inspect: review-party inspect %s\n", bundle.ID)
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
func printBundleSelection(output io.Writer, selection *model.BundleSelection) {
	if selection == nil || selection.Source == "" {
		return
	}
	fmt.Fprintf(output, "selection: %s", selection.Kind)
	if selection.LimitSource != "" {
		fmt.Fprintf(output, " · limit %d (%s)", selection.ConcurrencyLimit, selection.LimitSource)
	}
	fmt.Fprint(output, "\n")
}

func printBundleWarnings(output io.Writer, warnings []model.BundleWarning) {
	for _, warning := range warnings {
		fmt.Fprintf(output, "warning: %s\n", warning.Message)
	}
}

// printBundleDeduplication explains every occurrence removed by exact-identity
// deduplication and where its first execution remains.
func printBundleDeduplication(output io.Writer, duplicates []model.SkippedDuplicate) {
	for _, duplicate := range duplicates {
		fmt.Fprintf(output, "deduplicated: %s:%s selected again by %s; first run kept at %s\n", duplicate.Scope, duplicate.Profile, duplicate.Origin, duplicate.KeptOrigin)
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
