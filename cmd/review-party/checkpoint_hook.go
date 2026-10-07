package main

// The git hook entry point decides one Checkpoint for git. It refuses only on
// a decision: a declared Checkpoint whose change lacks its Reviews. Anything
// that prevents a decision warns on one line and lets git continue.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
	"reviewparty/internal/subject"
)

func newCheckpointHookCommand(streams commandIO) *cobra.Command {
	hook := &cobra.Command{Use: "hook", Short: "Entry points that Checkpoint Integrations call", Args: cobra.NoArgs, RunE: showCommandHelp}
	git := &cobra.Command{
		Use:   "git <pre-push|pre-commit> [hook arguments]",
		Short: "Decide a declared Checkpoint from inside a git hook",
		Long: `Decide a declared Checkpoint from inside a git hook. review-party checkpoint
install git wires this command into the repository's hooks.

pre-push reads the refs git sends on standard input and checks each pushed
range; pre-commit checks the staged content. Exits 0 silently when the
Checkpoint is not declared or passes, and 1 with one line naming the next
command when it does not. Setup, configuration, or git errors warn on one
line and exit 0, so a broken setup never stops a push or commit.`,
		Args: cobra.MatchAll(cobra.MinimumNArgs(1), func(_ *cobra.Command, args []string) error {
			_, err := configuration.ParseCheckpointName(args[0])
			return err
		}),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := checkpointOptions{name: configuration.CheckpointName(args[0]), configuration: stringFlag(cmd, "config")}
			return commandResult(executeCheckpointHook(cmd.Context(), options, args[1:], streams))
		},
	}
	addConfigurationFlag(git)
	hook.AddCommand(git)
	for _, adapter := range agentIntegrations {
		hook.AddCommand(newAgentHookCommand(adapter, streams))
	}
	return hook
}

// hookDecision is what one hook run concluded: a refusal line when the
// Checkpoint does not pass, a warning when no decision could be made.
type hookDecision struct {
	refusal string
	warning error
}

func executeCheckpointHook(ctx context.Context, options checkpointOptions, hookArgs []string, streams commandIO) int {
	decision := decideCheckpointHook(ctx, options, hookArgs, streams)
	switch {
	case decision.refusal != "":
		return printFailure(streams.errors, errors.New(decision.refusal))
	case decision.warning != nil:
		printCommandError(streams.errors, 0, fmt.Errorf("warning: %s Checkpoint not checked: %w", options.name, decision.warning))
	}
	return 0
}

func decideCheckpointHook(ctx context.Context, options checkpointOptions, hookArgs []string, streams commandIO) hookDecision {
	root, declared, err := declaredCheckpoint(options, streams)
	switch {
	case err != nil:
		return hookDecision{warning: err}
	case !declared:
		return hookDecision{}
	case options.name == configuration.CheckpointPreCommit:
		return decideHookContent(ctx, options)
	}
	refs, err := readPushedRefs(hookArgs, streams.input)
	if err != nil {
		return hookDecision{warning: err}
	}
	return decidePushedRefs(ctx, root, options, refs)
}

// declaredCheckpoint finds the root of the repository a hook checks and
// whether it declares the Checkpoint. Every hook stays silent on an
// undeclared one.
func declaredCheckpoint(options checkpointOptions, streams commandIO) (string, bool, error) {
	root, err := subject.ResolveRepositoryRoot(options.repository)
	if err != nil {
		return "", false, err
	}
	declared, err := streams.configurationManager(options.configuration).Checkpoints(configuration.Repository(root))
	if err != nil {
		return "", false, err
	}
	_, ok := declared[options.name]
	return root, ok, nil
}

// readPushedRefs parses the refs git sends a pre-push hook. Its first
// argument names the remote.
func readPushedRefs(hookArgs []string, input io.Reader) ([]subject.PushedRef, error) {
	remote := "origin"
	if len(hookArgs) > 0 {
		remote = hookArgs[0]
	}
	payload, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("read pushed refs: %w", err)
	}
	return subject.ParsePushedRefs(remote, payload)
}

// decidePushedRefs checks every pushed ref and refuses on the first that does
// not pass. A ref without a base is allowed with a warning.
func decidePushedRefs(ctx context.Context, root string, options checkpointOptions, refs []subject.PushedRef) hookDecision {
	var warnings []error
	for _, ref := range refs {
		if ref.Deletes() {
			continue
		}
		base, err := subject.PushedRefBase(root, ref)
		if err != nil {
			warnings = append(warnings, err)
			continue
		}
		options.base, options.head = base, ref.LocalObject
		decision := decideHookContent(ctx, options)
		if decision.refusal != "" {
			return decision
		}
		if decision.warning != nil {
			warnings = append(warnings, decision.warning)
		}
	}
	return joinedWarning(warnings)
}

// joinedWarning folds the warnings of several checks into one line.
func joinedWarning(warnings []error) hookDecision {
	if len(warnings) == 0 {
		return hookDecision{}
	}
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		messages = append(messages, warning.Error())
	}
	return hookDecision{warning: errors.New(strings.Join(messages, "; "))}
}

func decideHookContent(ctx context.Context, options checkpointOptions) hookDecision {
	target, err := openCheckpoint(options)
	if err != nil {
		return hookDecision{warning: err}
	}
	report, err := target.check(ctx, options.configuration)
	if err != nil {
		return hookDecision{warning: err}
	}
	if report.State.Passes() {
		return hookDecision{}
	}
	return hookDecision{refusal: hookRefusal(report)}
}

// hookRefusal is the one line a refused hook prints. It names the unreviewed
// lines and the one next command, never a judgment of the change. A spent
// budget names no command: a person decides.
func hookRefusal(report checkpointReport) string {
	scope := "the staged changes"
	switch {
	case report.Checkpoint == configuration.CheckpointPrePush:
		scope = shortObject(report.Base) + ".." + shortObject(report.Head)
	case report.commit == commitTracked:
		scope = "the tracked changes"
	}
	var line string
	switch report.nextLabel {
	case "wait":
		line = fmt.Sprintf("%s Checkpoint is waiting on a running Review of %s; wait: %s", report.Checkpoint, scope, report.NextCommand)
	case "judge":
		line = judgeRefusal(report, scope)
	case "stop":
		spent := spentProfile(report.Profiles)
		line = fmt.Sprintf("%s Checkpoint: %s in %s exceed %d after %d of %d Reviews; stop and ask a person", report.Checkpoint, unreviewedLines(spent.UnreviewedLines, report.binary), scope, report.allowance, spent.BudgetSpent, spent.ReviewBudget)
	default:
		line = fmt.Sprintf("%s Checkpoint: %s in %s; next: %s", report.Checkpoint, unreviewedLines(report.UnreviewedLines, report.binary), scope, report.NextCommand)
	}
	if report.WaiveCommand != "" {
		line += "; or waive: " + report.WaiveCommand
	}
	return line
}

// judgeRefusal names the first Review whose Findings need verdicts and counts
// the rest, since each Review takes its own finding record command.
func judgeRefusal(report checkpointReport, scope string) string {
	unjudged := unjudgedReviews(report.Profiles)
	line := fmt.Sprintf("%s Checkpoint needs a verdict on %s of %s for %s; judge: %s", report.Checkpoint, findingOrdinals(unjudged[0].Ordinals), unjudged[0].Review, scope, report.NextCommand)
	switch more := len(unjudged) - 1; {
	case more == 1:
		line += "; 1 more Review needs verdicts"
	case more > 1:
		line += fmt.Sprintf("; %d more Reviews need verdicts", more)
	}
	return line
}

func shortObject(object string) string {
	if len(object) > 12 {
		return object[:12]
	}
	return object
}
