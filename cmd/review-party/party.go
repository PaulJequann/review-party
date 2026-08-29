package main

import (
	"context"
	"encoding/json"
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
