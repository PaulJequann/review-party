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

type replayOptions struct {
	id            model.ReviewID
	format        string
	configuration string
	reviewer      string
	model         string
	effort        string
}

func executeReplay(ctx context.Context, options replayOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	record, err := conductor.Replay(ctx, model.ReplaySelection{SourceReviewID: options.id, Reviewer: options.reviewer, Model: options.model, Effort: options.effort})
	if err != nil {
		return printFailure(stderr, err)
	}
	if err := printRecordWithConfiguration(stdout, record, options.format, options.configuration); err != nil {
		return printFailure(stderr, err)
	}
	if record.Lifecycle == model.LifecycleIncomplete {
		return 2
	}
	return 0
}

func executeInspect(ctx context.Context, options inspectOptions, stdout, stderr io.Writer) int {
	if strings.HasPrefix(string(options.id), "rb_") {
		return runInspectBundle(ctx, options, stdout, stderr)
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	record, err := conductor.Inspect(ctx, options.id)
	if err != nil {
		return printFailure(stderr, err)
	}
	if options.verifyArtifacts {
		if err := conductor.VerifyArtifacts(record); err != nil {
			return printFailure(stderr, fmt.Errorf("verify artifacts: %w", err))
		}
	}
	if err := printRecordWithConfiguration(stdout, record, options.format, options.configuration); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

type inspectOptions struct {
	id              model.ReviewID
	format          string
	verifyArtifacts bool
	configuration   string
}

func runInspectBundle(ctx context.Context, options inspectOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	bundle, err := conductor.InspectBundle(ctx, model.ReviewBundleID(options.id))
	if err != nil {
		return printFailure(stderr, err)
	}
	if err := printBundle(stdout, bundle, options.format); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

func printRecord(output io.Writer, record model.ReviewRecord, format string) error {
	return printRecordWithConfiguration(output, record, format, defaultUserConfigurationPath())
}

func printRecordWithConfiguration(output io.Writer, record model.ReviewRecord, format, configuration string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(record)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	return printHumanRecord(output, record, configuration)
}

func printHumanRecord(output io.Writer, record model.ReviewRecord, configuration string) error {
	return writeCommandOutput(output, func(output *commandOutput) {
		findings := 0
		if record.Result != nil {
			findings = record.Result.FindingCount()
		}
		output.write("review %s\n", record.ID)
		printReplayLineage(output, record.ReplaysReviewID)
		provenance := latestProvenance(record)
		output.write("%s · %d finding(s) · %s/%s (%s)\n", record.Lifecycle, findings, provenance.ReviewerID, provenance.Model, provenance.Effort)
		if record.ProfileRevision.Source != "" {
			output.write("profile: %s · %s\n", record.ProfileRevision.Name, record.ProfileRevision.Source)
		}
		if record.Result != nil {
			output.write("%s\n", record.Result.Raw)
		}
		printArtifactReferences(output, record)
		if record.Termination != nil {
			output.write("incomplete: %s at %s: %s\n", record.Termination.Category, record.Termination.Phase, record.Termination.Message)
		}
		output.write("inspect: review-party inspect %s", record.ID)
		if configuration != defaultUserConfigurationPath() {
			output.write(" --config %s", shellQuoteArgument(configuration))
		}
		output.write("\n")
	})
}

func printReplayLineage(output *commandOutput, source *model.ReviewID) {
	if source != nil {
		output.write("replays: %s\n", *source)
	}
}

func shellQuoteArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func latestProvenance(record model.ReviewRecord) model.ReviewerProvenance {
	provenance := model.ReviewerProvenance{
		ReviewerID: record.ProfileRevision.ReviewerID,
		Model:      record.ProfileRevision.Model,
		Effort:     record.ProfileRevision.Effort,
	}
	for _, pass := range record.Passes {
		if len(pass.Attempts) > 0 {
			provenance = pass.Attempts[len(pass.Attempts)-1].Provenance
		}
	}
	return provenance
}
