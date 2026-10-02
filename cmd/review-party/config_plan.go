package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"

	"reviewparty/internal/configuration"
)

func publishConfigurationPlan(manager *configuration.Manager, plan configuration.Plan, options configurationMutationOptions, streams commandIO) int {
	if !plan.Valid() {
		return printInvalidConfigurationPlan(plan, options, streams)
	}
	if err := confirmConfigurationPlanIfNeeded(options, plan, streams); err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	if err := manager.Publish(plan); err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	return printPublishedConfigurationPlan(plan, options, streams)
}

func printInvalidConfigurationPlan(plan configuration.Plan, options configurationMutationOptions, streams commandIO) int {
	result := configurationPlanResultFor(plan, options, false)
	if options.format == "json" {
		return writeConfigurationPlanJSON(result, options.format, streams, 1)
	}
	return printConfigFailure(options.format, streams.output, streams.errors, fmt.Errorf("invalid configuration plan: %s", plan.Reason()))
}

func confirmConfigurationPlanIfNeeded(options configurationMutationOptions, plan configuration.Plan, streams commandIO) error {
	if options.yes {
		return nil
	}
	if options.format == "json" {
		return errors.New("JSON mutations require --yes")
	}
	if !streams.isTerminal(streams.input) {
		return errors.New("configuration mutation requires --yes when stdin is not a terminal")
	}
	if err := printHumanPlan(streams.output, plan); err != nil {
		return err
	}
	if err := printHumanWarnings(streams.output, warningsForPlan(plan)); err != nil {
		return err
	}
	confirmed, err := confirmConfigurationPlan(streams.input, streams.output)
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("configuration change cancelled")
	}
	return nil
}

func printPublishedConfigurationPlan(plan configuration.Plan, options configurationMutationOptions, streams commandIO) int {
	result := configurationPlanResultFor(plan, options, true)
	if options.format == "json" {
		return writeConfigurationPlanJSON(result, options.format, streams, 0)
	}
	if err := printHumanPlan(streams.output, plan); err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	if err := printHumanWarnings(streams.output, warningsForPlan(plan)); err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	if err := writeCommandOutput(streams.output, func(output *commandOutput) {
		output.write("published\n")
	}); err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	return 0
}

func writeConfigurationPlanJSON(result configurationPlanResult, format string, streams commandIO, exitCode int) int {
	if err := writeJSON(streams.output, result); err != nil {
		return printConfigFailure(format, streams.output, streams.errors, err)
	}
	return exitCode
}

func configurationPlanResultFor(plan configuration.Plan, options configurationMutationOptions, published bool) configurationPlanResult {
	return configurationPlanResult{
		Valid: published, Published: published, Reason: plan.Reason(),
		Warnings: warningsForPlan(plan), Scopes: scopesAsStrings(plan.Scopes()),
		Paths: plan.Paths(), Changes: changesForPlan(plan),
	}
}

func warningsForPlan(plan configuration.Plan) []string {
	return plan.Warnings()
}

func changesForPlan(plan configuration.Plan) []configurationChange {
	changes := plan.Changes()
	result := make([]configurationChange, len(changes))
	for index, change := range changes {
		result[index] = configurationChange{
			Field: change.Field, Scope: string(change.Scope), Path: change.Path,
			Before: change.Before, After: change.After, HadBefore: change.HadBefore, HadAfter: change.HadAfter,
		}
	}
	return result
}

func scopesAsStrings(scopes []configuration.Scope) []string {
	result := make([]string, len(scopes))
	for index, scope := range scopes {
		result[index] = string(scope)
	}
	return result
}

func printHumanPlan(output io.Writer, plan configuration.Plan) error {
	return configuration.RenderPlanHuman(output, plan)
}

func printHumanWarnings(output io.Writer, warnings []string) error {
	return configuration.RenderPlanWarningsHuman(output, warnings)
}

func confirmConfigurationPlan(input io.Reader, output io.Writer) (bool, error) {
	if _, err := io.WriteString(output, "Apply this configuration plan? [y/N] "); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

func isTerminalStream(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && (isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd()))
}
