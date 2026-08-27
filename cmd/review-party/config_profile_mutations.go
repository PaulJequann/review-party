package main

import (
	"errors"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
)

func executeConfigProfileCreate(name string, cmd *cobra.Command, options configurationMutationOptions, streams commandIO) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		scope, err := parseConfigurationScope(stringFlag(cmd, "scope"))
		if err != nil {
			return 0, err
		}
		templateID := stringFlag(cmd, "template")
		blank := boolFlag(cmd, "blank")
		if (templateID == "") != blank {
			return 0, errors.New("choose exactly one of --template or --blank")
		}
		instructions, err := instructionFlags(cmd)
		if err != nil {
			return 0, err
		}
		plan, err := manager.PlanProfileCreation(configuration.Repository(options.repository), configuration.ProfileDraft{
			Target: scope, Name: name, Reviewer: stringFlag(cmd, "reviewer"), Model: stringFlag(cmd, "model"),
			ReasoningEffort: stringFlag(cmd, "effort"), AttemptDeadline: stringFlag(cmd, "deadline"),
			Instructions: instructions, TemplateID: templateID,
		})
		if err != nil {
			return 0, err
		}
		return publishConfigurationPlan(manager, plan, options, streams), nil
	})
}

func executeConfigProfileCopy(value, targetValue string, options configurationMutationOptions, streams commandIO) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		target, err := parseConfigurationScope(targetValue)
		if err != nil {
			return 0, err
		}
		repository := configuration.Repository(options.repository)
		plan, err := manager.PlanProfileCopyFromReference(repository, value, target)
		if err != nil {
			return 0, err
		}
		return publishConfigurationPlan(manager, plan, options, streams), nil
	})
}

func executeConfigPartyCreate(name string, cmd *cobra.Command, options configurationMutationOptions, streams commandIO) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		scope, err := parseConfigurationScope(stringFlag(cmd, "scope"))
		if err != nil {
			return 0, err
		}
		profiles, err := parseProfileReferences(cmd)
		if err != nil {
			return 0, err
		}
		plan, err := manager.PlanPartyCreation(configuration.Repository(options.repository), configuration.PartyDraft{
			Target: scope, Name: name, Description: stringFlag(cmd, "description"),
			ConcurrencyLimit: intFlag(cmd, "concurrency-limit"), Profiles: profiles,
		})
		if err != nil {
			return 0, err
		}
		return publishConfigurationPlan(manager, plan, options, streams), nil
	})
}

func parseProfileReferences(cmd *cobra.Command) ([]configuration.ProfileReference, error) {
	values, err := cmd.Flags().GetStringArray("profile")
	if err != nil {
		return nil, err
	}
	defaultScope, err := parseConfigurationScope(stringFlag(cmd, "scope"))
	if err != nil {
		return nil, err
	}
	return configuration.ParseProfileReferences(defaultScope, values)
}
