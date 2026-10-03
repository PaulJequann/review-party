package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"reviewparty/internal/configuration"
	"reviewparty/internal/engine"
)

const usageExitCode = 2

type commandIO struct {
	input                io.Reader
	output               io.Writer
	errors               io.Writer
	configurationManager func(string) *configuration.Manager
	// terminal reports whether a stream is an interactive terminal. Nil uses
	// the real check; tests substitute it to reach terminal-only paths.
	terminal func(stream any) bool
}

func (streams commandIO) isTerminal(stream any) bool {
	if streams.terminal != nil {
		return streams.terminal(stream)
	}
	return isTerminalStream(stream)
}

// interactive reports whether both stdin and stdout reach a terminal, which
// prompting forms need.
func (streams commandIO) interactive() bool {
	return streams.isTerminal(streams.input) && streams.isTerminal(streams.output)
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
		return printCommandError(streams.errors, usageExitCode, err)
	}
	return 0
}

type commandOutput struct {
	writer io.Writer
	err    error
}

func writeCommandOutput(writer io.Writer, render func(*commandOutput)) error {
	output := &commandOutput{writer: writer}
	render(output)
	if output.err != nil {
		return fmt.Errorf("write command output: %w", output.err)
	}
	return nil
}

func printCommandOutput(stdout, stderr io.Writer, render func(*commandOutput)) int {
	if err := writeCommandOutput(stdout, render); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

func (output *commandOutput) write(format string, args ...any) {
	if output.err != nil {
		return
	}
	_, output.err = fmt.Fprintf(output.writer, format, args...)
}

func printCommandError(output io.Writer, code int, err error) int {
	if writeErr := writeCommandOutput(output, func(output *commandOutput) {
		output.write("review-party: %v\n", err)
	}); writeErr != nil {
		return 1
	}
	return code
}

func printFailure(output io.Writer, err error) int {
	return printCommandError(output, 1, err)
}

func newRootCommand(streams commandIO) *cobra.Command {
	if streams.configurationManager == nil {
		streams.configurationManager = func(globalConfigPath string) *configuration.Manager {
			options := engine.ReviewPartyConfigurationOptions()
			if globalConfigPath != "" {
				options.GlobalRoot = filepath.Dir(globalConfigPath)
				options.GlobalConfigPath = globalConfigPath
			}
			return configuration.NewManager(options)
		}
	}
	root := &cobra.Command{
		Use:           "review-party",
		Short:         "Run bounded code reviews through coding agents",
		Long:          "Review Party resolves one repository's review selection into bounded agent runs and stores validated review records.",
		Example:       "  review-party run --repo .\n  review-party run --profile code-quality --repo .\n  review-party config\n  review-party history --format json",
		Version:       humanVersion(currentRuntimeProvenance()),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("{{.Version}}")
	root.SetIn(streams.input)
	root.SetOut(streams.output)
	root.SetErr(streams.errors)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return err })
	root.AddCommand(
		newRunCommand(streams),
		newReplayCommand(streams),
		newInspectCommand(streams),
		newHistoryCommand(streams),
		newMissCommand(streams),
		newFindingCommand(streams),
		newStatusCommand(streams),
		newWaitCommand(streams),
		newCheckpointCommand(streams),
		newProfilesCommand(streams),
		newExplainCommand("explain", streams),
		newProfileCommand(streams),
		newPartiesCommand(streams),
		newEvalCommand(streams),
		newConfigCommand(streams),
		newInitCommand(streams), newDoctorCommand(streams),
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
	cmd.RegisterFlagCompletionFunc("reviewer", completeReviewers) //nolint:errcheck // Cobra completion registration is best-effort
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

func addFullFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("full", false, "Include the complete review record under record in JSON output; human output adds raw output, artifacts, changed paths, and patch")
}

func addSubjectFlags(cmd *cobra.Command) {
	cmd.Flags().String("base", "", "Committed-range base revision")
	cmd.Flags().String("head", "", "Committed-range head revision")
}

func addReviewFlags(cmd *cobra.Command) {
	addRepositoryFlag(cmd, "Git repository to review")
	addFormatFlag(cmd)
	addFullFlag(cmd)
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
