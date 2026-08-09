package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"reviewparty"
)

func runProfiles(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("profiles", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "human", "Output format: human or json")
	repository := flags.String("repo", ".", "Git repository whose Profiles should be listed")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: profiles accepts no arguments")
		return 2
	}

	conductor, err := reviewparty.New(reviewparty.Config{UserConfigurationPath: *configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	profiles, err := conductor.ProfilesForRepository(ctx, *repository)
	if err != nil {
		return printFailure(stderr, err)
	}
	if err := printProfiles(stdout, profiles, *format); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

func runExplain(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	options, exitCode := parseExplainOptions(arguments, stderr)
	if exitCode != 0 {
		return exitCode
	}

	conductor, err := reviewparty.New(reviewparty.Config{AttemptDeadline: options.deadline, UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	explanation, err := conductor.ExplainForRepository(ctx, reviewparty.ProfileSelection{Profile: options.profile, Reviewer: options.reviewer, Model: options.model, Effort: options.effort}, options.repository)
	if err != nil {
		return printFailure(stderr, err)
	}
	if err := printProfileExplanation(stdout, explanation, options.format); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

type explainOptions struct {
	profile       string
	format        string
	deadline      time.Duration
	configuration string
	reviewer      string
	model         string
	effort        string
	repository    string
}

func parseExplainOptions(arguments []string, stderr io.Writer) (explainOptions, int) {
	profile, remaining, ok := requiredLeadingArgument(arguments)
	if !ok {
		fmt.Fprintln(stderr, "review-party: explain requires a profile name")
		return explainOptions{}, 2
	}
	flags := flag.NewFlagSet("explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "human", "Output format: human or json")
	deadline := flags.Duration("deadline", 10*time.Minute, "Attempt deadline")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	reviewer := flags.String("reviewer", "", "Reviewer: "+strings.Join(reviewparty.SupportedReviewers(), ", ")+"; empty uses configured/Profile default")
	model := flags.String("model", "", "Explicit model for the selected Reviewer")
	effort := flags.String("effort", "", "Explicit reasoning effort for the selected Reviewer")
	repository := flags.String("repo", ".", "Git repository whose Profile should be explained")
	if err := flags.Parse(remaining); err != nil {
		return explainOptions{}, 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: explain accepts one profile name")
		return explainOptions{}, 2
	}
	return explainOptions{profile: profile, format: *format, deadline: *deadline, configuration: *configuration, reviewer: *reviewer, model: *model, effort: *effort, repository: *repository}, 0
}

func requiredLeadingArgument(arguments []string) (string, []string, bool) {
	if len(arguments) == 0 {
		return "", nil, false
	}
	value := arguments[0]
	if value == "" {
		return "", nil, false
	}
	if value[0] == '-' {
		return "", nil, false
	}
	return value, arguments[1:], true
}

func printProfiles(output io.Writer, profiles []reviewparty.ProfileSummary, format string) error {
	if format == "json" {
		return writeJSON(output, profiles)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	for _, profile := range profiles {
		if profile.Error != "" {
			fmt.Fprintf(output, "%s\tinvalid: %s\n", profile.Name, profile.Error)
			continue
		}
		fmt.Fprintf(output, "%s\t%s\t%s\tdefault %s/%s via %s/%s\n", profile.Name, profile.Source, profile.Description, profile.DefaultReviewer.ReviewerID, profile.DefaultReviewer.Model, profile.DefaultReviewer.Harness, profile.DefaultReviewer.Transport)
	}
	return nil
}

func printProfileExplanation(output io.Writer, explanation reviewparty.ProfileExplanation, format string) error {
	if format == "json" {
		return writeJSON(output, explanation)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	revision := explanation.ProfileRevision
	selection := "explicit"
	if explanation.ReviewerWasDefault {
		selection = "default"
	}
	fmt.Fprintf(output, "profile: %s\n", revision.Name)
	fmt.Fprintf(output, "revision: %s\n", revision.Revision)
	fmt.Fprintf(output, "source: %s\n", revision.Source)
	fmt.Fprintf(output, "purpose: %s\n", revision.Purpose)
	fmt.Fprintf(output, "materiality: %s\n", revision.MaterialityThreshold)
	fmt.Fprintf(output, "reviewer: %s (%s)\n", revision.Reviewer.ReviewerID, selection)
	fmt.Fprintf(output, "model/effort: %s/%s\n", revision.Reviewer.Model, revision.Reviewer.Effort)
	fmt.Fprintf(output, "harness/transport: %s/%s\n", revision.Reviewer.Harness, revision.Reviewer.Transport)
	for _, pass := range revision.Passes {
		fmt.Fprintf(output, "pass: %s (required=%t, prompt=%s)\n", pass.Name, pass.Required, pass.PromptRevision)
	}
	fmt.Fprintf(output, "requires: %s\n", joinCapabilities(revision.RequiredCapabilities))
	fmt.Fprintf(output, "budget: %d attempt, %s deadline\n", revision.AttemptLimit, revision.ExecutionDeadline)
	fmt.Fprintf(output, "result contract: %s\n", revision.ResultContract)
	fmt.Fprintln(output, "availability: not checked")
	fmt.Fprintln(output, "no Agent Harness launched; no Review Record created")
	fmt.Fprintf(output, "\n--- PROFILE MARKDOWN ---\n%s\n", explanation.Instructions)
	return nil
}

func joinCapabilities(capabilities []reviewparty.Capability) string {
	values := make([]string, len(capabilities))
	for index, capability := range capabilities {
		values[index] = string(capability)
	}
	return strings.Join(values, ", ")
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printFailure(output io.Writer, err error) int {
	fmt.Fprintf(output, "review-party: %v\n", err)
	return 1
}
