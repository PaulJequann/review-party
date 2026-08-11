package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"reviewparty/internal/engine"
)

func runProfile(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		fmt.Fprintln(stderr, "review-party: profile requires: explain PROFILE or create NAME")
		return 2
	}
	switch arguments[0] {
	case "explain":
		return runExplain(ctx, arguments[1:], stdout, stderr)
	case "create":
		return runProfileCreate(arguments[1:], stdout, stderr)
	case "install-defaults":
		return runProfileInstallDefaults(arguments[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, "review-party: profile requires: explain PROFILE or create NAME")
		return 2
	}
}

func runProfileInstallDefaults(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("profile install-defaults", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository that will own the Profiles")
	global := flags.Bool("global", false, "Install personal global Profiles")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: profile install-defaults accepts one scope: --repo PATH or --global")
		return 2
	}
	if *global && *repository != "." {
		fmt.Fprintln(stderr, "review-party: profile install-defaults accepts one scope: --repo PATH or --global")
		return 2
	}
	result, err := engine.InitializeProfiles(engine.ProfileInitialization{Repository: *repository, Global: *global})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Installed %d owned starter Profiles in %s; kept %d existing files.\n", len(result.Created), result.Directory, len(result.Existing))
	fmt.Fprintln(stdout, "Owned Profiles shadow packaged updates. Default Profile selection was not changed.")
	return 0
}

func runProfileCreate(arguments []string, stdout, stderr io.Writer) int {
	name, arguments := takeLeadingValue(arguments)
	flags := flag.NewFlagSet("profile create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository that will own the Profile")
	global := flags.Bool("global", false, "Create a personal global Profile")
	blank := flags.Bool("blank", false, "Start from a minimal blank Profile")
	packaged := flags.String("from-packaged", "", "Start from a packaged Profile")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if name == "" {
		fmt.Fprintln(stderr, "review-party: profile create requires NAME, exactly one of --blank or --from-packaged PROFILE, and one scope")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: profile create requires NAME, exactly one of --blank or --from-packaged PROFILE, and one scope")
		return 2
	}
	if *global && *repository != "." {
		fmt.Fprintln(stderr, "review-party: profile create requires NAME, exactly one of --blank or --from-packaged PROFILE, and one scope")
		return 2
	}
	result, err := engine.CreateProfile(engine.ProfileCreation{Name: name, Repository: *repository, Global: *global, Blank: *blank, PackagedProfile: *packaged})
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

func runInit(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository to initialize")
	stateDirectory := flags.String("state-dir", "", "Advanced per-user state location")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: init accepts no positional arguments")
		return 2
	}
	result, err := engine.InitializeReviewParty(engine.ReviewPartyInitialization{
		Repository:              *repository,
		StateDirectory:          *stateDirectory,
		UserConfigurationPath:   *configuration,
		UseDefaultConfiguration: *configuration == defaultUserConfigurationPath(),
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
