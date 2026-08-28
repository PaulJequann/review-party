package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"reviewparty/internal/discovery"
)

type discoveryReport struct {
	Results []discovery.Result `json:"results"`
}

func newConfigDiscoveryCommand(streams commandIO, dependencies configurationDependencies) *cobra.Command {
	cmd := newConfigLeafCommand("discover [REVIEWER]", "Observe available Reviewer models without starting authentication", cobra.MaximumNArgs(1), func(cmd *cobra.Command, args []string) error {
		reviewer := ""
		if len(args) == 1 {
			reviewer = args[0]
		}
		return commandResult(executeConfigurationDiscovery(cmd.Context(), reviewer, stringFlag(cmd, "format"), streams, dependencies.discoveryService))
	})
	addFormatFlag(cmd)
	return cmd
}

func executeConfigurationDiscovery(ctx context.Context, reviewer, format string, streams commandIO, discoveryService func() *discovery.Service) int {
	if err := validateConfigurationFormat(format); err != nil {
		return printConfigFailure(format, streams.output, streams.errors, err)
	}
	if discoveryService == nil {
		discoveryService = discovery.NewDefaultService
	}
	service := discoveryService()
	if service == nil {
		return printConfigFailure(format, streams.output, streams.errors, fmt.Errorf("discovery service is unavailable"))
	}
	if reviewer != "" {
		if !containsString(service.Reviewers(), reviewer) {
			return printConfigFailure(format, streams.output, streams.errors, fmt.Errorf("unknown Reviewer %q; expected %v", reviewer, service.Reviewers()))
		}
		results := service.DiscoverMany(ctx, []string{reviewer})
		flushDiscoveryCache(ctx, service)
		return printDiscoveryResults(discoveryOutput{format: format, stdout: streams.output, stderr: streams.errors, singleReviewer: true}, results)
	}
	results := service.DiscoverMany(ctx, nil)
	flushDiscoveryCache(ctx, service)
	return printDiscoveryResults(discoveryOutput{format: format, stdout: streams.output, stderr: streams.errors}, results)
}

func flushDiscoveryCache(ctx context.Context, service *discovery.Service) {
	flushContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_ = service.Flush(flushContext)
}

type discoveryOutput struct {
	format         string
	stdout         io.Writer
	stderr         io.Writer
	singleReviewer bool
}

func printDiscoveryResults(output discoveryOutput, results []discovery.Result) int {
	if output.format == "json" {
		return printDiscoveryJSON(output, results)
	}
	for index, result := range results {
		if index > 0 {
			fmt.Fprintln(output.stdout)
		}
		printHumanDiscovery(output.stdout, result)
	}
	return 0
}

func printDiscoveryJSON(output discoveryOutput, results []discovery.Result) int {
	if output.singleReviewer && len(results) == 1 {
		if err := writeJSON(output.stdout, results[0]); err != nil {
			return printConfigFailure(output.format, output.stdout, output.stderr, err)
		}
		return 0
	}
	if err := writeJSON(output.stdout, discoveryReport{Results: results}); err != nil {
		return printConfigFailure(output.format, output.stdout, output.stderr, err)
	}
	return 0
}

func printHumanDiscovery(output io.Writer, result discovery.Result) {
	fmt.Fprintf(output, "reviewer: %s\nstatus: %s\n", result.Reviewer, result.Status)
	printHarnessVersion(output, result)
	fmt.Fprintf(output, "authentication: %s\n", result.Authentication.Status)
	printDiscoveryModels(output, result.Models)
	printSignInAction(output, result.Authentication.SignIn)
	printDiscoveryDiagnostic(output, result.Diagnostic)
}

func printHarnessVersion(output io.Writer, result discovery.Result) {
	if result.HarnessVersion != "" {
		fmt.Fprintf(output, "harness version: %s\n", result.HarnessVersion)
	}
}

func printDiscoveryModels(output io.Writer, models []discovery.Model) {
	for _, model := range models {
		defaultMarker := ""
		if model.Default {
			defaultMarker = " (default)"
		}
		fmt.Fprintf(output, "  model: %s%s\n", model.ID, defaultMarker)
	}
}

func printSignInAction(output io.Writer, signIn *discovery.SignInAction) {
	if signIn == nil {
		return
	}
	fmt.Fprintf(output, "sign in: %s\n", joinCommand(signIn.Command))
	if signIn.DocumentationURL != "" {
		fmt.Fprintf(output, "  documentation: %s\n", signIn.DocumentationURL)
	}
}

func printDiscoveryDiagnostic(output io.Writer, diagnostic string) {
	if diagnostic != "" {
		fmt.Fprintf(output, "diagnostic: %s\n", diagnostic)
	}
}

func joinCommand(arguments []string) string {
	return strings.Join(arguments, " ")
}

func containsString(values []string, wanted string) bool {
	return sort.SearchStrings(values, wanted) < len(values) && values[sort.SearchStrings(values, wanted)] == wanted
}
