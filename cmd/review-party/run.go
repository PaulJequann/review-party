package main

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
)

// reviewSubjectReference turns the committed-range flags into one Subject
// Reference, defaulting to working changes.
func reviewSubjectReference(base, head string) (model.SubjectReference, error) {
	if (base == "") != (head == "") {
		return model.SubjectReference{}, errors.New("--base and --head must be provided together")
	}
	if base != "" {
		return model.CommittedRange(base, head), nil
	}
	return model.WorkingChanges(), nil
}

func newRunCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the repository's saved review selection, or one explicit Profile or Party",
		Long: `Run one bounded review selection over a Review Subject.

Without --profile or --party, runs the repository's complete saved selection
from .reviewparty/config.json reviews. An explicit Profile or Party replaces
the saved selection for this one run and never changes configuration.`,
		Example:           "  review-party run --repo .\n  review-party run --profile code-quality\n  review-party run --party baseline --base main --head HEAD",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			subjectReference, err := reviewSubjectReference(stringFlag(cmd, "base"), stringFlag(cmd, "head"))
			if err != nil {
				return err
			}
			options := runOptions{
				profile: stringFlag(cmd, "profile"), party: stringFlag(cmd, "party"),
				repository: stringFlag(cmd, "repo"), subject: subjectReference,
				format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"), full: boolFlag(cmd, "full"),
			}
			renderer, sink := newRunProgressSink(options.format, streams.errors)
			defer renderer.stop()
			options.progress = sink
			return commandResult(executeRun(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	cmd.Flags().String("profile", "", "Run exactly this Profile instead of the saved selection; prefix global: or repository: for an exact scope")
	cmd.Flags().String("party", "", "Run exactly this Party instead of the saved selection; prefix global: or repository: for an exact scope")
	cmd.RegisterFlagCompletionFunc("profile", completeProfileNames) //nolint:errcheck // Cobra completion registration is best-effort
	cmd.RegisterFlagCompletionFunc("party", completePartyNames)     //nolint:errcheck // Cobra completion registration is best-effort
	cmd.MarkFlagsMutuallyExclusive("profile", "party")
	addReviewFlags(cmd)
	return cmd
}

type runOptions struct {
	profile       string
	party         string
	repository    string
	subject       model.SubjectReference
	format        string
	configuration string
	full          bool
	progress      func(model.RunProgressEvent)
}

func executeRun(ctx context.Context, options runOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration, Progress: options.progress})
	if err != nil {
		return printFailure(stderr, err)
	}
	return executeRunWithConductor(ctx, conductor, options, commandIO{output: stdout, errors: stderr})
}

type runConductor interface {
	ReviewExplicitProfile(context.Context, model.RunSelection) (model.ReviewRecord, error)
	Run(context.Context, model.RunSelection) (model.ReviewBundle, error)
	Inspect(context.Context, model.ReviewID) (model.ReviewRecord, error)
}

func executeRunWithConductor(ctx context.Context, conductor runConductor, options runOptions, streams commandIO) int {
	report, err := runReport(ctx, conductor, options)
	if err != nil {
		return printFailure(streams.errors, err)
	}
	if err := printReport(streams.output, report, reportOptions{format: options.format, configuration: options.configuration}); err != nil {
		return printFailure(streams.errors, err)
	}
	if report.incomplete() {
		return usageExitCode
	}
	return 0
}

func runReport(ctx context.Context, conductor runConductor, options runOptions) (reviewReport, error) {
	selection := model.RunSelection{
		Repository: options.repository,
		Subject:    options.subject,
		Profile:    options.profile,
		Party:      options.party,
	}
	if options.profile != "" {
		record, err := conductor.ReviewExplicitProfile(ctx, selection)
		if err != nil {
			return reviewReport{}, err
		}
		return recordReport(record, options.full), nil
	}
	bundle, err := conductor.Run(ctx, selection)
	if err != nil {
		return reviewReport{}, err
	}
	return bundleReport(ctx, conductor, bundle, options.full)
}
