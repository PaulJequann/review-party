package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

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
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Replay(ctx, model.ReplaySelection{SourceReviewID: options.id, Reviewer: options.reviewer, Model: options.model, Effort: options.effort})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printRecordWithConfiguration(stdout, record, options.format, options.configuration); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if record.Lifecycle == model.LifecycleIncomplete {
		return 2
	}
	return 0
}

type reviewOptions struct {
	profile       string
	repository    string
	format        string
	deadline      time.Duration
	configuration string
	reviewer      string
	model         string
	effort        string
	base          string
	head          string
}

func executeReview(ctx context.Context, options reviewOptions, stdout, stderr io.Writer) int {
	subjectReference, err := reviewSubjectReference(options.base, options.head)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return usageExitCode
	}
	conductor, err := engine.New(engine.Config{
		AttemptDeadline:       options.deadline,
		UserConfigurationPath: options.configuration,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Review(ctx, model.ReviewSelection{
		Repository: options.repository,
		Subject:    subjectReference,
		Profile:    options.profile,
		Reviewer:   options.reviewer,
		Model:      options.model,
		Effort:     options.effort,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printRecordWithConfiguration(stdout, record, options.format, options.configuration); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if record.Lifecycle == model.LifecycleIncomplete {
		return usageExitCode
	}
	return 0
}

func reviewSubjectReference(base, head string) (model.SubjectReference, error) {
	if (base == "") != (head == "") {
		return model.SubjectReference{}, errors.New("--base and --head must be provided together")
	}
	if base != "" {
		return model.CommittedRange(base, head), nil
	}
	return model.WorkingChanges(), nil
}

func executeInspect(ctx context.Context, options inspectOptions, stdout, stderr io.Writer) int {
	if strings.HasPrefix(string(options.id), "rb_") {
		return runInspectBundle(ctx, options, stdout, stderr)
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Inspect(ctx, options.id)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if options.verifyArtifacts {
		if err := conductor.VerifyArtifacts(record); err != nil {
			fmt.Fprintf(stderr, "review-party: verify artifacts: %v\n", err)
			return 1
		}
	}
	if err := printRecordWithConfiguration(stdout, record, options.format, options.configuration); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
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
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	bundle, err := conductor.InspectBundle(ctx, model.ReviewBundleID(options.id))
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printBundle(stdout, bundle, options.format); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
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
	printHumanRecord(output, record, configuration)
	return nil
}

func printHumanRecord(output io.Writer, record model.ReviewRecord, configuration string) {
	findings := 0
	if record.Result != nil {
		findings = record.Result.FindingCount()
	}
	fmt.Fprintf(output, "review %s\n", record.ID)
	printReplayLineage(output, record.ReplaysReviewID)
	provenance := latestProvenance(record)
	fmt.Fprintf(output, "%s · %d finding(s) · %s/%s (%s)\n", record.Lifecycle, findings, provenance.ReviewerID, provenance.Model, provenance.Effort)
	if record.ProfileRevision.Source != "" {
		fmt.Fprintf(output, "profile: %s · %s\n", record.ProfileRevision.Name, record.ProfileRevision.Source)
	}
	if record.Result != nil {
		fmt.Fprintln(output, record.Result.Raw)
	}
	printArtifactReferences(output, record)
	if record.Termination != nil {
		fmt.Fprintf(output, "incomplete: %s at %s: %s\n", record.Termination.Category, record.Termination.Phase, record.Termination.Message)
	}
	fmt.Fprintf(output, "inspect: review-party inspect %s", record.ID)
	if configuration != defaultUserConfigurationPath() {
		fmt.Fprintf(output, " --config %s", shellQuoteArgument(configuration))
	}
	fmt.Fprintln(output)
}

func printReplayLineage(output io.Writer, source *model.ReviewID) {
	if source != nil {
		fmt.Fprintf(output, "replays: %s\n", *source)
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
