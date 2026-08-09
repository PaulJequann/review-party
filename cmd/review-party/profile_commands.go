package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"reviewparty"
)

func runProfile(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] != "explain" {
		fmt.Fprintln(stderr, "review-party: profile requires: explain PROFILE")
		return 2
	}
	return runExplain(ctx, arguments[1:], stdout, stderr)
}

func runInit(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository to initialize")
	global := flags.Bool("global", false, "Initialize the user-wide profile library")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: init accepts no positional arguments")
		return 2
	}
	result, err := reviewparty.InitializeProfiles(reviewparty.ProfileInitialization{
		Repository: *repository,
		Global:     *global,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	printInitialization(stdout, result)
	return 0
}

func printInitialization(output io.Writer, result reviewparty.ProfileInitializationResult) {
	fmt.Fprintf(output, "profile library %s\n", result.Directory)
	for _, path := range result.Created {
		fmt.Fprintf(output, "created  %s\n", path)
	}
	for _, path := range result.Existing {
		fmt.Fprintf(output, "kept     %s\n", path)
	}
}
