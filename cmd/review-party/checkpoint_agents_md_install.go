package main

// The agents-md Integration keeps one marked block in the repository's agent
// instructions that tells a Caller Agent to check each declared Checkpoint
// before a hook would refuse. The block is generated from the declared
// Checkpoint names alone, so only declaring or removing a Checkpoint leaves it
// stale, and the installer then replaces only the lines between its markers.

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
		Short: "Write the AGENTS.md block that tells agents to check every declared Checkpoint",
		Long: `Write a marked block into AGENTS.md at the repository root. It has one line per
declared Checkpoint telling the agent to run review-party checkpoint check
before the action the Checkpoint guards and to follow what the check prints. A
repository with CLAUDE.md and no AGENTS.md gets the block in CLAUDE.md; one
with neither gets a new AGENTS.md.

A missing block is appended after one blank line. The block depends only on
which Checkpoints are declared, so changing a Checkpoint's settings leaves it
in place; declaring or removing one makes it stale, and the installer replaces
the lines between its markers and nothing outside them. Markers that are
unbalanced or repeated are left for you to fix by hand. Rerunning is safe.`,
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
	outcome, updated, manual := placeAgentsMDBlock(string(content), found, agentsMDBody(names))
	if updated != string(content) {
		plan.writes[plan.location] = []byte(updated)
	}
	for _, name := range names {
		plan.steps = append(plan.steps, hookInstallStep{checkpoint: name, path: plan.location, outcome: outcome, manual: manual})
	}
	return plan, nil
}

// agentsMDLines tells the agent, per Checkpoint, to run the check before the
// action the Checkpoint guards. The check carries the declaration's details
// and prints the next command, so the lines do not repeat them.
var agentsMDLines = map[configuration.CheckpointName]string{
	configuration.CheckpointPreCommit: "Before committing, stage the change and run `review-party checkpoint check pre-commit`; when it refuses, run the next command it prints and check again.\n",
	configuration.CheckpointPrePush:   "Before pushing, run `review-party checkpoint check pre-push`; when it refuses, run the next command it prints and check again.\n",
}

// agentsMDBody is the text between the markers: one line per declared
// Checkpoint, then where to read more.
func agentsMDBody(names []configuration.CheckpointName) string {
	var body strings.Builder
	for _, name := range names {
		body.WriteString(agentsMDLines[name])
	}
	body.WriteString("`review-party checkpoint --help` has details.\n")
	return body.String()
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
			sequence.problem(),
			"Delete the extra or misplaced marker lines, or both markers and the lines between them, then rerun the install.",
		}
	}
	if strings.Join(lines[at[0]+1:at[1]], "") == body {
		return hookInstalled, content, nil
	}
	return hookBlockStale, strings.Join(lines[:at[0]+1], "") + body + strings.Join(lines[at[1]:], ""), nil
}

type agentsMDSequence string

func agentsMDMarkers(lines []string) (agentsMDSequence, []int) {
	kinds := map[string]string{agentsMDBegin: "b", agentsMDEnd: "e"}
	var sequence strings.Builder
	var indexes []int
	for index, line := range lines {
		if kind, found := kinds[strings.TrimSpace(line)]; found {
			sequence.WriteString(kind)
			indexes = append(indexes, index)
		}
	}
	return agentsMDSequence(sequence.String()), indexes
}

func (sequence agentsMDSequence) problem() string {
	return fmt.Sprintf("Found %d %q and %d %q lines; the block needs one begin line before one end line.", strings.Count(string(sequence), "b"), agentsMDBegin, strings.Count(string(sequence), "e"), agentsMDEnd)
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
