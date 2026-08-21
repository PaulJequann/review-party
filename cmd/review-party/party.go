package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
)

func runParty(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	subcommand, remaining := takeLeadingValue(arguments)
	switch subcommand {
	case "run":
		return runPartyRun(ctx, remaining, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "review-party: party requires run")
		return 2
	}
}

func runPartyRun(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	options, ok := parsePartyRunOptions(arguments, stderr)
	if !ok {
		return 2
	}
	conductor, err := engine.New(engine.Config{
		AttemptDeadline:       options.deadline,
		UserConfigurationPath: options.configuration,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	bundle, err := conductor.RunParty(ctx, model.PartySelection{
		Name:             options.name,
		Repository:       options.repository,
		Subject:          options.subject,
		Reviewer:         options.reviewer,
		Model:            options.model,
		Effort:           options.effort,
		ConcurrencyLimit: options.concurrency,
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
	reviewer      string
	model         string
	effort        string
	concurrency   int
	deadline      time.Duration
	format        string
	configuration string
}

func parsePartyRunOptions(arguments []string, stderr io.Writer) (partyRunOptions, bool) {
	name, remaining := takeLeadingValue(arguments)
	if name == "" {
		fmt.Fprintln(stderr, "review-party: party run requires a party name")
		return partyRunOptions{}, false
	}
	flags := flag.NewFlagSet("party run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository to review")
	format := flags.String("format", "human", "Output format: human or json")
	deadline := flags.Duration("deadline", 10*time.Minute, "Attempt deadline")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	reviewer := flags.String("reviewer", "", "Reviewer adapter override applied to every member")
	modelName := flags.String("model", "", "Explicit model override applied to every member")
	effort := flags.String("effort", "", "Explicit reasoning effort override applied to every member")
	concurrency := flags.Int("concurrency", 0, "Maximum active Reviewer executions; default uses the Party definition")
	base := flags.String("base", "", "Committed-range base revision")
	head := flags.String("head", "", "Committed-range head revision")
	if err := flags.Parse(remaining); err != nil {
		return partyRunOptions{}, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: party run accepts one party name")
		return partyRunOptions{}, false
	}
	subjectReference, err := reviewSubjectReference(*base, *head)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return partyRunOptions{}, false
	}
	return partyRunOptions{
		name:          name,
		repository:    *repository,
		subject:       subjectReference,
		reviewer:      *reviewer,
		model:         *modelName,
		effort:        *effort,
		concurrency:   *concurrency,
		deadline:      *deadline,
		format:        *format,
		configuration: *configuration,
	}, true
}

func runParties(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("parties", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository whose Parties should be listed")
	format := flags.String("format", "human", "Output format: human or json")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: parties accepts no positional arguments")
		return 2
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: *configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	summaries, err := conductor.PartiesForRepository(*repository)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if *format != "json" && *format != "human" {
		fmt.Fprintf(stderr, "review-party: unknown output format %q\n", *format)
		return 2
	}
	return printPartySummaries(stdout, summaries, *format)
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
		fmt.Fprintf(stdout, "%s · %s · %s\n  profiles: %s\n", summary.Name, summary.Source, description, strings.Join(summary.Members, ", "))
	}
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
