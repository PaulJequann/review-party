package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

func newStatusCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [REVIEW_OR_BUNDLE_ID]",
		Short: "Show the current lifecycle of a Review Bundle or Review without waiting",
		Long: `Show the current lifecycle of a Review Bundle (rb_…) or Review (rp_…) and
each Review it contains, read from the ledger without waiting for it to finish.

Without an ID, lists the Review Bundles and Reviews still pending or running
for the repository. Exits 0 whenever the lookup succeeds, whatever the
lifecycle, and 1 after printing when a member's Review Record cannot be read.
Use wait to block until a run finishes.`,
		Example: "  review-party status rb_...\n  review-party status rp_... --format json\n  review-party status --repo .",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := statusOptions{
				repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"),
				configuration: stringFlag(cmd, "config"), now: time.Now,
			}
			if len(args) == 1 {
				options.id = args[0]
			}
			return commandResult(executeStatus(cmd.Context(), options, streams))
		},
	}
	addRepositoryFlag(cmd, "Git repository whose in-flight Reviews to list when no ID is given")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

type statusOptions struct {
	id            string
	repository    string
	format        string
	configuration string
	now           func() time.Time
}

type inFlightReport struct {
	Repository string               `json:"repository"`
	InFlight   []model.ReviewStatus `json:"in_flight"`
}

func executeStatus(ctx context.Context, options statusOptions, streams commandIO) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(streams.errors, err)
	}
	if options.id != "" {
		return executeRunStatus(ctx, conductor, options, streams)
	}
	return executeInFlightStatus(ctx, conductor, options, streams)
}

func executeRunStatus(ctx context.Context, conductor *engine.Conductor, options statusOptions, streams commandIO) int {
	status, err := conductor.Status(ctx, options.id)
	if err != nil {
		return printStatusFailure(streams.errors, err)
	}
	return statusOutcome(streams.errors, renderLedgerOutput(options.format, streams.output, streams.errors, status, func(output *commandOutput) {
		printHumanStatus(output, status, options)
	}), status)
}

func executeInFlightStatus(ctx context.Context, conductor *engine.Conductor, options statusOptions, streams commandIO) int {
	root, err := subject.ResolveRepositoryRoot(options.repository)
	if err != nil {
		return printFailure(streams.errors, err)
	}
	statuses, err := conductor.InFlight(ctx, root)
	if err != nil {
		return printFailure(streams.errors, err)
	}
	return statusOutcome(streams.errors, renderLedgerOutput(options.format, streams.output, streams.errors, inFlightReport{Repository: root, InFlight: statuses}, func(output *commandOutput) {
		if len(statuses) == 0 {
			output.write("no reviews in flight for %s\n", root)
		}
		for index, status := range statuses {
			if index > 0 {
				output.write("\n")
			}
			printHumanStatus(output, status, options)
		}
	}), statuses...)
}

func statusOutcome(stderr io.Writer, exit int, statuses ...model.ReviewStatus) int {
	if exit != 0 {
		return exit
	}
	var failures []error
	for _, status := range statuses {
		for _, member := range status.Reviews {
			if member.ReadError != "" {
				failures = append(failures, fmt.Errorf("read bundle member %s review %s: %s", statusMemberLabel(member), member.ReviewID, member.ReadError))
			}
		}
	}
	if len(failures) > 0 {
		return printFailure(stderr, errors.Join(failures...))
	}
	return 0
}

func printStatusFailure(stderr io.Writer, err error) int {
	if errors.Is(err, engine.ErrUnsupportedStatusID) {
		return printCommandError(stderr, usageExitCode, err)
	}
	return printFailure(stderr, err)
}

func printHumanStatus(output *commandOutput, status model.ReviewStatus, options statusOptions) {
	header := fmt.Sprintf("%s %s · %s", status.Kind, status.ID, status.Lifecycle)
	if status.Kind == model.ReviewStatusBundle {
		header += fmt.Sprintf(" · %d/%d review(s) finished", finishedStatusMembers(status), len(status.Reviews))
	}
	output.write("%s\n", header)
	for _, member := range status.Reviews {
		output.write("  %-20s %s\n", statusMemberLabel(member), statusMemberDetail(member, options.now()))
	}
	if status.Termination != nil {
		output.write("stopped: %s: %s\n", status.Termination.Category, boundedProgressMessage(status.Termination.Message))
	}
	if status.Lifecycle.Terminal() {
		output.write("inspect: review-party inspect %s%s\n", status.ID, configurationArgument(options.configuration))
		return
	}
	output.write("wait: review-party wait %s%s\n", status.ID, configurationArgument(options.configuration))
}

func finishedStatusMembers(status model.ReviewStatus) int {
	finished := 0
	for _, member := range status.Reviews {
		if member.Lifecycle.Terminal() {
			finished++
		}
	}
	return finished
}

func statusMemberLabel(member model.ReviewStatusMember) string {
	if member.Scope == "" {
		return member.Profile
	}
	return member.Scope + ":" + member.Profile
}

func statusMemberDetail(member model.ReviewStatusMember, now time.Time) string {
	detail := fmt.Sprintf("%s %s", member.Lifecycle, member.ReviewID)
	switch {
	case member.ReadError != "":
		return detail + " · " + boundedProgressMessage(member.ReadError)
	case member.Lifecycle == model.LifecycleCompleted:
		detail += " · " + completedResultLabel(member.Lifecycle, member.Status, member.FindingCount)
	case member.Termination != nil:
		detail += " · " + string(member.Termination.Category)
	case member.Attempts > 0:
		detail += fmt.Sprintf(" · attempt %d", member.Attempts)
	}
	if member.Lifecycle.Terminal() {
		return detail + " · " + formatProgressElapsed(member.DurationMS)
	}
	return detail + " · updated " + formatProgressElapsed(now.Sub(member.UpdatedAt).Milliseconds()) + " ago"
}
