package main

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
)

func executeConfigReviewsAdd(cmd *cobra.Command, streams commandIO) int {
	return executeConfigReviewsMutation(cmd, streams, func(selection configuration.ReviewSelection, scope configuration.Scope) (configuration.ReviewSelection, error) {
		item, err := selectionItemFromCommand(cmd, scope)
		if err != nil {
			return configuration.ReviewSelection{}, err
		}
		return configuration.AddReviewSelection(selection, scope, item).Selection, nil
	})
}

type reviewSelectionOperation uint8

const (
	reviewSelectionRemove reviewSelectionOperation = iota
	reviewSelectionMove
)

func executeConfigReviewsSelection(cmd *cobra.Command, streams commandIO, operation reviewSelectionOperation) int {
	return executeConfigReviewsMutation(cmd, streams, func(selection configuration.ReviewSelection, scope configuration.Scope) (configuration.ReviewSelection, error) {
		switch operation {
		case reviewSelectionRemove:
			index, err := removalIndex(cmd, selection, scope)
			if err != nil {
				return configuration.ReviewSelection{}, err
			}
			intent, err := configuration.RemoveReviewSelection(selection, scope, index)
			if err != nil {
				return configuration.ReviewSelection{}, err
			}
			return intent.Selection, nil
		case reviewSelectionMove:
			from, to := intFlag(cmd, "from"), intFlag(cmd, "to")
			if from < 0 || to < 0 {
				return configuration.ReviewSelection{}, errors.New("--from and --to must be non-negative")
			}
			intent, err := configuration.MoveReviewSelection(selection, scope, from, to)
			if err != nil {
				return configuration.ReviewSelection{}, err
			}
			return intent.Selection, nil
		default:
			return configuration.ReviewSelection{}, fmt.Errorf("unknown review selection operation %d", operation)
		}
	})
}

func executeConfigReviewsMutation(cmd *cobra.Command, streams commandIO, transform func(configuration.ReviewSelection, configuration.Scope) (configuration.ReviewSelection, error)) int {
	options, err := reviewsMutationOptionsFromCommand(cmd)
	if err != nil {
		return printConfigFailure(stringFlag(cmd, "format"), streams.output, streams.errors, err)
	}
	return mutateReviewSelection(options, streams, func(selection configuration.ReviewSelection) (configuration.ReviewSelection, error) {
		return transform(selection, options.scope)
	})
}

func executeConfigReviewsConcurrency(value string, cmd *cobra.Command, streams commandIO) int {
	format := stringFlag(cmd, "format")
	options := configurationMutationOptions{
		repository: stringFlag(cmd, "repo"), format: format,
		configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes"),
	}
	return mutateReviewSelection(options, streams, func(selection configuration.ReviewSelection) (configuration.ReviewSelection, error) {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return configuration.ReviewSelection{}, fmt.Errorf("Concurrency Limit %q is not an integer", value)
		}
		selection.ConcurrencyLimit = limit
		return selection, nil
	})
}

func mutateReviewSelection(options configurationMutationOptions, streams commandIO, transform func(configuration.ReviewSelection) (configuration.ReviewSelection, error)) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		selection, err := currentReviewSelection(manager, configuration.Repository(options.repository))
		if err != nil {
			return 0, err
		}
		selection, err = transform(selection)
		if err != nil {
			return 0, err
		}
		return publishReviewSelection(manager, selection, options, streams), nil
	})
}

func publishReviewSelection(manager *configuration.Manager, selection configuration.ReviewSelection, options configurationMutationOptions, streams commandIO) int {
	plan, err := manager.Plan(configuration.Repository(options.repository), []configuration.Intent{configuration.SetReviewSelection{Selection: selection}})
	if err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	return publishConfigurationPlan(manager, plan, options, streams)
}

func reviewsMutationOptionsFromCommand(cmd *cobra.Command) (configurationMutationOptions, error) {
	format := stringFlag(cmd, "format")
	configurationPath := stringFlag(cmd, "config")
	mutation := configurationMutationOptions{
		repository: stringFlag(cmd, "repo"), format: format, configuration: configurationPath, yes: boolFlag(cmd, "yes"),
	}
	scope, err := parseConfigurationScope(stringFlag(cmd, "scope"))
	if err != nil {
		return configurationMutationOptions{format: format}, err
	}
	mutation.scope = scope
	return mutation, nil
}

func currentReviewSelection(manager *configuration.Manager, repository configuration.Repository) (configuration.ReviewSelection, error) {
	selection, value, err := manager.EffectiveReviewSelection(repository)
	if err != nil {
		return configuration.ReviewSelection{}, err
	}
	if !value.Authored {
		selection = configuration.DefaultReviewSelection()
	}
	return selection, nil
}

func selectionItemFromCommand(cmd *cobra.Command, scope configuration.Scope) (configuration.SelectionItem, error) {
	profile, party := stringFlag(cmd, "profile"), stringFlag(cmd, "party")
	return parseSelectionItem(scope, profile, party)
}

func parseSelectionItem(scope configuration.Scope, profile, party string) (configuration.SelectionItem, error) {
	if (profile == "") == (party == "") {
		return configuration.SelectionItem{}, errors.New("choose exactly one of --profile or --party")
	}
	value := profile
	if value == "" {
		value = party
	}
	qualified, name := configuration.ParseScopedReference(value)
	if qualified != "" && qualified != scope {
		return configuration.SelectionItem{}, fmt.Errorf("%s selection must reference a %s definition", value, scope)
	}
	if profile != "" {
		return configuration.SelectionItem{Profile: name}, nil
	}
	return configuration.SelectionItem{Party: name}, nil
}

func removalIndex(cmd *cobra.Command, selection configuration.ReviewSelection, scope configuration.Scope) (int, error) {
	index := intFlag(cmd, "index")
	profile, party := stringFlag(cmd, "profile"), stringFlag(cmd, "party")
	if index < 0 {
		return selectionIndex(selection, scope, profile, party)
	}
	if profile != "" || party != "" {
		return 0, errors.New("--index cannot be combined with --profile or --party")
	}
	return index, nil
}

func selectionIndex(selection configuration.ReviewSelection, scope configuration.Scope, profile, party string) (int, error) {
	item, err := parseSelectionItem(scope, profile, party)
	if err != nil {
		return 0, err
	}
	for index, candidate := range selectionItems(selection, scope) {
		if candidate == item {
			return index, nil
		}
	}
	name, err := item.Name()
	if err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("review selection does not contain %s %q", scope, name)
}

func selectionItems(selection configuration.ReviewSelection, scope configuration.Scope) []configuration.SelectionItem {
	if scope == configuration.ScopeGlobal {
		return selection.Global
	}
	return selection.Repository
}
