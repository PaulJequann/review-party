package main

import (
	"context"
	"errors"
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
		return commandResult(executeConfigurationDiscovery(cmd.Context(), reviewer, stringFlag(cmd, "format"), boolFlag(cmd, "refresh"), streams, dependencies.discoveryService))
	})
	addFormatFlag(cmd)
	cmd.Flags().Bool("refresh", false, "Discard any cached result for the observed Reviewer before discovery")
	return cmd
}

func executeConfigurationDiscovery(ctx context.Context, reviewer, format string, refresh bool, streams commandIO, discoveryService func() *discovery.Service) int {
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
	if refresh {
		if err := forgetDiscoveryCache(service, reviewer); err != nil {
			return printConfigFailure(format, streams.output, streams.errors, err)
		}
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

// forgetDiscoveryCache discards cached results before a refresh. Without a
// Reviewer every known Reviewer entry is discarded; failures are joined so
// one unreadable entry does not hide the others.
func forgetDiscoveryCache(service *discovery.Service, reviewer string) error {
	reviewers := service.Reviewers()
	if reviewer != "" {
		if !containsString(reviewers, reviewer) {
			return fmt.Errorf("unknown Reviewer %q; expected %v", reviewer, reviewers)
		}
		reviewers = []string{reviewer}
	}
	var failures []error
	for _, known := range reviewers {
		if _, err := service.ForgetCached(known); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func flushDiscoveryCache(ctx context.Context, service *discovery.Service) {
	flushContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := service.Flush(flushContext); err != nil {
		return
	}
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
	if err := writeCommandOutput(output.stdout, func(writer *commandOutput) {
		for index, result := range results {
			if index > 0 {
				writer.write("\n")
			}
			writeHumanDiscovery(writer, result)
		}
	}); err != nil {
		return printConfigFailure(output.format, output.stdout, output.stderr, err)
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

func writeHumanDiscovery(output *commandOutput, result discovery.Result) {
	output.write("reviewer: %s\nstatus: %s\n", result.Reviewer, result.Status)
	if result.HarnessVersion != "" {
		output.write("harness version: %s\n", result.HarnessVersion)
	}
	output.write("authentication: %s\n", result.Authentication.Status)
	for _, model := range result.Models {
		defaultMarker := ""
		if model.Default {
			defaultMarker = " (default)"
		}
		output.write("  model: %s%s\n", model.ID, defaultMarker)
	}
	if signIn := result.Authentication.SignIn; signIn != nil {
		output.write("sign in: %s\n", joinCommand(signIn.Command))
		if signIn.DocumentationURL != "" {
			output.write("  documentation: %s\n", signIn.DocumentationURL)
		}
	}
	if result.Diagnostic != "" {
		output.write("diagnostic: %s\n", result.Diagnostic)
	}
}

func joinCommand(arguments []string) string {
	return strings.Join(arguments, " ")
}

func containsString(values []string, wanted string) bool {
	return sort.SearchStrings(values, wanted) < len(values) && values[sort.SearchStrings(values, wanted)] == wanted
}
