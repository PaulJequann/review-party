package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
)

type profilesOptions struct {
	format        string
	repository    string
	configuration string
}

func executeProfiles(ctx context.Context, options profilesOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	profiles, err := conductor.ProfilesForRepository(ctx, options.repository)
	if err != nil {
		return printFailure(stderr, err)
	}
	if err := printProfiles(stdout, profiles, options.format); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

func executeExplain(ctx context.Context, options explainOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{AttemptDeadline: options.deadline, UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	explanation, err := conductor.ExplainForRepository(ctx, model.ProfileSelection{Profile: options.profile, Reviewer: options.reviewer, Model: options.model, Effort: options.effort}, options.repository)
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

func printProfiles(output io.Writer, profiles []model.ProfileSummary, format string) error {
	if format == "json" {
		return writeJSON(output, profiles)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	return writeCommandOutput(output, func(output *commandOutput) {
		for _, profile := range profiles {
			if profile.Error != "" {
				output.write("%s\tinvalid: %s\n", profile.Name, profile.Error)
				continue
			}
			output.write("%s\t%s\t%s\tdefault %s/%s via %s/%s\n", profile.Name, profile.Source, profile.Description, profile.DefaultReviewer.ReviewerID, profile.DefaultReviewer.Model, profile.DefaultReviewer.Harness, profile.DefaultReviewer.Transport)
		}
	})
}

func printProfileExplanation(output io.Writer, explanation model.ProfileExplanation, format string) error {
	if format == "json" {
		return writeJSON(output, explanation)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	return writeCommandOutput(output, func(output *commandOutput) {
		revision := explanation.ProfileRevision
		selection := "explicit"
		if explanation.ReviewerWasDefault {
			selection = "default"
		}
		output.write("profile: %s\n", revision.Name)
		output.write("revision: %s\n", revision.Revision)
		output.write("source: %s\n", revision.Source)
		output.write("purpose: %s\n", revision.Purpose)
		output.write("materiality: %s\n", revision.MaterialityThreshold)
		output.write("reviewer: %s (%s)\n", revision.Reviewer.ReviewerID, selection)
		output.write("model/effort: %s/%s\n", revision.Reviewer.Model, revision.Reviewer.Effort)
		output.write("harness/transport: %s/%s\n", revision.Reviewer.Harness, revision.Reviewer.Transport)
		for _, pass := range revision.Passes {
			output.write("pass: %s (required=%t, prompt=%s)\n", pass.Name, pass.Required, pass.PromptRevision)
		}
		output.write("requires: %s\n", joinCapabilities(revision.RequiredCapabilities))
		output.write("budget: %d attempt, %s deadline\n", revision.AttemptLimit, revision.ExecutionDeadline)
		output.write("result contract: %s\n", revision.ResultContract)
		output.write("availability: not checked\n")
		output.write("no Agent Harness launched; no Review Record created\n")
		output.write("\n--- PROFILE MARKDOWN ---\n%s\n", explanation.Instructions)
	})
}

func joinCapabilities(capabilities []model.Capability) string {
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
