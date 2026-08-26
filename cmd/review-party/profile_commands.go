package main

import (
	"fmt"
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
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	printInitialization(stdout, result)
	return 0
}

func printInitialization(output io.Writer, result engine.ReviewPartyInitializationResult) {
	fmt.Fprintf(output, "Review Party is ready for %s.\n", result.Repository)
	if result.AdvancedState {
		fmt.Fprintf(output, "Advanced state location: %s\n", result.StateDirectory)
	} else {
		fmt.Fprintln(output, "State is managed automatically.")
	}
	if result.AlreadyReady {
		fmt.Fprintln(output, "Existing state was kept unchanged.")
	}
	fmt.Fprintf(output, "Next: configure a saved Review Profile for %s.\n", result.Repository)
}
