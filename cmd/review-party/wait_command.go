package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
)

func newWaitCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wait REVIEW_OR_BUNDLE_ID",
		Short: "Wait for a Review Bundle or Review to finish and print its result",
		Long: `Wait until a Review Bundle (rb_…) or Review (rp_…) is completed or
incomplete, checking the ledger once a second, then print the result exactly
as run would have and exit with run's exit code.

Use it to reattach to a run started elsewhere. --timeout bounds the wait; when
it passes first, wait exits 1 and the run keeps going.`,
		Example: "  review-party wait rb_...\n  review-party wait rp_... --format json\n  review-party wait rb_... --timeout 10m",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conductor, err := engine.New(engine.Config{UserConfigurationPath: stringFlag(cmd, "config")})
			if err != nil {
				return commandResult(printFailure(streams.errors, err))
			}
			timeout, err := cmd.Flags().GetDuration("timeout")
			if err != nil {
				return err
			}
			options := waitOptions{
				id: args[0], format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"),
				full: boolFlag(cmd, "full"), timeout: timeout, interval: time.Second,
			}
			return commandResult(executeWait(cmd.Context(), conductor, options, streams))
		},
	}
	cmd.Flags().Duration("timeout", 0, "Stop waiting after this long, for example 10m; 0 waits until the run finishes")
	addFormatFlag(cmd)
	addFullFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

type waitOptions struct {
	id            string
	format        string
	configuration string
	full          bool
	timeout       time.Duration
	interval      time.Duration
}

type waitConductor interface {
	Status(context.Context, string) (model.ReviewStatus, error)
	Inspect(context.Context, model.ReviewID) (model.ReviewRecord, error)
	InspectBundle(context.Context, model.ReviewBundleID) (model.ReviewBundle, error)
}

func executeWait(ctx context.Context, conductor waitConductor, options waitOptions, streams commandIO) int {
	var deadline <-chan time.Time
	if options.timeout > 0 {
		timer := time.NewTimer(options.timeout)
		defer timer.Stop()
		deadline = timer.C
	}
	for {
		status, err := conductor.Status(ctx, options.id)
		if err != nil {
			return printStatusFailure(streams.errors, err)
		}
		if status.Lifecycle.Terminal() {
			report, err := finishedReport(ctx, conductor, status, options.full)
			return printRunOutcome(streams, report, err, reportOptions{format: options.format, configuration: options.configuration})
		}
		select {
		case <-ctx.Done():
			return printFailure(streams.errors, ctx.Err())
		case <-deadline:
			return printFailure(streams.errors, fmt.Errorf("timed out after %s waiting for %s (%s)", options.timeout, options.id, status.Lifecycle))
		case <-time.After(options.interval):
		}
	}
}

func finishedReport(ctx context.Context, conductor waitConductor, status model.ReviewStatus, full bool) (reviewReport, error) {
	if status.Kind == model.ReviewStatusReview {
		record, err := conductor.Inspect(ctx, model.ReviewID(status.ID))
		if err != nil {
			return reviewReport{}, err
		}
		return recordReport(record, full), nil
	}
	bundle, err := conductor.InspectBundle(ctx, model.ReviewBundleID(status.ID))
	if err != nil {
		return reviewReport{}, err
	}
	if bundle.Termination != nil {
		return reviewReport{}, errors.New(bundle.Termination.Message)
	}
	return bundleReport(ctx, conductor, bundle, full), nil
}
