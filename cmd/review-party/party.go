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

func executePartyRun(ctx context.Context, options partyRunOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	bundle, err := conductor.RunParty(ctx, model.PartySelection{
		Name:       options.name,
		Repository: options.repository,
		Subject:    options.subject,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printBundle(stdout, bundle, options.format); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if bundle.Lifecycle == model.LifecycleIncomplete {
		return 2
	}
	return 0
}

type partyRunOptions struct {
	name          string
	repository    string
	subject       model.SubjectReference
	format        string
	configuration string
}

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
	fmt.Fprintf(output, "%s · %s · %d/%d review(s) completed\n", bundle.Party, bundle.Lifecycle, completedBundleMembers(bundle), len(bundle.Members))
	fmt.Fprintf(output, "party revision: %s\n", bundle.PartyRevision)
	fmt.Fprintf(output, "subject: %s %s\n", bundle.SubjectKind, shortIdentity(bundle.SubjectIdentity))
	for _, member := range bundle.Members {
		line := fmt.Sprintf("  %-16s %s", member.Profile, memberStatus(member))
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
