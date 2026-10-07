package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
)

func checkpointNameArguments() []string {
	names := configuration.CheckpointNames()
	arguments := make([]string, len(names))
	for index, name := range names {
		arguments[index] = string(name)
	}
	return arguments
}

func newConfigCheckpointCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{Use: "checkpoint", Short: "Declare the Review Checkpoints this repository expects", Args: cobra.NoArgs, RunE: showCommandHelp}
	set := newConfigLeafCommand("set <pre-push|pre-commit>", "Create or replace one Review Checkpoint", cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs), func(cmd *cobra.Command, args []string) error {
		checkpoint := configuration.Checkpoint{
			Requirement:     configuration.CheckpointRequirement(stringFlag(cmd, "requirement")),
			ExemptPaths:     stringArrayFlag(cmd, "exempt"),
			UnreviewedLines: intFlag(cmd, "unreviewed-lines"),
			ReviewBudget:    intFlag(cmd, "review-budget"),
			Waivers:         configuration.WaiverPolicy(stringFlag(cmd, "waivers")),
		}
		for _, integration := range stringArrayFlag(cmd, "integration") {
			checkpoint.Integrations = append(checkpoint.Integrations, configuration.IntegrationName(integration))
		}
		intent := configuration.SetCheckpoint{Name: configuration.CheckpointName(args[0]), Checkpoint: checkpoint}
		return commandResult(publishCheckpointIntent(cmd, streams, intent))
	})
	set.Long = `Create or replace one Review Checkpoint in Repository Configuration.

Omitted flags take their defaults, so set replaces the whole declaration.
--exempt takes slash-separated patterns relative to the repository root: * and
? match within one path segment, ** as a whole segment matches any number of
segments, and a pattern without / matches the file name at any depth.
--requirement judged also needs a current verdict, recorded with review-party
finding record, on every Finding of the Reviews that cover the change.
--unreviewed-lines lets that many lines change after a Review without a new
one. --review-budget caps the Reviews one change may spend before the
Checkpoint asks a person to step in; declare it on one Checkpoint.`
	set.Example = "  review-party config checkpoint set pre-push --exempt '*.md' --exempt 'docs/**' --integration git\n  review-party config checkpoint set pre-push --requirement judged --yes\n  review-party config checkpoint set pre-commit --unreviewed-lines 10 --review-budget 2 --waivers anyone --yes"
	set.ValidArgs = checkpointNameArguments()
	set.Flags().String("requirement", string(configuration.RequirementReviewed), "What the Checkpoint expects: reviewed, or judged for a verdict on every Finding too")
	set.Flags().StringArray("exempt", nil, "Path pattern the Checkpoint does not require Reviews for; repeatable")
	set.Flags().Int("unreviewed-lines", configuration.DefaultUnreviewedLines, "Added plus deleted lines that may follow a Review without a new one; 0 allows none")
	set.Flags().Int("review-budget", configuration.DefaultReviewBudget, fmt.Sprintf("Reviews one change may spend before a person must step in, 1 to %d", configuration.MaxReviewBudget))
	set.Flags().String("waivers", string(configuration.WaiversHuman), "Who may waive the Checkpoint: anyone, human, or none")
	set.Flags().StringArray("integration", nil, "Integration the team installs for this Checkpoint: git, claude-code, codex, or agents-md; repeatable")
	addConfigMutationFlags(set, false, false, "")

	remove := newConfigLeafCommand("remove <pre-push|pre-commit>", "Remove one Review Checkpoint", cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs), func(cmd *cobra.Command, args []string) error {
		return commandResult(publishCheckpointIntent(cmd, streams, configuration.RemoveCheckpoint{Name: configuration.CheckpointName(args[0])}))
	})
	remove.ValidArgs = checkpointNameArguments()
	addConfigMutationFlags(remove, false, false, "")
	cmd.AddCommand(set, remove)
	return cmd
}

func stringArrayFlag(cmd *cobra.Command, name string) []string {
	values, err := cmd.Flags().GetStringArray(name)
	if err != nil {
		panic(fmt.Sprintf("read registered string array flag %q: %v", name, err))
	}
	return values
}

func publishCheckpointIntent(cmd *cobra.Command, streams commandIO, intent configuration.Intent) int {
	options := configurationMutationOptions{
		repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"),
		configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes"),
	}
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		plan, err := manager.Plan(configuration.Repository(options.repository), []configuration.Intent{intent})
		if err != nil {
			return 0, err
		}
		return publishConfigurationPlan(manager, plan, options, streams), nil
	})
}
