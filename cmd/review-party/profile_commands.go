package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"reviewparty"
)

func runProfile(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] != "explain" {
		fmt.Fprintln(stderr, "review-party: profile requires: explain PROFILE")
		return 2
	}
	return runProfileExplain(ctx, arguments[1:], stdout, stderr)
}

func runProfileExplain(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	name, arguments := takeLeadingValue(arguments)
	if name == "" {
		fmt.Fprintln(stderr, "review-party: profile explain requires a Profile name")
		return 2
	}
	flags := flag.NewFlagSet("profile explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository whose Profile should be explained")
	reviewer := flags.String("reviewer", "", "Reviewer adapter: "+strings.Join(reviewparty.SupportedReviewers(), ", "))
	format := flags.String("format", "human", "Output format: human or json")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: profile explain accepts one Profile name")
		return 2
	}
	explanation, err := reviewparty.NewProfileCatalog("").Explain(reviewparty.ProfileExplanationRequest{Repository: *repository, Name: name, Reviewer: *reviewer})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printProfileExplanation(stdout, explanation, *format); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return 0
}

func printProfileExplanation(output io.Writer, explanation reviewparty.ProfileExplanation, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(explanation)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	revision := explanation.Revision
	fmt.Fprintf(output, "profile  %s\n", revision.Name)
	fmt.Fprintf(output, "source   %s\n", revision.Source)
	fmt.Fprintf(output, "reviewer %s/%s (%s)\n", revision.ReviewerID, revision.Model, revision.Effort)
	fmt.Fprintf(output, "revision %s\n", revision.Revision)
	for _, pass := range explanation.Passes {
		fmt.Fprintf(output, "pass     %s (required: %t)\n", pass.Name, pass.Required)
	}
	fmt.Fprintf(output, "\n--- PROFILE MARKDOWN ---\n%s\n", explanation.Instructions)
	return nil
}

func runInit(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository to initialize")
	global := flags.Bool("global", false, "Initialize the user-wide profile library")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: init accepts no positional arguments")
		return 2
	}
	result, err := reviewparty.InitializeProfiles(reviewparty.ProfileInitialization{
		Repository: *repository,
		Global:     *global,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	printInitialization(stdout, result)
	return 0
}

func printInitialization(output io.Writer, result reviewparty.ProfileInitializationResult) {
	fmt.Fprintf(output, "profile library %s\n", result.Directory)
	for _, path := range result.Created {
		fmt.Fprintf(output, "created  %s\n", path)
	}
	for _, path := range result.Existing {
		fmt.Fprintf(output, "kept     %s\n", path)
	}
}

func runProfiles(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("profiles", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository whose Profiles should be listed")
	format := flags.String("format", "human", "Output format: human or json")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: profiles accepts no positional arguments")
		return 2
	}
	profiles, err := loadProfiles(ctx, *repository)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printProfiles(stdout, profiles, *format); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return 0
}

func loadProfiles(ctx context.Context, repository string) ([]reviewparty.ProfileSummary, error) {
	return reviewparty.NewProfileCatalog("").Profiles(repository)
}

func printProfiles(output io.Writer, profiles []reviewparty.ProfileSummary, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(profiles)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	for _, profile := range profiles {
		if profile.Error != "" {
			fmt.Fprintf(output, "%-20s invalid: %s\n", profile.Name, profile.Error)
			continue
		}
		fmt.Fprintf(output, "%-20s %s\n", profile.Name, profile.Source)
	}
	return nil
}
