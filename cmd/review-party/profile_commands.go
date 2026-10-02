package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"reviewparty/internal/configuration"
	"reviewparty/internal/configurationhub" //nolint:depguard // Cobra is the terminal composition root for the dedicated Hub adapter.
	"reviewparty/internal/discovery"
	"reviewparty/internal/engine"
)

type initOptions struct {
	repository       string
	stateDirectory   string
	configuration    string
	backup           bool
	fresh            bool
	yes              bool
	accessible       bool
	setup            []setupTarget
	discoveryService func() *discovery.Service
}

// setupTarget is one --profile or --party name, kept in command-line order.
type setupTarget struct {
	kind  configuration.AuthoredItemKind
	value string
}

// setupTargetFlag appends every occurrence of one repeatable setup flag to a
// list the --profile and --party flags share, so their interleaved order is
// the order they join the selection.
type setupTargetFlag struct {
	kind    configuration.AuthoredItemKind
	targets *[]setupTarget
}

func (flag setupTargetFlag) String() string { return "" }

func (flag setupTargetFlag) Set(value string) error {
	*flag.targets = append(*flag.targets, setupTarget{kind: flag.kind, value: value})
	return nil
}

func (flag setupTargetFlag) Type() string { return "name" }

func executeInit(ctx context.Context, options initOptions, streams commandIO) int {
	if !initRecoveryConfirmed(options) {
		return printFailure(streams.errors, errors.New("state recovery requires --yes for this confirmation step"))
	}
	result, err := engine.InitializeReviewParty(engine.ReviewPartyInitialization{
		Repository:              options.repository,
		StateDirectory:          options.stateDirectory,
		UserConfigurationPath:   options.configuration,
		UseDefaultConfiguration: options.configuration == defaultUserConfigurationPath(),
		BackupIncompatible:      options.backup, Fresh: options.fresh,
	})
	if err != nil {
		return printFailure(streams.errors, err)
	}
	if err := printStateRecovery(streams.output, result, options); err != nil || result.Backup != nil {
		return failureCode(streams.errors, err)
	}
	manager := streams.configurationManager(options.configuration)
	repository := configuration.Repository(result.Repository)
	switch {
	case len(options.setup) > 0:
		return applyInitSetup(manager, repository, options, streams)
	case streams.interactive():
		hub := configurationHubOptions{
			repository: result.Repository, configuration: options.configuration,
			accessible: options.accessible, discoveryService: options.discoveryService,
		}
		if err := runFirstUseJourney(ctx, manager, hub, streams); err != nil {
			return printFailure(streams.errors, err)
		}
	}
	return failureCode(streams.errors, reportInitGaps(streams.output, manager, repository, options))
}

func failureCode(output io.Writer, err error) int {
	if err != nil {
		return printFailure(output, err)
	}
	return 0
}

func initRecoveryConfirmed(options initOptions) bool {
	if !options.backup && !options.fresh {
		return true
	}
	return options.yes
}

// printStateRecovery reports state preparation only when it recovered from
// incompatible state; ordinary preparation is silent.
func printStateRecovery(output io.Writer, result engine.ReviewPartyInitializationResult, options initOptions) error {
	return writeCommandOutput(output, func(output *commandOutput) {
		if result.Backup != nil {
			output.write("Incompatible state was backed up without migration to %s.\n", result.Backup.Directory)
			output.write("Next: separately confirm fresh state with %s.\n", freshInitializationCommand(result.Repository, options))
			return
		}
		if options.fresh {
			output.write("Fresh state is ready for %s.\n", result.Repository)
		}
	})
}

func freshInitializationCommand(repository string, options initOptions) string {
	command := "review-party init --fresh --yes --repo " + shellQuoteArgument(repository)
	if options.stateDirectory != "" {
		command += " --state-dir " + shellQuoteArgument(options.stateDirectory)
	}
	if options.configuration != "" {
		command += " --config " + shellQuoteArgument(options.configuration)
	}
	return command
}

// applyInitSetup adds each named Profile or Party to the selection group of
// the scope it resolves to, Repository before Global, through one Plan.
// Names the selection already holds change nothing.
func applyInitSetup(manager *configuration.Manager, repository configuration.Repository, options initOptions, streams commandIO) int {
	selection, err := currentReviewSelection(manager, repository)
	if err != nil {
		return printFailure(streams.errors, err)
	}
	for _, target := range options.setup {
		group, item, err := manager.ResolveSelectionTarget(repository, target.kind, target.value)
		if err != nil {
			return printFailure(streams.errors, err)
		}
		selection = configuration.AddReviewSelection(selection, group, item).Selection
	}
	plan, err := manager.Plan(repository, []configuration.Intent{configuration.SetReviewSelection{Selection: selection}})
	if err != nil {
		return printFailure(streams.errors, err)
	}
	if plan.Valid() && len(plan.Changes()) == 0 {
		return printCommandOutput(streams.output, streams.errors, func(output *commandOutput) {
			output.write("The Review selection already includes every name given; nothing was written.\n")
		})
	}
	return publishConfigurationPlan(manager, plan, configurationMutationOptions{
		repository: string(repository), configuration: options.configuration, yes: options.yes, format: "human",
	}, streams)
}

func runFirstUseJourney(parent context.Context, manager *configuration.Manager, hub configurationHubOptions, streams commandIO) error {
	ctx, stop := configurationHubContext(parent)
	defer stop()
	options := hub.runOptions(ctx, manager, streams)
	options.InstallCheckpointHooks = func(integration configuration.IntegrationName, personal bool, confirm func() (bool, error)) error {
		target := hookInstallTarget{root: hub.repository, configuration: hub.configuration, integration: integration, personal: personal}
		return installCheckpointHooks(target, manager, streams.output, confirm)
	}
	for _, agent := range agentIntegrations {
		if _, err := exec.LookPath(agent.binary); err == nil {
			options.AgentsOnPath = append(options.AgentsOnPath, agent.integration)
		}
	}
	if err := configurationhub.RunFirstUse(manager, options); err != nil {
		return fmt.Errorf("run first-use journey: %w", err)
	}
	return nil
}
