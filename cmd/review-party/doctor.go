package main

import (
	"fmt"

	"reviewparty/internal/configuration"

	"github.com/spf13/cobra"
)

type doctorOptions struct {
	repository    string
	format        string
	configuration string
}

type doctorResult struct {
	Valid         bool                            `json:"valid"`
	Validation    configurationValidationResult   `json:"validation"`
	TemplateDrift []configuration.TemplateDrift   `json:"template_drift"`
	Skipped       []configuration.SkippedTemplate `json:"skipped_templates"`
}

func newDoctorCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "Check configuration health and Template drift", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		options := doctorOptions{repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config")}
		return commandResult(executeDoctor(options, streams))
	}}
	addRepositoryFlag(cmd, "Repository whose configuration should be checked")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func executeDoctor(options doctorOptions, streams commandIO) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		validation, validationErr := validateConfiguration(manager, "", configuration.Repository(options.repository))
		drift, driftErr := manager.TemplateDrift(configuration.Repository(options.repository))
		if driftErr != nil {
			return 0, driftErr
		}
		result := doctorResult{Valid: validationErr == nil, Validation: validation, TemplateDrift: drift, Skipped: manager.SkippedTemplates()}
		result.Validation.Valid = validationErr == nil
		if validationErr != nil {
			result.Validation.Error = validationErr.Error()
		}
		if options.format == "json" {
			if err := writeJSON(streams.output, result); err != nil {
				return 0, err
			}
			if !result.Valid {
				return 1, nil
			}
			return 0, nil
		}
		if validationErr != nil {
			return printFailure(streams.errors, validationErr), nil
		}
		return printConfigOutput(streams, result.writeText), nil
	})
}

func (result doctorResult) writeText(output *commandOutput) {
	output.write("configuration is valid\n")
	for _, item := range result.TemplateDrift {
		output.write("%s\n", doctorTemplateDriftLine(item))
	}
	for _, item := range result.Skipped {
		output.write("Template skipped: %s: %s\n", item.TemplateID, item.Reason)
	}
}

func doctorTemplateDriftLine(item configuration.TemplateDrift) string {
	if item.Status == configuration.TemplateSourceUnavailable {
		return fmt.Sprintf("Template source unavailable: %s:%s %s@%s; saved instructions still run", item.Scope, item.Profile, item.TemplateID, item.TemplateRevision)
	}
	return fmt.Sprintf("Template update: %s:%s %s → %s", item.Scope, item.Profile, item.TemplateRevision, item.AvailableRevision)
}
