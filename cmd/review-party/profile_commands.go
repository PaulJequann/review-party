package main

import (
	"errors"
	"io"

	"reviewparty/internal/engine"
)

type initOptions struct {
	repository     string
	stateDirectory string
	configuration  string
	backup         bool
	fresh          bool
	yes            bool
}

func executeInit(options initOptions, stdout, stderr io.Writer) int {
	if (options.backup || options.fresh) && !options.yes {
		return printFailure(stderr, errors.New("state recovery requires --yes for this confirmation step"))
	}
	result, err := engine.InitializeReviewParty(engine.ReviewPartyInitialization{
		Repository:              options.repository,
		StateDirectory:          options.stateDirectory,
		UserConfigurationPath:   options.configuration,
		UseDefaultConfiguration: options.configuration == defaultUserConfigurationPath(),
		BackupIncompatible:      options.backup, Fresh: options.fresh,
	})
	if err != nil {
		return printFailure(stderr, err)
	}
	if err := printInitialization(stdout, result, options); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

func printInitialization(output io.Writer, result engine.ReviewPartyInitializationResult, options initOptions) error {
	return writeCommandOutput(output, func(output *commandOutput) {
		if result.Backup != nil {
			output.write("Incompatible state was backed up without migration to %s.\n", result.Backup.Directory)
			output.write("Next: separately confirm fresh state with %s.\n", freshInitializationCommand(result.Repository, options))
			return
		}
		output.write("Review Party is ready for %s.\n", result.Repository)
		if result.AdvancedState {
			output.write("Advanced state location: %s\n", result.StateDirectory)
		} else {
			output.write("State is managed automatically.\n")
		}
		if result.AlreadyReady {
			output.write("Existing state was kept unchanged.\n")
		}
		output.write("Next: configure a saved Review Profile for %s.\n", result.Repository)
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
