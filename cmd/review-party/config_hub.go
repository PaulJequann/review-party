package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"reviewparty/internal/configuration"
	"reviewparty/internal/configurationhub" //nolint:depguard // Cobra is the terminal composition root for the dedicated Hub adapter.
	"reviewparty/internal/discovery"
	"reviewparty/internal/subject"
)

type configurationHubOptions struct {
	repository       string
	repositoryFlag   bool
	configuration    string
	accessible       bool
	discoveryService func() *discovery.Service
}

func executeConfigurationHub(parent context.Context, options configurationHubOptions, streams commandIO) int {
	if !streams.interactive() {
		return printConfigFailure("human", streams.output, streams.errors, fmt.Errorf("the Configuration Hub requires a terminal; use explicit 'review-party config' subcommands for automation"))
	}
	repository, err := resolveConfigurationHubRepository(options.repository, options.repositoryFlag)
	if err != nil {
		return printFailure(streams.errors, fmt.Errorf("resolve repository: %w", err))
	}
	options.repository = repository
	manager := streams.configurationManager(options.configuration)
	ctx, stop := configurationHubContext(parent)
	defer stop()
	if err := configurationhub.Run(manager, options.runOptions(ctx, manager, streams)); err != nil {
		return printConfigFailure("human", streams.output, streams.errors, fmt.Errorf("run Configuration Hub: %w", err))
	}
	return 0
}

// runOptions wires the Hub's forms for a resolved repository, or for none, to
// the terminal, Profile receipts, model discovery, and the
// undeclared-Integrations check a Checkpoints publish reports. Integrations
// are installed per repository, so without one there is nothing to check. The
// Hub and the init first-use journey share it so both drive the same forms.
func (options configurationHubOptions) runOptions(ctx context.Context, manager *configuration.Manager, streams commandIO) configurationhub.RunOptions {
	repository := options.repository
	hubOptions := configurationhub.RunOptions{
		Context:    ctx,
		Repository: configuration.Repository(repository),
		Input:      configurationHubInput(streams.input), Output: streams.output, Accessible: options.accessible,
		Receipts: profileReceiptProvider(ctx, options.configuration),
	}
	if repository != "" {
		hubOptions.UndeclaredIntegrations = func() (string, error) {
			_, command, err := undeclaredIntegrations(repository, options.configuration, manager)
			if command == "" {
				return "", err
			}
			return undeclaredIntegrationsNotice(command), nil
		}
	}
	if options.discoveryService != nil {
		hubOptions.Discovery = options.discoveryService()
		hubOptions.ModelChoiceCheck = func(reviewer, model string) configuration.ModelChoiceCheck {
			return modelChoiceCheck(modelWarningInput{
				manager: manager, discovery: options.discoveryService, repository: repository,
				reviewer: reviewer, model: model,
			})
		}
	}
	return hubOptions
}

// resolveConfigurationHubRepository returns "" when a launch without --repo is
// outside every git repository: the Hub then opens on Global Configuration
// only. An explicit --repo must name a repository.
func resolveConfigurationHubRepository(repository string, explicit bool) (string, error) {
	root, err := subject.ResolveRepositoryRoot(repository)
	if !explicit && errors.Is(err, subject.ErrNotRepository) {
		return "", nil
	}
	return root, err
}

func configurationHubInput(input io.Reader) io.ReadCloser {
	if readCloser, ok := input.(io.ReadCloser); ok {
		return readCloser
	}
	return io.NopCloser(input)
}

func configurationHubContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return signalContext(parent)
}
