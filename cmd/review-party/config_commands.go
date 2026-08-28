package main

import (
	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
	"reviewparty/internal/discovery"
)

type configurationDependencies struct {
	discoveryService func() *discovery.Service
}

func defaultConfigurationDependencies() configurationDependencies {
	return configurationDependencies{discoveryService: discovery.NewDefaultService}
}

func newConfigLeafCommand(use, short string, args cobra.PositionalArgs, run func(*cobra.Command, []string) error) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: args, RunE: run}
}

func configurationCommandRun[T any](options func(*cobra.Command) T, execute func(T, commandIO) int, streams commandIO) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		return commandResult(execute(options(cmd), streams))
	}
}

func configurationFileOptionsFromCommand(cmd *cobra.Command) configurationFileOptions {
	return configurationFileOptions{
		scope: stringFlag(cmd, "scope"), repository: stringFlag(cmd, "repo"),
		format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"),
	}
}

func configurationShowOptionsFromCommand(cmd *cobra.Command) configurationFileOptions {
	return configurationFileOptions{
		repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"),
	}
}

func configurationValidationOptionsFromCommand(cmd *cobra.Command) configurationValidationOptions {
	return configurationValidationOptions{
		scope: stringFlag(cmd, "scope"), repository: stringFlag(cmd, "repo"),
		format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"),
	}
}

func newConfigShowCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("show", "Show effective configuration and the Reviews that will run", cobra.NoArgs,
		configurationCommandRun(configurationShowOptionsFromCommand, executeConfigurationShow, streams))
	addRepositoryFlag(cmd, "Repository whose effective configuration should be shown")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newConfigFileShowCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("show", "Show one authored configuration document", cobra.NoArgs,
		configurationCommandRun(configurationFileOptionsFromCommand, executeConfigurationFileShow, streams))
	cmd.Flags().String("scope", string(configuration.ScopeGlobal), "Configuration scope: global or repository")
	addRepositoryFlag(cmd, "Repository whose authored Configuration should be shown")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newConfigValidateCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("validate", "Validate authored configuration", cobra.NoArgs,
		configurationCommandRun(configurationValidationOptionsFromCommand, executeConfigurationValidate, streams))
	cmd.Flags().String("scope", "", "Validate one scope: global or repository; default validates both")
	addRepositoryFlag(cmd, "Repository whose Configuration should be validated")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newConfigProfileCommand(streams commandIO, dependencies configurationDependencies) *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Create or copy Review Profiles", Args: cobra.NoArgs, RunE: showCommandHelp}
	cmd.AddCommand(newConfigProfileCreateCommand(streams, dependencies), newConfigProfileCopyCommand(streams))
	return cmd
}

func newConfigProfileCreateCommand(streams commandIO, dependencies configurationDependencies) *cobra.Command {
	cmd := newConfigLeafCommand("create NAME", "Create a complete executable Review Profile", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) error {
		options := configurationMutationOptions{
			repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"),
			configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes"),
		}
		return commandResult(executeConfigProfileCreate(args[0], cmd, options, streams, dependencies.discoveryService))
	})
	cmd.Flags().String("template", "", "Seed instructions from a packaged Review Profile Template")
	cmd.Flags().Bool("blank", false, "Create from supplied instructions without a Template")
	cmd.Flags().String("reviewer", "", "Reviewer identifier")
	cmd.Flags().String("model", "", "Model identifier")
	cmd.Flags().String("effort", "", "Reasoning effort")
	cmd.Flags().String("deadline", "", "Positive Attempt deadline, such as 3m")
	cmd.Flags().String("instructions", "", "Profile instructions")
	cmd.Flags().String("instructions-file", "", "Read Profile instructions from this file")
	addConfigMutationFlags(cmd, true, false, string(configuration.ScopeGlobal))
	cmd.MarkFlagsMutuallyExclusive("template", "blank")
	cmd.MarkFlagsMutuallyExclusive("instructions", "instructions-file")
	return cmd
}

func newConfigProfileCopyCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("copy NAME", "Copy one Profile into another Configuration scope", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) error {
		options := configurationMutationOptions{
			repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"),
			configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes"),
		}
		return commandResult(executeConfigProfileCopy(args[0], stringFlag(cmd, "target-scope"), options, streams))
	})
	cmd.Flags().String("target-scope", "", "Destination Configuration scope: global or repository")
	addConfigMutationFlags(cmd, false, false, "")
	return cmd
}

func newConfigPartyCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{Use: "party", Short: "Create flat Review Parties", Args: cobra.NoArgs, RunE: showCommandHelp}
	cmd.AddCommand(newConfigPartyCreateCommand(streams))
	return cmd
}

func newConfigPartyCreateCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("create NAME", "Create a flat Party of scoped Profile references", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) error {
		options := configurationMutationOptions{
			repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"),
			configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes"),
		}
		return commandResult(executeConfigPartyCreate(args[0], cmd, options, streams))
	})
	cmd.Flags().String("description", "", "Party description")
	cmd.Flags().StringArray("profile", nil, "Scoped Profile reference; repeat for each member")
	cmd.Flags().Int("concurrency-limit", 0, "Party Concurrency Limit")
	addConfigMutationFlags(cmd, true, false, string(configuration.ScopeGlobal))
	return cmd
}

func newConfigReviewsCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{Use: "reviews", Short: "Edit the repository's ordered Review selection", Args: cobra.NoArgs, RunE: showCommandHelp}
	cmd.AddCommand(
		newConfigReviewsAddCommand(streams),
		newConfigReviewsRemoveCommand(streams),
		newConfigReviewsMoveCommand(streams),
		newConfigReviewsConcurrencyCommand(streams),
	)
	return cmd
}

func newConfigReviewsAddCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("add", "Append one Profile or Party to the Review selection", cobra.NoArgs, func(cmd *cobra.Command, _ []string) error {
		return commandResult(executeConfigReviewsAdd(cmd, streams))
	})
	addConfigMutationFlags(cmd, true, true, "")
	return cmd
}

func newConfigReviewsRemoveCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("remove", "Remove one item from the Review selection", cobra.NoArgs, func(cmd *cobra.Command, _ []string) error {
		return commandResult(executeConfigReviewsSelection(cmd, streams, reviewSelectionRemove))
	})
	cmd.Flags().Int("index", -1, "Zero-based item index in the selected scope")
	addConfigMutationFlags(cmd, true, true, "")
	return cmd
}

func newConfigReviewsMoveCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("move", "Move one item within a Review selection scope", cobra.NoArgs, func(cmd *cobra.Command, _ []string) error {
		return commandResult(executeConfigReviewsSelection(cmd, streams, reviewSelectionMove))
	})
	cmd.Flags().Int("from", -1, "Current zero-based item index")
	cmd.Flags().Int("to", -1, "Destination zero-based item index")
	addConfigMutationFlags(cmd, true, false, "")
	return cmd
}

func newConfigReviewsConcurrencyCommand(streams commandIO) *cobra.Command {
	cmd := newConfigLeafCommand("set-concurrency N", "Set the repository Review selection's Concurrency Limit", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) error {
		return commandResult(executeConfigReviewsConcurrency(args[0], cmd, streams))
	})
	addConfigMutationFlags(cmd, false, false, "")
	return cmd
}
