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
	"strconv"
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
		Short: "Report what each selected Review has left unreviewed of the content a push or commit accepts",
		Long: `Report, for each Profile the repository selects, how much of the content the
checkpoint accepts no completed Review of that Profile has examined.

pre-push checks the range from --base to --head. Without --base, the base is
the merge base of HEAD and the branch's upstream, else of HEAD and origin/HEAD. pre-commit
checks the staged content against HEAD.

A Profile has reviewed a path up to the newest blob its completed Reviews
reach from the base blob, so rebased, amended, squashed, and recommitted
content stays reviewed and a Review of the unreviewed delta extends the
chain. The lines left between that blob and the current one are the
unreviewed lines. When the repository declares the Checkpoint, its exempt
paths are left out, up to unreviewed_lines pass without a new Review, a
change that has spent its review_budget asks for a person, and a matching
Waiver passes. Under the judged requirement, every Finding of the Reviews on
the chain also needs a current verdict, recorded with review-party finding
record. Exits 0 when the Checkpoint passes, 1 when it does not, and 2 on a
usage error or when Review Party is not initialized.`,
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
		Short: "Record a Waiver that passes a declared Checkpoint for the exact unreviewed delta",
		Long: `Record a Waiver that passes a declared Checkpoint for exactly the unreviewed
delta check reports. Any further change to a non-exempt path needs its own
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
	// commit says which content a pre-commit Checkpoint checks.
	commit commitScope
}

// commitScope is the content a commit takes: the index, or every tracked
// change as git commit -a commits it.
type commitScope int

const (
	commitStaged commitScope = iota
	commitTracked
)

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
	// UnreviewedLines is the size of the delta run --unreviewed would review:
	// from the newest state every Profile with something unreviewed reached.
	UnreviewedLines int                        `json:"unreviewed_lines"`
	Waivers         configuration.WaiverPolicy `json:"waivers,omitempty"`
	Exemption       *checkpointExemption       `json:"exemption,omitempty"`
	Waiver          *model.CheckpointWaiver    `json:"waiver,omitempty"`
	Profiles        []checkpointProfile        `json:"profiles"`
	NextCommand     string                     `json:"next_command,omitempty"`
	WaiveCommand    string                     `json:"waive_command,omitempty"`
	nextLabel       string
	commit          commitScope
	allowance       int
	binary          bool
}

type checkpointExemption struct {
	ExemptPaths []string `json:"exempt_paths,omitempty"`
}

type checkpointProfile struct {
	Scope   string           `json:"scope"`
	Name    string           `json:"name"`
	State   string           `json:"state"`
	Reviews []model.ReviewID `json:"reviews"`
	Running []model.ReviewID `json:"running,omitempty"`
	// Unreviewed is the delta from this Profile's reviewed state, per path.
	Unreviewed      []checkpointUnreviewed `json:"unreviewed,omitempty"`
	UnreviewedLines int                    `json:"unreviewed_lines"`
	BudgetSpent     int                    `json:"budget_spent"`
	ReviewBudget    int                    `json:"review_budget"`
	// Unjudged lists, under the judged requirement, the Reviews on the chain
	// with Findings that have no current verdict.
	Unjudged []engine.UnjudgedReview `json:"unjudged,omitempty"`
}

type checkpointUnreviewed struct {
	Path   string `json:"path"`
	Lines  int    `json:"lines"`
	Binary bool   `json:"binary,omitempty"`
}

var rangeSourceDescriptions = map[string]string{
	subject.PushBaseUpstream:   "base is the merge base with the upstream branch",
	subject.PushBaseRemoteHead: "base is the merge base with origin/HEAD",
	rangeSourceFlags:           "range from --base",
}

// checkpointContent is what one Checkpoint accepts, and the commands that
// would review or waive exactly what is unreviewed of it.
type checkpointContent struct {
	report       checkpointReport
	content      []model.ContentChange
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
	return engine.CheckpointRequest{Repository: target.root, Name: target.content.report.Checkpoint, Content: target.content.content}
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
		content:      changes.Changes,
		runCommand:   "review-party run --unreviewed" + rangeArguments,
		waiveCommand: "review-party checkpoint waive pre-push" + rangeArguments + ` --reason "<why>"`,
	}, nil
}

func commitCheckpointContent(root string, options checkpointOptions) (checkpointContent, error) {
	if options.commit == commitTracked {
		return trackedCheckpointContent(root, options)
	}
	staged, err := subject.StagedContentChanges(root)
	if err != nil {
		return checkpointContent{}, err
	}
	working, err := subject.WorkingContentChanges(root, subject.AllFiles)
	if err != nil {
		return checkpointContent{}, err
	}
	return checkpointContent{
		report:       checkpointReport{Checkpoint: configuration.CheckpointPreCommit, UnstagedContent: !slices.Equal(staged, working)},
		content:      staged,
		runCommand:   "review-party run --unreviewed" + options.followUpArguments(root),
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

// trackedCheckpointContent is what git commit -a commits. No waive command
// names this content, since checkpoint waive pre-commit waives the index.
func trackedCheckpointContent(root string, options checkpointOptions) (checkpointContent, error) {
	tracked, err := subject.WorkingContentChanges(root, subject.TrackedFiles)
	if err != nil {
		return checkpointContent{}, err
	}
	return checkpointContent{
		report:     checkpointReport{Checkpoint: configuration.CheckpointPreCommit, commit: commitTracked},
		content:    tracked,
		runCommand: "review-party run --unreviewed" + options.followUpArguments(root),
	}, nil
}

// completeCheckpointReport names one next step for a Checkpoint that does not
// pass: a Review of the unreviewed delta, a wait on a running Review, a stop
// for a person when the budget is spent, or verdicts on the first unjudged
// Review. Only the stop offers the waive command, and only under a policy
// that lets anyone waive: every other refusal is its one command.
func completeCheckpointReport(content checkpointContent, result engine.CheckpointReport, configurationPath string) checkpointReport {
	report := content.report
	report.State = result.State
	report.Waiver = result.Waiver
	report.UnreviewedLines = result.Coverage.UnreviewedLines.Total
	report.binary = len(result.Coverage.UnreviewedLines.Binary) > 0
	budget := 0
	if result.Declaration != nil {
		budget = result.Declaration.ReviewBudget
		report.allowance = result.Declaration.UnreviewedLines
		report.Waivers = result.Declaration.Waivers
	}
	if result.Exemption != nil {
		report.Exemption = &checkpointExemption{ExemptPaths: result.Exemption.ExemptPaths}
	}
	report.Profiles = make([]checkpointProfile, 0, len(result.Coverage.Profiles))
	for _, profile := range result.Coverage.Profiles {
		report.Profiles = append(report.Profiles, newCheckpointProfile(profile, budget))
	}
	if result.State.Passes() {
		return report
	}
	report.nextLabel, report.NextCommand = nextCheckpointStep(report, content.runCommand, configurationPath)
	if result.State == engine.CheckpointSpent && report.Waivers == configuration.WaiversAnyone {
		report.WaiveCommand = content.waiveCommand
	}
	return report
}

func newCheckpointProfile(profile engine.ProfileCoverage, budget int) checkpointProfile {
	entry := checkpointProfile{
		Scope: string(profile.Scope), Name: profile.Profile, State: string(profile.State),
		Reviews: append([]model.ReviewID{}, profile.Reviews...), Running: profile.Running,
		UnreviewedLines: profile.UnreviewedLines.Total, BudgetSpent: profile.Spent, ReviewBudget: budget, Unjudged: profile.Unjudged,
	}
	for _, change := range profile.Unreviewed {
		entry.Unreviewed = append(entry.Unreviewed, checkpointUnreviewed{
			Path: change.Path, Lines: profile.UnreviewedLines.ByPath[change.Path], Binary: slices.Contains(profile.UnreviewedLines.Binary, change.Path),
		})
	}
	return entry
}

// nextCheckpointStep follows the Checkpoint state. A spent budget has no
// command: a person decides.
func nextCheckpointStep(report checkpointReport, runCommand, configurationPath string) (label, command string) {
	switch report.State {
	case engine.CheckpointMissing:
		return "next", runCommand
	case engine.CheckpointRunning:
		return "wait", fmt.Sprintf("review-party wait %s%s", runningReview(report.Profiles), configurationArgument(configurationPath))
	case engine.CheckpointSpent:
		return "stop", ""
	case engine.CheckpointUnjudged:
		return "judge", fmt.Sprintf("review-party finding record %s%s", unjudgedReviews(report.Profiles)[0].Review, configurationArgument(configurationPath))
	case engine.CheckpointCovered, engine.CheckpointResidual, engine.CheckpointExempt, engine.CheckpointWaived:
		return "", ""
	}
	return "", ""
}

func runningReview(profiles []checkpointProfile) model.ReviewID {
	for _, profile := range profiles {
		if len(profile.Running) > 0 {
			return profile.Running[0]
		}
	}
	return ""
}

func spentProfile(profiles []checkpointProfile) checkpointProfile {
	for _, profile := range profiles {
		if profile.State == string(engine.CoverageSpent) {
			return profile
		}
	}
	return checkpointProfile{}
}

func unjudgedReviews(profiles []checkpointProfile) []engine.UnjudgedReview {
	var unjudged []engine.UnjudgedReview
	for _, profile := range profiles {
		unjudged = append(unjudged, profile.Unjudged...)
	}
	return unjudged
}

func printHumanCheckpoint(output *commandOutput, report checkpointReport) {
	if report.Checkpoint == configuration.CheckpointPreCommit {
		output.write("%s checkpoint: staged changes\n", report.Checkpoint)
	} else {
		output.write("%s checkpoint: %s..%s (%s)\n", report.Checkpoint, report.Base, report.Head, rangeSourceDescriptions[report.RangeSource])
	}
	if report.Exemption != nil {
		output.write("exempt paths: %s\n", strings.Join(report.Exemption.ExemptPaths, ", "))
	}
	if report.State == engine.CheckpointExempt {
		output.write("exempt: every changed path is exempt\n")
	}
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
	switch {
	case report.nextLabel == "stop":
		spent := spentProfile(report.Profiles)
		output.write("stop: %s exceed %d after %d of %d Reviews; ask a person\n", unreviewedLines(spent.UnreviewedLines, report.binary), report.allowance, spent.BudgetSpent, spent.ReviewBudget)
	case report.NextCommand != "":
		output.write("%s: %s\n", report.nextLabel, report.NextCommand)
	}
	if report.WaiveCommand != "" {
		output.write("waive: %s\n", report.WaiveCommand)
	}
}

// checkpointProfileState is one line of facts: the evidence Reviews, the
// unreviewed delta, and the budget when the repository declares one.
func checkpointProfileState(profile checkpointProfile) string {
	switch profile.State {
	case string(engine.CoverageCovered), string(engine.CoverageUnjudged):
		return "covered by " + joinReviewIDs(profile.Reviews) + unjudgedFindings(profile.Unjudged)
	}
	line := profile.State
	if len(profile.Running) > 0 {
		line += " " + joinReviewIDs(profile.Running)
	}
	line += ", " + unreviewedSummary(profile.UnreviewedLines, profile.Unreviewed)
	if profile.ReviewBudget > 0 {
		line += fmt.Sprintf("; %d of %d Reviews spent", profile.BudgetSpent, profile.ReviewBudget)
	}
	if len(profile.Reviews) > 0 {
		line += "; after " + joinReviewIDs(profile.Reviews)
	}
	return line
}

func unreviewedSummary(lines int, unreviewed []checkpointUnreviewed) string {
	names := make([]string, 0, len(unreviewed))
	for _, change := range unreviewed {
		name := change.Path
		if change.Binary {
			name += " (binary)"
		}
		names = append(names, name)
	}
	return unreviewedLines(lines, false) + " in " + strings.Join(names, ", ")
}

// unreviewedLines reads "412 unreviewed lines", "1 unreviewed line", or, when
// a delta has no line count, "0 unreviewed lines and binary content".
func unreviewedLines(lines int, binary bool) string {
	text := fmt.Sprintf("%d unreviewed lines", lines)
	if lines == 1 {
		text = "1 unreviewed line"
	}
	if binary {
		text += " and binary content"
	}
	return text
}

func joinReviewIDs(ids []model.ReviewID) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, string(id))
	}
	return strings.Join(names, ", ")
}

func unjudgedFindings(unjudged []engine.UnjudgedReview) string {
	text := ""
	for _, review := range unjudged {
		text += fmt.Sprintf("; no verdict on %s of %s", findingOrdinals(review.Ordinals), review.Review)
	}
	return text
}

// findingOrdinals names Findings by ordinal: "Finding 2" or "Findings 1, 3".
func findingOrdinals(ordinals []int) string {
	numbers := make([]string, 0, len(ordinals))
	for _, ordinal := range ordinals {
		numbers = append(numbers, strconv.Itoa(ordinal))
	}
	if len(numbers) == 1 {
		return "Finding " + numbers[0]
	}
	return "Findings " + strings.Join(numbers, ", ")
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
