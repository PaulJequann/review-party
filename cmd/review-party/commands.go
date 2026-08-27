package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"reviewparty/internal/engine"
)

const usageExitCode = 2

type commandIO struct {
	input  io.Reader
	output io.Writer
	errors io.Writer
}

type commandExitError struct {
	code int
}

func (err commandExitError) Error() string { return "command failed" }

func execute(ctx context.Context, arguments []string, streams commandIO) int {
	root := newRootCommand(streams)
	root.SetArgs(arguments)
	if err := root.ExecuteContext(ctx); err != nil {
		var exitErr commandExitError
		if errors.As(err, &exitErr) {
			return exitErr.code
		}
		fmt.Fprintf(streams.errors, "review-party: %v\n", err)
		return usageExitCode
	}
	return 0
}

func newRootCommand(streams commandIO) *cobra.Command {
	root := &cobra.Command{
		Use:           "review-party",
		Short:         "Run bounded code reviews through coding agents",
		Long:          "Review Party compiles named review profiles into bounded agent runs and stores validated review records.",
		Example:       "  review-party review bugs --repo .\n  review-party config\n  review-party history --format json",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetIn(streams.input)
	root.SetOut(streams.output)
	root.SetErr(streams.errors)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return err })
	root.AddCommand(
		newReviewCommand(streams),
		newReplayCommand(streams),
		newInspectCommand(streams),
		newHistoryCommand(streams),
		newProfilesCommand(streams),
		newExplainCommand("explain", streams),
		newProfileCommand(streams),
		newPartiesCommand(streams),
		newPartyCommand(streams),
		newEvalCommand(streams),
		newConfigCommand(streams),
		newInitCommand(streams),
		newVersionCommand(streams),
	)
	return root
}

func commandResult(code int) error {
	if code == 0 {
		return nil
	}
	return commandExitError{code: code}
}

func stringFlag(cmd *cobra.Command, name string) string {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(fmt.Sprintf("read registered string flag %q: %v", name, err))
	}
	return value
}

func intFlag(cmd *cobra.Command, name string) int {
	value, err := cmd.Flags().GetInt(name)
	if err != nil {
		panic(fmt.Sprintf("read registered integer flag %q: %v", name, err))
	}
	return value
}

func boolFlag(cmd *cobra.Command, name string) bool {
	value, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(fmt.Sprintf("read registered boolean flag %q: %v", name, err))
	}
	return value
}

func durationFlag(cmd *cobra.Command, name string) time.Duration {
	value, err := cmd.Flags().GetDuration(name)
	if err != nil {
		panic(fmt.Sprintf("read registered duration flag %q: %v", name, err))
	}
	return value
}

func changedFlags(cmd *cobra.Command) map[string]bool {
	changed := map[string]bool{}
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		changed[flag.Name] = true
	})
	return changed
}

func addCommonSelectionFlags(cmd *cobra.Command) {
	cmd.Flags().String("reviewer", "", "Reviewer adapter: "+strings.Join(engine.SupportedReviewers(), ", "))
	cmd.Flags().String("model", "", "Explicit model for the selected Reviewer")
	cmd.Flags().String("effort", "", "Explicit reasoning effort")
	_ = cmd.RegisterFlagCompletionFunc("reviewer", completeReviewers)
}

func addRepositoryFlag(cmd *cobra.Command, usage string) {
	cmd.Flags().String("repo", ".", usage)
}

func addConfigurationFlag(cmd *cobra.Command) {
	cmd.Flags().String("config", defaultUserConfigurationPath(), "Global Configuration path")
}

func addFormatFlag(cmd *cobra.Command) {
	cmd.Flags().String("format", "human", "Output format: human or json")
}

func addSubjectFlags(cmd *cobra.Command) {
	cmd.Flags().String("base", "", "Committed-range base revision")
	cmd.Flags().String("head", "", "Committed-range head revision")
}

func addReviewFlags(cmd *cobra.Command) {
	addRepositoryFlag(cmd, "Git repository to review")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	addSubjectFlags(cmd)
}

func completeReviewers(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	matches := make([]string, 0, len(engine.SupportedReviewers()))
	for _, reviewer := range engine.SupportedReviewers() {
		if strings.HasPrefix(reviewer, prefix) {
			matches = append(matches, reviewer)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}
