package main

import (
	"fmt"
	"io"

	"reviewparty/internal/engine"
)

type profileScopeOptions struct {
	repository string
	global     bool
}

type profileCreateOptions struct {
	profileScopeOptions
	name     string
	blank    bool
	packaged string
}

func executeProfileInstallDefaults(options profileScopeOptions, stdout, stderr io.Writer) int {
	if options.global && options.repository != "." {
		fmt.Fprintln(stderr, "review-party: profile install-defaults accepts one scope: --repo PATH or --global")
		return usageExitCode
	}
	result, err := engine.InitializeProfiles(engine.ProfileInitialization{Repository: options.repository, Global: options.global})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Installed %d owned starter Profiles in %s; kept %d existing files.\n", len(result.Created), result.Directory, len(result.Existing))
	fmt.Fprintln(stdout, "Owned Profiles shadow packaged updates. Default Profile selection was not changed.")
	return 0
}

func executeProfileCreate(options profileCreateOptions, stdout, stderr io.Writer) int {
	if options.global && options.repository != "." {
		fmt.Fprintln(stderr, "review-party: profile create requires NAME, exactly one of --blank or --from-packaged PROFILE, and one scope")
		return usageExitCode
	}
	result, err := engine.CreateProfile(engine.ProfileCreation{Name: options.name, Repository: options.repository, Global: options.global, Blank: options.blank, PackagedProfile: options.packaged})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	path := result.Created
	if len(path) == 0 {
		fmt.Fprintf(stderr, "review-party: Profile already exists at %s; refusing to overwrite owned material\n", result.Existing[0])
		return 1
	}
	fmt.Fprintf(stdout, "Created owned Profile: %s\n", path[0])
	fmt.Fprintln(stdout, "Owned Profiles shadow packaged updates until you remove or edit the owned file.")
	return 0
}

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
	fmt.Fprintf(output, "Next: review-party review bugs --repo %s\n", result.Repository)
}
