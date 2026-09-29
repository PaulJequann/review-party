package main

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func newReplayCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "replay REVIEW_ID",
		Short:   "Replay a recorded committed Review",
		Example: "  review-party replay rp_...",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := replayOptions{
				id: model.ReviewID(args[0]), format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"), full: boolFlag(cmd, "full"),
			}
			return commandResult(executeReplay(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	addFormatFlag(cmd)
	addFullFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newInspectCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "inspect REVIEW_OR_BUNDLE_ID",
		Short:   "Inspect a Review Record or Review Bundle",
		Example: "  review-party inspect rp_... --format json\n  review-party inspect rb_...",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := inspectOptions{id: model.ReviewID(args[0]), format: stringFlag(cmd, "format"), verifyArtifacts: boolFlag(cmd, "verify-artifacts"), full: boolFlag(cmd, "full"), configuration: stringFlag(cmd, "config")}
			return commandResult(executeInspect(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	addFormatFlag(cmd)
	addFullFlag(cmd)
	addConfigurationFlag(cmd)
	cmd.Flags().Bool("verify-artifacts", false, "Verify referenced artifact files")
	return cmd
}

func newHistoryCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "history",
		Short:   "Query recorded Reviews",
		Example: "  review-party history --reviewer opencode --profile bugs\n  review-party history --format json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			query := store.HistoryQuery{
				Repository: stringFlag(cmd, "repo"), Reviewer: stringFlag(cmd, "reviewer"), Profile: stringFlag(cmd, "profile"),
				Lifecycle: model.Lifecycle(stringFlag(cmd, "lifecycle")), Termination: model.TerminationCategory(stringFlag(cmd, "termination")),
				Subject: stringFlag(cmd, "subject"), Limit: intFlag(cmd, "limit"),
			}
			options := historyOptions{query: query, format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"), sinceText: stringFlag(cmd, "since"), summary: boolFlag(cmd, "summary")}
			return commandResult(executeHistory(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	cmd.Flags().String("repo", "", "Git repository identity to match")
	cmd.Flags().String("reviewer", "", "Recorded Reviewer to match")
	cmd.Flags().String("profile", "", "Recorded Profile to match")
	cmd.Flags().String("lifecycle", "", "Lifecycle to match")
	cmd.Flags().String("termination", "", "Termination category to match")
	cmd.Flags().String("subject", "", "Subject identity to match")
	cmd.Flags().String("since", "", "Include Reviews at or after this RFC3339 timestamp")
	cmd.Flags().Int("limit", 20, "Maximum Reviews to show")
	cmd.Flags().Bool("summary", false, "Show per-Profile execution receipts instead of individual Reviews")
	return cmd
}

func newProfilesCommand(streams commandIO) *cobra.Command {
	return newLibraryListCommand(profileListSpec(), streams)
}

func newExplainCommand(use string, streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: use + " PROFILE", Short: "Explain a compiled Review Profile without launching a Reviewer",
		Example: "  review-party " + use + " bugs", Args: cobra.ExactArgs(1),
		ValidArgsFunction: completeProfileNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			options := explainOptions{
				profile: args[0], format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"), repository: stringFlag(cmd, "repo"),
			}
			return commandResult(executeExplain(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	addRepositoryFlag(cmd, "Git repository whose Profile should be explained")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newProfileCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Inspect one saved Review Profile", Args: cobra.NoArgs, RunE: showCommandHelp}
	cmd.AddCommand(newExplainCommand("explain", streams))
	return cmd
}

func newPartiesCommand(streams commandIO) *cobra.Command {
	return newLibraryListCommand(partyListSpec(), streams)
}

type libraryListOptions struct {
	repository    string
	format        string
	configuration string
}

type libraryListSpec struct {
	use     string
	label   string
	execute func(context.Context, libraryListOptions, commandIO) int
}

func profileListSpec() libraryListSpec {
	return libraryListSpec{use: "profiles", label: "Review Profiles", execute: func(ctx context.Context, options libraryListOptions, streams commandIO) int {
		return executeProfiles(ctx, profilesOptions{format: options.format, repository: options.repository, configuration: options.configuration}, streams.output, streams.errors)
	}}
}

func partyListSpec() libraryListSpec {
	return libraryListSpec{use: "parties", label: "Review Parties", execute: func(ctx context.Context, options libraryListOptions, streams commandIO) int {
		return executeParties(ctx, partiesOptions(options), streams.output, streams.errors)
	}}
}

func newLibraryListCommand(spec libraryListSpec, streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: spec.use, Short: "List available " + spec.label, Example: "  review-party " + spec.use + " --repo . --format json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := libraryListOptions{repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config")}
			return commandResult(spec.execute(cmd.Context(), options, streams))
		},
	}
	addRepositoryFlag(cmd, "Git repository whose "+spec.label+" should be listed")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func newConfigCommand(streams commandIO) *cobra.Command {
	dependencies := defaultConfigurationDependencies()
	cmd := &cobra.Command{Use: "config", Short: "Open the Configuration Hub or use explicit configuration commands", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		options := configurationHubOptions{
			repository: stringFlag(cmd, "repo"), configuration: stringFlag(cmd, "config"), accessible: boolFlag(cmd, "accessible"),
			discoveryService: dependencies.discoveryService,
		}
		return commandResult(executeConfigurationHub(cmd.Context(), options, streams))
	}}
	addRepositoryFlag(cmd, "Repository whose configuration should be managed")
	addConfigurationFlag(cmd)
	cmd.Flags().Bool("accessible", false, "Use the non-redrawing accessible Hub")
	path := &cobra.Command{Use: "path", Short: "Print the Global Configuration path", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return printConfigurationPath(stringFlag(cmd, "config"), streams.output)
	}}
	file := &cobra.Command{Use: "file", Short: "Inspect authored Configuration files", Args: cobra.NoArgs, RunE: showCommandHelp}
	addConfigurationFlag(path)
	file.AddCommand(newConfigFileShowCommand(streams))
	cmd.AddCommand(path, file, newConfigShowCommand(streams), newConfigValidateCommand(streams), newConfigDiscoveryCommand(streams, dependencies), newConfigProfileCommand(streams, dependencies), newConfigPartyCommand(streams), newConfigReviewsCommand(streams))
	return cmd
}

func newInitCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "init", Short: "Prepare managed state for a repository", Example: "  review-party init --repo .", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := initOptions{repository: stringFlag(cmd, "repo"), stateDirectory: stringFlag(cmd, "state-dir"), configuration: stringFlag(cmd, "config"), backup: boolFlag(cmd, "backup-incompatible"), fresh: boolFlag(cmd, "fresh"), yes: boolFlag(cmd, "yes")}
			return commandResult(executeInit(options, streams.output, streams.errors))
		},
	}
	addRepositoryFlag(cmd, "Git repository to initialize")
	cmd.Flags().String("state-dir", "", "Advanced per-user state location")
	cmd.Flags().Bool("backup-incompatible", false, "Back up an incompatible ledger and SQLite sidecars without initializing")
	cmd.Flags().Bool("fresh", false, "Initialize fresh state after a separately confirmed backup")
	cmd.Flags().Bool("yes", false, "Confirm this recovery step")
	cmd.MarkFlagsMutuallyExclusive("backup-incompatible", "fresh")
	addConfigurationFlag(cmd)
	return cmd
}

func showCommandHelp(cmd *cobra.Command, _ []string) error { return cmd.Help() }

type libraryNameLoader func(context.Context, *engine.Conductor, string) ([]string, error)

var completeProfileNames = completeLibraryNames(loadProfileNames)
var completePartyNames = completeLibraryNames(loadPartyNames)

func completeLibraryNames(load libraryNameLoader) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		conductor, err := engine.New(engine.Config{UserConfigurationPath: stringFlag(cmd, "config")})
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		names, err := load(cmd.Context(), conductor, stringFlag(cmd, "repo"))
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		matches := make([]string, 0, len(names))
		for _, name := range names {
			if strings.HasPrefix(name, prefix) {
				matches = append(matches, name)
			}
		}
		return matches, cobra.ShellCompDirectiveNoFileComp
	}
}

func loadProfileNames(ctx context.Context, conductor *engine.Conductor, repository string) ([]string, error) {
	profiles, err := conductor.ProfilesForRepository(ctx, repository)
	if err != nil {
		return nil, err
	}
	return collectNames(profiles, func(profile model.ProfileSummary) string { return profile.Name }), nil
}

func loadPartyNames(_ context.Context, conductor *engine.Conductor, repository string) ([]string, error) {
	parties, err := conductor.PartiesForRepository(repository)
	if err != nil {
		return nil, err
	}
	return collectNames(parties, func(party model.PartySummary) string { return party.Name }), nil
}

func collectNames[T any](items []T, name func(T) string) []string {
	names := make([]string, len(items))
	for index, item := range items {
		names[index] = name(item)
	}
	return names
}
