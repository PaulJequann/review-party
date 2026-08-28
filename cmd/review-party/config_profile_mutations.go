package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
	"reviewparty/internal/discovery"
)

func executeConfigProfileCreate(name string, cmd *cobra.Command, options configurationMutationOptions, streams commandIO, discoveryService func() *discovery.Service) int {
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
		draft := configuration.ProfileDraft{
			Target: scope, Name: name, Reviewer: stringFlag(cmd, "reviewer"), Model: stringFlag(cmd, "model"),
			ReasoningEffort: stringFlag(cmd, "effort"), AttemptDeadline: stringFlag(cmd, "deadline"),
			Instructions: instructions, TemplateID: templateID,
		}
		check, warning := modelChoiceCheck(modelWarningInput{
			manager: manager, discovery: discoveryService, repository: options.repository,
			reviewer: draft.Reviewer, model: draft.Model,
		})
		draft.ModelChoiceCheck = check
		if warning != "" {
			options.warnings = append(options.warnings, warning)
		}
		plan, err := manager.PlanProfileCreation(configuration.Repository(options.repository), draft)
		if err != nil {
			return 0, err
		}
		return publishConfigurationPlan(manager, plan, options, streams), nil
	})
}

type modelWarningInput struct {
	manager    *configuration.Manager
	discovery  func() *discovery.Service
	repository string
	reviewer   string
	model      string
}

func modelChoiceCheck(input modelWarningInput) (configuration.ModelChoiceCheck, string) {
	if input.model == "" || input.discovery == nil {
		return configuration.ModelChoiceCheck{}, ""
	}
	effective, err := input.manager.Resolve(configuration.Request{Repository: configuration.Repository(input.repository)})
	if err != nil {
		return configuration.ModelChoiceCheck{}, "model choice could not be checked against configured choices: " + err.Error()
	}
	settings, found := effective.ReviewerPolicy(input.reviewer)
	if !found {
		return configuration.ModelChoiceCheck{}, fmt.Sprintf("model %q was entered manually for unknown Reviewer %q; execution may be unavailable", input.model, input.reviewer)
	}
	configured := append([]string{settings.Model.Value}, settings.AllowedModels.Value...)
	packaged := []string{}
	if model := input.manager.PackagedReviewerModel(input.reviewer); model != "" {
		packaged = append(packaged, model)
	}
	service := input.discovery()
	if service == nil {
		return configuration.ModelChoiceCheck{}, ""
	}
	choices := service.ChoiceSnapshot(discovery.ChoiceRequest{
		Reviewer: input.reviewer, Configured: configured, Packaged: packaged,
	})
	modelChoices := choices.Choices()
	ids := make([]string, len(modelChoices))
	for index, choice := range modelChoices {
		ids[index] = choice.Model.ID
	}
	return configuration.ModelChoiceCheck{Checked: true, Choices: ids}, ""
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
	profiles := make([]configuration.ProfileReference, 0, len(values))
	for _, value := range values {
		scope, name := configuration.ParseScopedReference(value)
		if scope == "" {
			scope = defaultScope
		}
		profiles = append(profiles, configuration.ProfileReference{Scope: scope, Profile: name})
	}
	return profiles, nil
}
