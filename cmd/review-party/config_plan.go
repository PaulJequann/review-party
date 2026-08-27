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
	return printPublishedConfigurationPlan(plan, options.format, streams.output, streams.errors)
}

func printInvalidConfigurationPlan(plan configuration.Plan, options configurationMutationOptions, streams commandIO) int {
	result := configurationPlanResult{
		Valid: false, Reason: plan.Reason(), Scopes: scopesAsStrings(plan.Scopes()),
		Paths: plan.Paths(), Changes: changesForPlan(plan),
	}
	if options.format == "json" {
		if err := writeJSON(streams.output, result); err != nil {
			return printConfigFailure(options.format, streams.output, streams.errors, err)
		}
		return 1
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
	if !isTerminalInput(streams.input) {
		return errors.New("configuration mutation requires --yes when stdin is not a terminal")
	}
	printHumanPlan(streams.output, plan)
	confirmed, err := confirmConfigurationPlan(streams.input, streams.output)
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("configuration change cancelled")
	}
	return nil
}

func printPublishedConfigurationPlan(plan configuration.Plan, format string, stdout, stderr io.Writer) int {
	result := configurationPlanResult{
		Valid: true, Published: true, Scopes: scopesAsStrings(plan.Scopes()),
		Paths: plan.Paths(), Changes: changesForPlan(plan),
	}
	if format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return printConfigFailure(format, stdout, stderr, err)
		}
		return 0
	}
	printHumanPlan(stdout, plan)
	fmt.Fprintln(stdout, "published")
	return 0
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

func printHumanPlan(output io.Writer, plan configuration.Plan) {
	fmt.Fprintln(output, "configuration plan:")
	for _, change := range plan.Changes() {
		before := "<absent>"
		if change.HadBefore {
			before = change.Before
		}
		after := "<absent>"
		if change.HadAfter {
			after = change.After
		}
		fmt.Fprintf(output, "  %s %s %q: %s -> %s\n", change.Scope, change.Field, change.Path, before, after)
	}
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

func isTerminalInput(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
}
