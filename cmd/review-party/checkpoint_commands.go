// Checkpoint commands tell a Git hook whether the Reviews this repository
// selects already cover the content a push or commit would accept. They
// report coverage; the Caller decides whether an uncovered Checkpoint blocks.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

const rangeSourceFlags = "flags"

func newCheckpointCommand(streams commandIO) *cobra.Command {
	checkpoint := &cobra.Command{
		Use:   "checkpoint",
		Short: "Check, waive, and install Review Checkpoints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	checkpoint.AddCommand(newCheckpointCheckCommand(streams), newCheckpointWaiveCommand(streams), newCheckpointHookCommand(streams), newCheckpointInstallCommand(streams))
	return checkpoint
}

func newCheckpointCheckCommand(streams commandIO) *cobra.Command {
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
covered. When the repository declares the Checkpoint, its exempt paths are
left out of both sides, a small enough change passes, and a matching Waiver
passes. Exits 0 when the Checkpoint passes, 1 when a Review is missing or still
running, and 2 on a usage error or when Review Party is not initialized.`,
		Example: "  review-party checkpoint check pre-push\n  review-party checkpoint check pre-push --base origin/main --head HEAD\n  review-party checkpoint check pre-commit --format json",
		Args:    cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := checkpointOptionsFromCommand(cmd, args)
			if err != nil {
				return err
			}
			return commandResult(executeCheckpointCheck(cmd.Context(), options, streams))
		},
	}
	addCheckpointRangeFlags(check, "Git repository to check")
	check.ValidArgs = checkpointNameArguments()
	return check
}

func newCheckpointWaiveCommand(streams commandIO) *cobra.Command {
	waive := &cobra.Command{
		Use:   "waive <pre-push|pre-commit> --reason TEXT",
		Short: "Record a Waiver that passes a declared Checkpoint for the exact current change",
		Long: `Record a Waiver that passes a declared Checkpoint for exactly the content
check would examine. Any later change to a non-exempt path needs its own
Reviews or Waiver.

The Checkpoint's waivers policy decides who may waive: anyone records directly,
human requires a person to confirm in a terminal (--yes does not count), and
none refuses.`,
		Example: "  review-party checkpoint waive pre-push --reason 'revert of a broken release'",
		Args:    cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := checkpointOptionsFromCommand(cmd, args)
			if err != nil {
				return err
			}
			waiver := checkpointWaiveOptions{checkpointOptions: options, reason: strings.TrimSpace(stringFlag(cmd, "reason")), yes: boolFlag(cmd, "yes")}
			if waiver.reason == "" {
				return errors.New("--reason is required: say why this change passes without its Reviews")
			}
			return commandResult(executeCheckpointWaive(cmd.Context(), waiver, streams))
		},
	}
	addCheckpointRangeFlags(waive, "Git repository whose Checkpoint to waive")
	waive.Flags().String("reason", "", "Why this change passes without its Reviews")
	waive.Flags().Bool("yes", false, "Skip the confirmation when the policy lets anyone waive")
	waive.ValidArgs = checkpointNameArguments()
	return waive
}

func addCheckpointRangeFlags(cmd *cobra.Command, repository string) {
	addRepositoryFlag(cmd, repository)
	addConfigurationFlag(cmd)
	addFormatFlag(cmd)
	cmd.Flags().String("base", "", "pre-push: base revision of the range (default: merge base with the upstream, else with origin/HEAD)")
	cmd.Flags().String("head", "", "pre-push: head revision of the range (default: HEAD)")
}

type checkpointOptions struct {
	name          configuration.CheckpointName
	repository    string
	format        string
	configuration string
	base          string
	head          string
}

func checkpointOptionsFromCommand(cmd *cobra.Command, args []string) (checkpointOptions, error) {
	options := checkpointOptions{
		name: configuration.CheckpointName(args[0]), repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"),
		configuration: stringFlag(cmd, "config"), base: stringFlag(cmd, "base"), head: stringFlag(cmd, "head"),
	}
	if options.name == configuration.CheckpointPreCommit && options.base+options.head != "" {
		return checkpointOptions{}, errors.New("--base and --head apply only to pre-push")
	}
	if options.configuration == "" {
		return options, nil
	}
	// Suggested commands carry --config, so it must name the same file
	// wherever the Caller runs them.
	var err error
	options.configuration, err = filepath.Abs(options.configuration)
	return options, err
}

type checkpointReport struct {
	Checkpoint      configuration.CheckpointName `json:"checkpoint"`
	Base            string                       `json:"base,omitempty"`
	Head            string                       `json:"head,omitempty"`
	RangeSource     string                       `json:"range_source,omitempty"`
	UnstagedContent bool                         `json:"unstaged_content,omitempty"`
	State           engine.CheckpointState       `json:"state"`
	Covered         bool                         `json:"covered"`
	Waivers         configuration.WaiverPolicy   `json:"waivers,omitempty"`
	Exemption       *checkpointExemption         `json:"exemption,omitempty"`
	Waiver          *model.CheckpointWaiver      `json:"waiver,omitempty"`
	Profiles        []checkpointProfile          `json:"profiles"`
	NextCommand     string                       `json:"next_command,omitempty"`
	WaiveCommand    string                       `json:"waive_command,omitempty"`
	nextLabel       string
}

type checkpointExemption struct {
	Reason           engine.ExemptionReason `json:"reason,omitempty"`
	ExemptPaths      []string               `json:"exempt_paths,omitempty"`
	ChangedLines     int                    `json:"changed_lines,omitempty"`
	SmallChangeLines int                    `json:"small_change_lines,omitempty"`
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

// checkpointContent is what one Checkpoint accepts, and the commands that
// would review or waive exactly that content.
type checkpointContent struct {
	report       checkpointReport
	coverage     engine.CoverageSubject
	runCommand   string
	waiveCommand string
}

type checkpointTarget struct {
	root      string
	content   checkpointContent
	conductor *engine.Conductor
}

func openCheckpoint(options checkpointOptions) (checkpointTarget, error) {
	root, err := subject.ResolveRepositoryRoot(options.repository)
	if err != nil {
		return checkpointTarget{}, err
	}
	content, err := resolveCheckpointContent(root, options)
	if err != nil {
		return checkpointTarget{}, err
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return checkpointTarget{}, err
	}
	return checkpointTarget{root: root, content: content, conductor: conductor}, nil
}

func (target checkpointTarget) request() engine.CheckpointRequest {
	return engine.CheckpointRequest{Repository: target.root, Name: target.content.report.Checkpoint, Content: target.content.coverage}
}

func (target checkpointTarget) check(ctx context.Context, configurationPath string) (checkpointReport, error) {
	result, err := target.conductor.CheckCheckpoint(ctx, target.request())
	if err != nil {
		return checkpointReport{}, err
	}
	return completeCheckpointReport(target.content, result, configurationPath), nil
}

func executeCheckpointCheck(ctx context.Context, options checkpointOptions, streams commandIO) int {
	target, err := openCheckpoint(options)
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	report, err := target.check(ctx, options.configuration)
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	return renderCheckpointReport(options.format, streams, report)
}

func renderCheckpointReport(format string, streams commandIO, report checkpointReport) int {
	code := renderLedgerOutput(format, streams.output, streams.errors, report, func(output *commandOutput) {
		printHumanCheckpoint(output, report)
	})
	if code == 0 && !report.State.Passes() {
		return 1
	}
	return code
}

func resolveCheckpointContent(root string, options checkpointOptions) (checkpointContent, error) {
	if options.name == configuration.CheckpointPreCommit {
		return commitCheckpointContent(root, options)
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
	rangeArguments := fmt.Sprintf(" --base %s --head %s%s", changes.Base, changes.Head, options.followUpArguments(root))
	return checkpointContent{
		report:       checkpointReport{Checkpoint: configuration.CheckpointPrePush, Base: changes.Base, Head: changes.Head, RangeSource: source},
		coverage:     engine.CoverageSubject{Changes: changes.Changes, Commits: changes.Commits, Lines: changes.Lines},
		runCommand:   "review-party run" + rangeArguments,
		waiveCommand: "review-party checkpoint waive pre-push" + rangeArguments + ` --reason "<why>"`,
	}, nil
}

func commitCheckpointContent(root string, options checkpointOptions) (checkpointContent, error) {
	staged, err := subject.StagedContentChanges(root)
	if err != nil {
		return checkpointContent{}, err
	}
	working, err := subject.WorkingContentChanges(root)
	if err != nil {
		return checkpointContent{}, err
	}
	lines, err := subject.StagedLineCounts(root)
	if err != nil {
		return checkpointContent{}, err
	}
	return checkpointContent{
		report:       checkpointReport{Checkpoint: configuration.CheckpointPreCommit, UnstagedContent: !slices.Equal(staged, working)},
		coverage:     engine.CoverageSubject{Changes: staged, Lines: lines},
		runCommand:   "review-party run" + options.followUpArguments(root),
		waiveCommand: "review-party checkpoint waive pre-commit" + options.followUpArguments(root) + ` --reason "<why>"`,
	}, nil
}

// followUpArguments keep a suggested command on the repository and
// configuration this check used, wherever the Caller runs it from.
func (options checkpointOptions) followUpArguments(root string) string {
	arguments := configurationArgument(options.configuration)
	if options.repository != "" {
		arguments = " --repo " + shellQuoteArgument(root) + arguments
	}
	return arguments
}

// completeCheckpointReport names one next step for a Checkpoint that does not
// pass: a run when any Profile lacks a Review, otherwise a wait on the first
// Review still running. Only a policy that lets anyone waive adds the waive
// command.
func completeCheckpointReport(content checkpointContent, result engine.CheckpointReport, configurationPath string) checkpointReport {
	report := content.report
	report.State = result.State
	report.Covered = result.Coverage.Covered
	report.Waiver = result.Waiver
	report.Profiles = make([]checkpointProfile, 0, len(result.Coverage.Profiles))
	for _, profile := range result.Coverage.Profiles {
		ids := append([]model.ReviewID{}, profile.ReviewIDs...)
		report.Profiles = append(report.Profiles, checkpointProfile{Scope: string(profile.Scope), Name: profile.Profile, State: string(profile.State), ReviewIDs: ids})
	}
	if result.Declaration != nil {
		report.Waivers = result.Declaration.Waivers
		report.Exemption = newCheckpointExemption(result.Exemption, result.Declaration.SmallChangeLines)
	}
	if result.State.Passes() {
		return report
	}
	report.nextLabel, report.NextCommand = nextCheckpointStep(report.Profiles, content.runCommand, configurationPath)
	if report.Waivers == configuration.WaiversAnyone {
		report.WaiveCommand = content.waiveCommand
	}
	return report
}

func newCheckpointExemption(exemption *engine.CheckpointExemption, smallChangeLines int) *checkpointExemption {
	if exemption == nil {
		return nil
	}
	return &checkpointExemption{Reason: exemption.Reason, ExemptPaths: exemption.ExemptPaths, ChangedLines: exemption.ChangedLines, SmallChangeLines: smallChangeLines}
}

func nextCheckpointStep(profiles []checkpointProfile, runCommand, configurationPath string) (label, command string) {
	var running model.ReviewID
	for _, profile := range profiles {
		if profile.State == string(engine.CoverageMissing) {
			return "next", runCommand
		}
		if running == "" && profile.State == string(engine.CoverageRunning) {
			running = profile.ReviewIDs[0]
		}
	}
	if running == "" {
		return "", ""
	}
	return "wait", fmt.Sprintf("review-party wait %s%s", running, configurationArgument(configurationPath))
}

func printHumanCheckpoint(output *commandOutput, report checkpointReport) {
	if report.Checkpoint == configuration.CheckpointPreCommit {
		output.write("%s checkpoint: staged changes\n", report.Checkpoint)
	} else {
		output.write("%s checkpoint: %s..%s (%s)\n", report.Checkpoint, report.Base, report.Head, rangeSourceDescriptions[report.RangeSource])
	}
	printHumanExemption(output, report.Exemption)
	for _, profile := range report.Profiles {
		output.write("%s (%s): %s\n", profile.Name, profile.Scope, checkpointProfileState(profile))
	}
	if report.Waiver != nil {
		output.write("waived by %s (%s): %s\n", report.Waiver.ID, report.Waiver.WaivedBy, report.Waiver.Reason)
	}
	printHumanNextSteps(output, report)
}

func printHumanNextSteps(output *commandOutput, report checkpointReport) {
	if report.nextLabel == "next" && report.UnstagedContent {
		output.write("The Review must match the staged content, so stash or stage the rest of your changes first.\n")
	}
	if report.NextCommand != "" {
		output.write("%s: %s\n", report.nextLabel, report.NextCommand)
	}
	if report.WaiveCommand != "" {
		output.write("waive: %s\n", report.WaiveCommand)
	}
}

func printHumanExemption(output *commandOutput, exemption *checkpointExemption) {
	if exemption == nil {
		return
	}
	if len(exemption.ExemptPaths) > 0 {
		output.write("exempt paths: %s\n", strings.Join(exemption.ExemptPaths, ", "))
	}
	switch {
	case exemption.Reason == engine.ExemptByPaths:
		output.write("exempt: every changed path is exempt\n")
	case exemption.Reason == engine.ExemptBySmallChange:
		output.write("exempt: small change of %d lines, at most %d pass\n", exemption.ChangedLines, exemption.SmallChangeLines)
	case exemption.ChangedLines > 0:
		output.write("changed lines: %d, over the small-change limit of %d\n", exemption.ChangedLines, exemption.SmallChangeLines)
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

type checkpointWaiveOptions struct {
	checkpointOptions
	reason string
	yes    bool
}

// executeCheckpointWaive asks for confirmation before recording: always when
// only a person may waive, and in a terminal without --yes otherwise.
func executeCheckpointWaive(ctx context.Context, options checkpointWaiveOptions, streams commandIO) int {
	target, err := openCheckpoint(options.checkpointOptions)
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	current, err := target.conductor.CheckCheckpoint(ctx, target.request())
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	if current.Declaration == nil || current.State.Passes() {
		return target.waive(ctx, options, model.WaivedByNonInteractive, streams)
	}
	prompt := options.promptStream(streams)
	by, err := options.confirmation(current.Declaration.Waivers, streams.isTerminal(streams.input) && streams.isTerminal(prompt))
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	if err := options.confirm(by, streams.input, prompt); err != nil {
		return printFailure(streams.errors, err)
	}
	return target.waive(ctx, options, by, streams)
}

func (options checkpointWaiveOptions) confirmation(policy configuration.WaiverPolicy, interactive bool) (model.WaivedBy, error) {
	if err := engine.WaiverPermission(policy, model.WaivedByTerminal); err != nil {
		return "", fmt.Errorf("checkpoint %s: %w by its waivers policy", options.name, err)
	}
	switch {
	case policy == configuration.WaiversHuman && !interactive:
		return "", fmt.Errorf("checkpoint %s allows only human waivers; a person must run review-party checkpoint waive %s in a terminal", options.name, options.name)
	case policy == configuration.WaiversHuman:
		return model.WaivedByTerminal, nil
	case interactive && !options.yes:
		return model.WaivedByTerminal, nil
	default:
		return model.WaivedByNonInteractive, nil
	}
}

// promptStream is where the confirmation goes: standard error under JSON, so
// standard output carries only the report and may be piped.
func (options checkpointWaiveOptions) promptStream(streams commandIO) io.Writer {
	if options.format == "json" {
		return streams.errors
	}
	return streams.output
}

func (options checkpointWaiveOptions) confirm(by model.WaivedBy, input io.Reader, prompt io.Writer) error {
	if by != model.WaivedByTerminal {
		return nil
	}
	question := fmt.Sprintf("Waive the %s Checkpoint for this exact change, reason %q?", options.name, options.reason)
	confirmed, err := confirmPrompt(input, prompt, question)
	if err == nil && !confirmed {
		err = errors.New("waiver cancelled")
	}
	return err
}

func (target checkpointTarget) waive(ctx context.Context, options checkpointWaiveOptions, by model.WaivedBy, streams commandIO) int {
	result, err := target.conductor.WaiveCheckpoint(ctx, engine.WaiverRequest{Checkpoint: target.request(), Reason: options.reason, WaivedBy: by})
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	return renderCheckpointReport(options.format, streams, completeCheckpointReport(target.content, result, options.configuration))
}
