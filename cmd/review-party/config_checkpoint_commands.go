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
			Requirement:      configuration.CheckpointRequirement(stringFlag(cmd, "requirement")),
			ExemptPaths:      stringArrayFlag(cmd, "exempt"),
			SmallChangeLines: intFlag(cmd, "small-change-lines"),
			Waivers:          configuration.WaiverPolicy(stringFlag(cmd, "waivers")),
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
segments, and a pattern without / matches the file name at any depth.`
	set.Example = "  review-party config checkpoint set pre-push --exempt '*.md' --exempt 'docs/**' --integration git\n  review-party config checkpoint set pre-commit --small-change-lines 10 --waivers anyone --yes"
	set.ValidArgs = checkpointNameArguments()
	set.Flags().String("requirement", string(configuration.RequirementReviewed), "What the Checkpoint expects: reviewed")
	set.Flags().StringArray("exempt", nil, "Path pattern the Checkpoint does not require Reviews for; repeatable")
	set.Flags().Int("small-change-lines", 0, "Pass changes of at most this many added plus deleted lines; 0 disables")
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
