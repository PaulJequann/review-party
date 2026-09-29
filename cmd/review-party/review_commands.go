package main

import (
	"context"
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
	full          bool
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
	report := recordReport(record, options.full)
	if err := printReport(stdout, report, reportOptions{format: options.format, configuration: options.configuration}); err != nil {
		return printFailure(stderr, err)
	}
	if report.incomplete() {
		return usageExitCode
	}
	return 0
}

type inspectOptions struct {
	id              model.ReviewID
	format          string
	verifyArtifacts bool
	full            bool
	configuration   string
}

func executeInspect(ctx context.Context, options inspectOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	var report reviewReport
	if strings.HasPrefix(string(options.id), "rb_") {
		report, err = inspectBundleReport(ctx, conductor, options)
	} else {
		report, err = inspectRecordReport(ctx, conductor, options)
	}
	if err == nil {
		err = report.attachMisses(ctx, conductor)
	}
	if err != nil {
		return printFailure(stderr, err)
	}
	if err := printReport(stdout, report, reportOptions{format: options.format, configuration: options.configuration}); err != nil {
		return printFailure(stderr, err)
	}
	if err := report.readFailure(); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

func inspectRecordReport(ctx context.Context, conductor *engine.Conductor, options inspectOptions) (reviewReport, error) {
	record, err := conductor.Inspect(ctx, options.id)
	if err != nil {
		return reviewReport{}, err
	}
	if options.verifyArtifacts {
		if err := conductor.VerifyArtifacts(record); err != nil {
			return reviewReport{}, fmt.Errorf("verify artifacts: %w", err)
		}
	}
	return recordReport(record, options.full), nil
}

func inspectBundleReport(ctx context.Context, conductor *engine.Conductor, options inspectOptions) (reviewReport, error) {
	bundle, err := conductor.InspectBundle(ctx, model.ReviewBundleID(options.id))
	if err != nil {
		return reviewReport{}, err
	}
	return bundleReport(ctx, conductor, bundle, options.full), nil
}
