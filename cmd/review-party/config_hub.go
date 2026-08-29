package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mattn/go-isatty"

	"reviewparty/internal/configuration"
	"reviewparty/internal/configurationhub" //nolint:depguard // Cobra is the terminal composition root for the dedicated Hub adapter.
)

type configurationHubOptions struct {
	repository    string
	configuration string
	accessible    bool
}

func executeConfigurationHub(parent context.Context, options configurationHubOptions, streams commandIO) int {
	if !isTerminalInput(streams.input) || !isTerminalOutput(streams.output) {
		return printConfigFailure("human", streams.output, streams.errors, fmt.Errorf("the Configuration Hub requires a terminal; use explicit 'review-party config' subcommands for automation"))
	}
	repository, err := filepath.Abs(options.repository)
	if err != nil {
		return printFailure(streams.errors, fmt.Errorf("resolve repository: %w", err))
	}
	manager := streams.configurationManager(options.configuration)
	ctx, stop := configurationHubContext(parent)
	defer stop()
	hubOptions := configurationhub.RunOptions{
		Context:    ctx,
		Repository: configuration.Repository(repository),
		Input:      configurationHubInput(streams.input), Output: streams.output, Accessible: options.accessible,
	}
	if err := configurationhub.Run(manager, hubOptions); err != nil {
		return printConfigFailure("human", streams.output, streams.errors, fmt.Errorf("run Configuration Hub: %w", err))
	}
	return 0
}

func configurationHubInput(input io.Reader) io.ReadCloser {
	if readCloser, ok := input.(io.ReadCloser); ok {
		return readCloser
	}
	return io.NopCloser(input)
}

func isTerminalOutput(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && (isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd()))
}

func configurationHubContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}
