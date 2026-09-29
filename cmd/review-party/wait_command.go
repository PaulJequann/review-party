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
incomplete, checking the ledger once a second, then print the result as run
would have, plus any misses and verdicts recorded since, and exit with run's
exit code.

Use it to reattach to a run started elsewhere. --timeout bounds how long wait
blocks: once it passes with the run still pending or running, wait exits 1 and
the run keeps going. A run wait finds finished is always reported, even if the
timeout passed during that check. When status would mark
the run stale, wait exits 1 at once rather than waiting on a run that may have
died.`,
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
	annotationLoader
}

func executeWait(ctx context.Context, conductor waitConductor, options waitOptions, streams commandIO) int {
	status, err := awaitFinished(ctx, conductor, options)
	if err != nil {
		return printStatusFailure(streams.errors, err)
	}
	report, err := finishedReport(ctx, conductor, status, options.full)
	if err != nil {
		return printFailure(streams.errors, err)
	}
	// The run's result stands without its misses and verdicts, so a failure to
	// read them is a warning and wait still prints the result and exits as run did.
	if err := report.annotate(ctx, conductor); err != nil {
		printCommandError(streams.errors, 0, fmt.Errorf("warning: %w; showing the result without misses or verdicts", err))
	}
	return printRunOutcome(streams, report, nil, reportOptions{format: options.format, configuration: options.configuration})
}

// awaitFinished checks the run's status once an interval until it is finished,
// failing when the status cannot be read, the run looks stale, the context ends,
// or the timeout passes first.
func awaitFinished(ctx context.Context, conductor waitConductor, options waitOptions) (model.ReviewStatus, error) {
	deadline, stop := waitTimeout(options.timeout)
	defer stop()
	for {
		status, err := conductor.Status(ctx, options.id)
		if err != nil {
			return model.ReviewStatus{}, err
		}
		if status.Lifecycle.Terminal() {
			return status, nil
		}
		if err := staleRunError(status); err != nil {
			return model.ReviewStatus{}, err
		}
		select {
		case <-ctx.Done():
			return model.ReviewStatus{}, ctx.Err()
		case <-deadline:
			return model.ReviewStatus{}, fmt.Errorf("timed out after %s waiting for %s (%s)", options.timeout, options.id, status.Lifecycle)
		case <-time.After(options.interval):
		}
	}
}

// waitTimeout fires once timeout passes; a zero timeout never fires.
func waitTimeout(timeout time.Duration) (<-chan time.Time, func() bool) {
	if timeout <= 0 {
		return nil, func() bool { return false }
	}
	timer := time.NewTimer(timeout)
	return timer.C, timer.Stop
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
