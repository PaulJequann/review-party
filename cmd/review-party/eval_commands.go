package main

import (
	"github.com/spf13/cobra"
)

func newEvalCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{Use: "eval", Short: "Run, inspect, adjudicate, and compare Reviewer evaluations", Args: cobra.NoArgs, RunE: showCommandHelp}
	cmd.AddCommand(newEvalRunCommand(streams), newEvalInspectCommand(streams), newEvalAdjudicationCommand(streams), newEvalScoreCommand(streams), newEvalCompareCommand(streams))
	return cmd
}

func newEvalRunCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "run SUITE", Short: "Run an Eval Suite with one Experiment Configuration",
		Example: "  review-party eval run global:general-bugs --reviewer opencode --model MODEL --format json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := evalRunOptions{
				suite: args[0], experimentPath: stringFlag(cmd, "experiment"), profile: stringFlag(cmd, "profile"), reviewer: stringFlag(cmd, "reviewer"),
				model: stringFlag(cmd, "model"), effort: stringFlag(cmd, "effort"), deadline: durationFlag(cmd, "deadline"),
				attempts: intFlag(cmd, "attempts"), concurrency: intFlag(cmd, "concurrency"), format: stringFlag(cmd, "format"),
				configuration: stringFlag(cmd, "config"), overrides: changedFlags(cmd),
			}
			return commandResult(executeEvalSuite(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	cmd.Flags().String("experiment", "", "Named Experiment Configuration JSON")
	cmd.Flags().String("profile", "", "Review Profile override")
	addCommonSelectionFlags(cmd)
	cmd.Flags().Duration("deadline", 0, "Execution deadline")
	cmd.Flags().Int("attempts", 0, "Maximum attempts per Eval Case")
	cmd.Flags().Int("concurrency", 0, "Maximum active Eval Cases")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newEvalInspectCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "inspect EVAL_ID", Short: "Inspect an Eval Run, Suite Run, or adjudication revision", Example: "  review-party eval inspect esr_... --format json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := evalInspectOptions{id: args[0], format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config")}
			return commandResult(executeEvalInspect(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newEvalAdjudicationCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{Use: "adjudication", Short: "Export Eval adjudication decisions", Args: cobra.NoArgs, RunE: showCommandHelp}
	export := &cobra.Command{
		Use: "export EVAL_SUITE_RUN_ID", Short: "Export an adjudication document for editing",
		Example: "  review-party eval adjudication export esr_... > decisions.json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := evalAdjudicationOptions{id: args[0], configuration: stringFlag(cmd, "config")}
			return commandResult(executeEvalAdjudication(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	addConfigurationFlag(export)
	cmd.AddCommand(export)
	return cmd
}

func newEvalScoreCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "score EVAL_SUITE_RUN_ID", Short: "Publish and score an edited adjudication document",
		Example: "  review-party eval score esr_... --adjudication decisions.json --format json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := evalScoreOptions{id: args[0], adjudicationPath: stringFlag(cmd, "adjudication"), format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config")}
			return commandResult(executeEvalScore(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	cmd.Flags().String("adjudication", "", "Adjudication JSON document")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newEvalCompareCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "compare", Short: "Compare two adjudicated Eval experiments",
		Example: "  review-party eval compare --baseline ar_... --candidate ar_... --format json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := evalCompareOptions{baseline: stringFlag(cmd, "baseline"), candidate: stringFlag(cmd, "candidate"), format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config")}
			return commandResult(executeEvalCompare(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	cmd.Flags().String("baseline", "", "Baseline adjudication revision id")
	cmd.Flags().String("candidate", "", "Candidate adjudication revision id")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}
