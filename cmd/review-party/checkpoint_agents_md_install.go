package main

// The agents-md Integration keeps one marked block in the repository's agent
// instructions that tells a Caller Agent how to satisfy each declared
// Checkpoint before a hook would refuse. The block is generated from the
// declaration, so a changed declaration leaves it stale and the installer
// replaces only the lines between its markers.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
)

// HTML comments, so rendered Markdown hides the markers.
const (
	agentsMDBegin = "<!-- review-party checkpoints: begin -->"
	agentsMDEnd   = "<!-- review-party checkpoints: end -->"
)

func newAgentsMDInstallCommand(streams commandIO) *cobra.Command {
	command := &cobra.Command{
		Use:   string(configuration.IntegrationAgentsMD),
		Short: "Write the AGENTS.md block that tells agents how to satisfy every declared Checkpoint",
		Long: `Write a marked block, generated from the declared Checkpoints, into AGENTS.md at
the repository root. A repository with CLAUDE.md and no AGENTS.md gets the
block in CLAUDE.md; one with neither gets a new AGENTS.md.

A missing block is appended after one blank line. A block that no longer
matches the declaration is replaced between its markers, and nothing outside
them changes. Markers that are unbalanced or repeated are left for you to fix
by hand. Rerunning is safe.`,
		Example: "  review-party checkpoint install agents-md\n  review-party checkpoint install agents-md --yes",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := hookInstallOptions{
				repository: stringFlag(cmd, "repo"), configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes"),
				integration: configuration.IntegrationAgentsMD,
			}
			return commandResult(executeCheckpointInstall(options, streams))
		},
	}
	addRepositoryFlag(command, "Git repository whose declared Checkpoints the block describes")
	addConfigurationFlag(command)
	command.Flags().Bool("yes", false, "Write the block without a confirmation prompt")
	return command
}

// agentsMDFile is the instructions file the block belongs in: AGENTS.md, or
// CLAUDE.md when only that exists.
func agentsMDFile(root string) string {
	agents := filepath.Join(root, "AGENTS.md")
	if _, err := os.Lstat(agents); err == nil {
		return agents
	}
	claude := filepath.Join(root, "CLAUDE.md")
	if _, err := os.Lstat(claude); err == nil {
		return claude
	}
	return agents
}

func planAgentsMDInstall(target hookInstallTarget, manager *configuration.Manager) (hookInstallPlan, error) {
	declared, err := manager.Checkpoints(configuration.Repository(target.root))
	if err != nil {
		return hookInstallPlan{}, err
	}
	plan := hookInstallPlan{tool: hookToolAgentsMD, location: agentsMDFile(target.root), writes: map[string][]byte{}, fileMode: 0o644}
	if config := configurationArgument(target.configuration); config != "" {
		plan.warnings = append(plan.warnings, "warning: the block's commands do not carry --config, so agents that run them load the default configuration, not"+config)
	}
	names := configuration.SortedCheckpointNames(declared)
	content, found, err := plan.pending(plan.location)
	if err != nil {
		return hookInstallPlan{}, err
	}
	outcome, updated, manual := placeAgentsMDBlock(string(content), found, agentsMDBody(declared, names))
	if updated != string(content) {
		plan.writes[plan.location] = []byte(updated)
	}
	for _, name := range names {
		plan.steps = append(plan.steps, hookInstallStep{checkpoint: name, path: plan.location, outcome: outcome, manual: manual})
	}
	return plan, nil
}

// agentsMDInstruction is what one Checkpoint's line tells the agent to do
// before the action the Checkpoint guards: the first Review, the Review of
// what a fix left unreviewed, and what the Checkpoint counts.
type agentsMDInstruction struct {
	before   string
	review   string
	followUp string
	action   string
	lines    string
}

var agentsMDInstructions = map[configuration.CheckpointName]agentsMDInstruction{
	configuration.CheckpointPreCommit: {before: "Before committing", review: "stage the change, stash any other changes, and review it with `review-party run`", followUp: "`review-party run --unreviewed`", action: "commit", lines: "unreviewed staged lines"},
	configuration.CheckpointPrePush:   {before: "Before pushing", review: "review the change with `review-party run --base <upstream> --head HEAD`", followUp: "`review-party run --unreviewed --base <upstream> --head HEAD`", action: "push", lines: "unreviewed lines"},
}

// agentsMDBody is the text between the markers: one line per declared
// Checkpoint, then where to read more.
func agentsMDBody(declared map[configuration.CheckpointName]configuration.Checkpoint, names []configuration.CheckpointName) string {
	var body strings.Builder
	for _, name := range names {
		body.WriteString(agentsMDLine(name, declared[name]))
	}
	body.WriteString("`review-party checkpoint --help` has details.\n")
	return body.String()
}

// agentsMDLine names the commands that satisfy one Checkpoint, what it
// refuses, the fix allowance, and the budget. Waivers are mentioned only
// where any caller may record one, as the refusal does.
func agentsMDLine(name configuration.CheckpointName, checkpoint configuration.Checkpoint) string {
	instruction := agentsMDInstructions[name]
	var line strings.Builder
	fmt.Fprintf(&line, "%s, %s", instruction.before, instruction.review)
	refuses := fmt.Sprintf("more than %d %s", checkpoint.UnreviewedLines, instruction.lines)
	if checkpoint.Requirement == configuration.RequirementJudged {
		line.WriteString(" and record a verdict for each Finding with `review-party finding record <id>`")
		refuses += " or a Finding without a verdict"
	}
	fmt.Fprintf(&line, "; the %s Checkpoint refuses a %s with %s. %s", name, instruction.action, refuses, instruction.fixRule(checkpoint.UnreviewedLines))
	fmt.Fprintf(&line, " At most %s per change; when the Checkpoint says stop, report the unreviewed lines and stop.", countedReviews(checkpoint.ReviewBudget))
	if checkpoint.Waivers == configuration.WaiversAnyone {
		line.WriteString(" When a Review does not fit, waive it with `review-party checkpoint waive " + string(name) + ` --reason "<why>"` + "`.")
	}
	line.WriteString("\n")
	return line.String()
}

// fixRule tells the agent when a fix after a Review needs the Review of what
// it left unreviewed.
func (instruction agentsMDInstruction) fixRule(allowance int) string {
	if allowance == 0 {
		return "Every fix after a Review needs " + instruction.followUp + "."
	}
	return fmt.Sprintf("Fixes of up to %d lines after a Review need no new Review; a larger fix needs %s.", allowance, instruction.followUp)
}

func countedReviews(count int) string {
	if count == 1 {
		return "1 Review"
	}
	return fmt.Sprintf("%d Reviews", count)
}

// placeAgentsMDBlock decides what the block needs in the file's content and
// returns the content with it in place. Only the lines between one begin
// marker and the end marker after it are ever rewritten.
func placeAgentsMDBlock(content string, found bool, body string) (hookOutcome, string, []string) {
	block := agentsMDBegin + "\n" + body + agentsMDEnd + "\n"
	lines := strings.SplitAfter(content, "\n")
	sequence, at := agentsMDMarkers(lines)
	switch {
	case !found:
		return hookCreated, block, nil
	case sequence == "":
		return hookInserted, content + blankLineBefore(content) + block, nil
	case sequence != "be":
		return hookBlockMalformed, content, []string{
			fmt.Sprintf("Found %d %q and %d %q lines; the block needs one begin line before one end line.", strings.Count(sequence, "b"), agentsMDBegin, strings.Count(sequence, "e"), agentsMDEnd),
			"Delete the extra or misplaced marker lines, or both markers and the lines between them, then rerun the install.",
		}
	}
	if strings.Join(lines[at[0]+1:at[1]], "") == body {
		return hookInstalled, content, nil
	}
	return hookBlockStale, strings.Join(lines[:at[0]+1], "") + body + strings.Join(lines[at[1]:], ""), nil
}

// agentsMDMarkers reads the marker lines in file order as a sequence of "b"
// for a begin line and "e" for an end line, with their line indexes. One
// well-formed block reads "be". A line is a marker only when it holds
// nothing else.
func agentsMDMarkers(lines []string) (string, []int) {
	kinds := map[string]string{agentsMDBegin: "b", agentsMDEnd: "e"}
	var sequence strings.Builder
	var indexes []int
	for index, line := range lines {
		if kind, found := kinds[strings.TrimSpace(line)]; found {
			sequence.WriteString(kind)
			indexes = append(indexes, index)
		}
	}
	return sequence.String(), indexes
}

// blankLineBefore is what appending after content needs so one blank line
// separates the block from it.
func blankLineBefore(content string) string {
	switch {
	case content == "", strings.HasSuffix(content, "\n\n"):
		return ""
	case strings.HasSuffix(content, "\n"):
		return "\n"
	}
	return "\n\n"
}
