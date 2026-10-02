// Checkpoint commands tell a Git hook whether the Reviews this repository
// selects already cover the content a push or commit would accept. They
// report coverage; the Caller decides whether an uncovered Checkpoint blocks.
package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

const (
	checkpointPrePush   = "pre-push"
	checkpointPreCommit = "pre-commit"
	rangeSourceFlags    = "flags"
)

func newCheckpointCommand(streams commandIO) *cobra.Command {
	checkpoint := &cobra.Command{
		Use:   "checkpoint",
		Short: "Check Review coverage at a Git checkpoint",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	check := &cobra.Command{
		Use:   "check <pre-push|pre-commit>",
		Short: "Report whether every selected Review covers the content a push or commit accepts",
		Long: `Report, for each Profile the repository selects, whether a completed Review
of that Profile examined exactly the content the checkpoint accepts.

pre-push checks the range from --base to --head. Without --base, the base is
the merge base of HEAD and the branch's upstream, else of HEAD and origin/HEAD. pre-commit
checks the staged content against HEAD.

A Review covers content when its Subject changed the same paths from the same
blobs to the same blobs, so rebased, amended, or recommitted content stays
covered. Exits 0 when covered, 1 when a Review is missing or still running,
and 2 on a usage error or when Review Party is not initialized.`,
		Example:   "  review-party checkpoint check pre-push\n  review-party checkpoint check pre-push --base origin/main --head HEAD\n  review-party checkpoint check pre-commit --format json",
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{checkpointPrePush, checkpointPreCommit},
		RunE: func(cmd *cobra.Command, args []string) error {
			options := checkpointOptions{
				kind: args[0], repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"),
				configuration: stringFlag(cmd, "config"), base: stringFlag(cmd, "base"), head: stringFlag(cmd, "head"),
			}
			if options.kind == checkpointPreCommit && options.namesRange() {
				return errors.New("--base and --head apply only to pre-push")
			}
			return commandResult(executeCheckpointCheck(cmd.Context(), options, streams))
		},
	}
	addRepositoryFlag(check, "Git repository to check")
	addConfigurationFlag(check)
	addFormatFlag(check)
	check.Flags().String("base", "", "pre-push: base revision of the range (default: merge base with the upstream, else with origin/HEAD)")
	check.Flags().String("head", "", "pre-push: head revision of the range (default: HEAD)")
	checkpoint.AddCommand(check)
	return checkpoint
}

type checkpointOptions struct {
	kind          string
	repository    string
	format        string
	configuration string
	base          string
	head          string
}

func (options checkpointOptions) namesRange() bool {
	return options.base != "" || options.head != ""
}

type checkpointReport struct {
	Checkpoint      string              `json:"checkpoint"`
	Base            string              `json:"base,omitempty"`
	Head            string              `json:"head,omitempty"`
	RangeSource     string              `json:"range_source,omitempty"`
	UnstagedContent bool                `json:"unstaged_content,omitempty"`
	Covered         bool                `json:"covered"`
	Profiles        []checkpointProfile `json:"profiles"`
	NextCommand     string              `json:"next_command,omitempty"`
	nextLabel       string
}

type checkpointProfile struct {
	Scope     string           `json:"scope"`
	Name      string           `json:"name"`
	State     string           `json:"state"`
	ReviewIDs []model.ReviewID `json:"review_ids"`
}

var rangeSourceDescriptions = map[string]string{
	subject.PushBaseUpstream:   "base is the merge base with the upstream branch",
	subject.PushBaseRemoteHead: "base is the merge base with origin/HEAD",
	rangeSourceFlags:           "range from --base",
}

// checkpointContent is what one checkpoint kind accepts, and the run command
// that would review exactly that content.
type checkpointContent struct {
	report     checkpointReport
	coverage   engine.CoverageSubject
	runCommand string
}

func executeCheckpointCheck(ctx context.Context, options checkpointOptions, streams commandIO) int {
	root, err := subject.ResolveRepositoryRoot(options.repository)
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	content, err := resolveCheckpointContent(root, options)
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	coverage, err := conductor.CheckCoverage(ctx, root, content.coverage)
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	report := completeCheckpointReport(content, coverage, options.configuration)
	code := renderLedgerOutput(options.format, streams.output, streams.errors, report, func(output *commandOutput) {
		printHumanCheckpoint(output, report)
	})
	if code == 0 && !report.Covered {
		return 1
	}
	return code
}

func resolveCheckpointContent(root string, options checkpointOptions) (checkpointContent, error) {
	if options.kind == checkpointPreCommit {
		return commitCheckpointContent(root, options.configuration)
	}
	return pushCheckpointContent(root, options)
}

func pushCheckpointContent(root string, options checkpointOptions) (checkpointContent, error) {
	base, source := options.base, rangeSourceFlags
	if base == "" {
		var err error
		base, source, err = subject.DefaultPushBase(root)
		if err != nil {
			return checkpointContent{}, fmt.Errorf("%w; pass --base to name the range to check", err)
		}
	}
	head := options.head
	if head == "" {
		head = "HEAD"
	}
	changes, err := subject.CommittedRangeContentChanges(root, model.CommittedRange(base, head))
	if err != nil {
		return checkpointContent{}, err
	}
	return checkpointContent{
		report:     checkpointReport{Checkpoint: checkpointPrePush, Base: changes.Base, Head: changes.Head, RangeSource: source},
		coverage:   engine.CoverageSubject{Changes: changes.Changes, Commits: changes.Commits},
		runCommand: fmt.Sprintf("review-party run --base %s --head %s%s", changes.Base, changes.Head, configurationArgument(options.configuration)),
	}, nil
}

func commitCheckpointContent(root, configuration string) (checkpointContent, error) {
	staged, err := subject.StagedContentChanges(root)
	if err != nil {
		return checkpointContent{}, err
	}
	working, err := subject.WorkingContentChanges(root)
	if err != nil {
		return checkpointContent{}, err
	}
	return checkpointContent{
		report:     checkpointReport{Checkpoint: checkpointPreCommit, UnstagedContent: !slices.Equal(staged, working)},
		coverage:   engine.CoverageSubject{Changes: staged},
		runCommand: "review-party run" + configurationArgument(configuration),
	}, nil
}

// completeCheckpointReport names one next step: a run when any Profile lacks
// a Review, otherwise a wait on the first Review still running.
func completeCheckpointReport(content checkpointContent, coverage engine.CoverageReport, configuration string) checkpointReport {
	report := content.report
	report.Covered = coverage.Covered
	report.Profiles = make([]checkpointProfile, 0, len(coverage.Profiles))
	var running model.ReviewID
	missing := false
	for _, profile := range coverage.Profiles {
		ids := append([]model.ReviewID{}, profile.ReviewIDs...)
		report.Profiles = append(report.Profiles, checkpointProfile{Scope: string(profile.Scope), Name: profile.Profile, State: string(profile.State), ReviewIDs: ids})
		missing = missing || profile.State == engine.CoverageMissing
		if running == "" && profile.State == engine.CoverageRunning {
			running = ids[0]
		}
	}
	switch {
	case missing:
		report.nextLabel, report.NextCommand = "next", content.runCommand
	case running != "":
		report.nextLabel, report.NextCommand = "wait", fmt.Sprintf("review-party wait %s%s", running, configurationArgument(configuration))
	}
	return report
}

func printHumanCheckpoint(output *commandOutput, report checkpointReport) {
	if report.Checkpoint == checkpointPreCommit {
		output.write("%s checkpoint: staged changes\n", report.Checkpoint)
	} else {
		output.write("%s checkpoint: %s..%s (%s)\n", report.Checkpoint, report.Base, report.Head, rangeSourceDescriptions[report.RangeSource])
	}
	for _, profile := range report.Profiles {
		output.write("%s (%s): %s\n", profile.Name, profile.Scope, checkpointProfileState(profile))
	}
	if report.nextLabel == "next" && report.UnstagedContent {
		output.write("The Review must match the staged content, so stash or stage the rest of your changes first.\n")
	}
	if report.NextCommand != "" {
		output.write("%s: %s\n", report.nextLabel, report.NextCommand)
	}
}

func checkpointProfileState(profile checkpointProfile) string {
	ids := make([]string, 0, len(profile.ReviewIDs))
	for _, id := range profile.ReviewIDs {
		ids = append(ids, string(id))
	}
	switch {
	case len(ids) == 0:
		return profile.State
	case profile.State == string(engine.CoverageCovered):
		return "covered by " + strings.Join(ids, ", ")
	default:
		return profile.State + " " + strings.Join(ids, ", ")
	}
}
