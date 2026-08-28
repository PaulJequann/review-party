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
		plan, err := manager.PlanProfileCreation(configuration.Repository(options.repository), configuration.ProfileDraft{
			Target: scope, Name: name, Reviewer: stringFlag(cmd, "reviewer"), Model: stringFlag(cmd, "model"),
			ReasoningEffort: stringFlag(cmd, "effort"), AttemptDeadline: stringFlag(cmd, "deadline"),
			Instructions: instructions, TemplateID: templateID,
		})
		if err != nil {
			return 0, err
		}
		if warning := manualModelWarning(modelWarningInput{
			manager: manager, discovery: discoveryService, repository: options.repository,
			reviewer: stringFlag(cmd, "reviewer"), model: stringFlag(cmd, "model"),
		}); warning != "" {
			options.warnings = append(options.warnings, warning)
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

func manualModelWarning(input modelWarningInput) string {
	if input.model == "" || input.discovery == nil {
		return ""
	}
	effective, err := input.manager.Resolve(configuration.Request{Repository: configuration.Repository(input.repository)})
	if err != nil {
		return "model choice could not be checked against configured choices: " + err.Error()
	}
	settings, found := effective.ReviewerPolicy(input.reviewer)
	if !found {
		return fmt.Sprintf("model %q was entered manually for unknown Reviewer %q; execution may be unavailable", input.model, input.reviewer)
	}
	configured := append([]string{settings.Model.Value}, settings.AllowedModels.Value...)
	packaged := []string{}
	if model := input.manager.PackagedReviewerModel(input.reviewer); model != "" {
		packaged = append(packaged, model)
	}
	service := input.discovery()
	if service == nil || service.IsKnownModel(input.reviewer, input.model, configured, packaged) {
		return ""
	}
	return fmt.Sprintf("model %q was not reported by cached, configured, or packaged choices for Reviewer %q; confirm it explicitly", input.model, input.reviewer)
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
