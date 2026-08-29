package main

import (
	"io"

	"reviewparty/internal/engine"
)

type initOptions struct {
	repository     string
	stateDirectory string
	configuration  string
}

func executeInit(options initOptions, stdout, stderr io.Writer) int {
	result, err := engine.InitializeReviewParty(engine.ReviewPartyInitialization{
		Repository:              options.repository,
		StateDirectory:          options.stateDirectory,
		UserConfigurationPath:   options.configuration,
		UseDefaultConfiguration: options.configuration == defaultUserConfigurationPath(),
	})
	if err != nil {
		return printFailure(stderr, err)
	}
	if err := printInitialization(stdout, result); err != nil {
		return printFailure(stderr, err)
	}
	return 0
}

func printInitialization(output io.Writer, result engine.ReviewPartyInitializationResult) error {
	return writeCommandOutput(output, func(output *commandOutput) {
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
