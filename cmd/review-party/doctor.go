package main

import (
	"context"
	"fmt"

	"reviewparty/internal/configuration"
	"reviewparty/internal/hostrun"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"

	"github.com/spf13/cobra"
)

type doctorOptions struct {
	repository    string
	format        string
	configuration string
}

type doctorResult struct {
	Valid              bool                            `json:"valid"`
	Validation         configurationValidationResult   `json:"validation"`
	TemplateDrift      []configuration.TemplateDrift   `json:"template_drift"`
	Skipped            []configuration.SkippedTemplate `json:"skipped_templates"`
	UnresolvedNames    []unresolvedName                `json:"unresolved_names"`
	IntegrationGaps    []floorGap                      `json:"integration_gaps"`
	ExemptionConflicts []exemptionConflict             `json:"exemption_conflicts"`
	Undeclared         []undeclaredIntegration         `json:"undeclared_integrations"`
	RecentWaivers      []model.CheckpointWaiver        `json:"recent_waivers"`
	WaiversUnread      *waiversUnread                  `json:"waivers_unread,omitempty"`
	Footprint          footprintSummary                `json:"footprint"`
	invalid            error
}

func newDoctorCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "Check configuration health, Template drift, and Checkpoint setup", Long: `Check configuration health and Template drift, then report what the
repository's declared Checkpoints still lack in this clone. Each finding is
one line ending in the command that fixes it. Unresolved names, missing or
edited team-floor Integrations, a stale agents-md block, Markdown exemptions
a selected documentation Profile reviews, and installed Integrations no
declared Checkpoint uses are listed, followed by the Waivers recorded here in
the last 30 days. The last line sums up what Review Party keeps on this host;
review-party footprint lists it.

Doctor exits 1 only when the configuration is invalid. Findings exit 0, so
the repository's instructions and the Caller decide whether one blocks.`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		options := doctorOptions{repository: stringFlag(cmd, "repo"), format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config")}
		return commandResult(executeDoctor(cmd.Context(), options, streams))
	}}
	addRepositoryFlag(cmd, "Repository whose configuration should be checked")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func executeDoctor(ctx context.Context, options doctorOptions, streams commandIO) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		result, err := options.inspect(manager)
		if err != nil {
			return 0, err
		}
		result.Footprint = footprintAfterReap(ctx, streams.host, options.configuration)
		if options.format == "json" {
			if err := writeJSON(streams.output, result); err != nil {
				return 0, err
			}
			if !result.Valid {
				return 1, nil
			}
			return 0, nil
		}
		if code := printConfigOutput(streams, result.writeText); code != 0 || result.invalid == nil {
			return code, nil
		}
		return printFailure(streams.errors, result.invalid), nil
	})
}

// inspect checks the configuration of the repository's root, or of the
// given path when it is not in a git repository, whose findings then cannot
// be read.
func (options doctorOptions) inspect(manager *configuration.Manager) (doctorResult, error) {
	root, rootErr := subject.ResolveRepositoryRoot(options.repository)
	repository := configuration.Repository(options.repository)
	if rootErr == nil {
		repository = configuration.Repository(root)
	}
	validation, validationErr := validateConfiguration(manager, "", repository)
	drift, err := manager.TemplateDrift(repository)
	if err != nil {
		return doctorResult{}, err
	}
	result := doctorResult{
		Valid: validationErr == nil, Validation: validation, TemplateDrift: drift, Skipped: manager.SkippedTemplates(),
		UnresolvedNames: []unresolvedName{}, IntegrationGaps: []floorGap{}, ExemptionConflicts: []exemptionConflict{}, Undeclared: []undeclaredIntegration{}, RecentWaivers: []model.CheckpointWaiver{},
		invalid: validationErr,
	}
	result.Validation.Valid = validationErr == nil
	if validationErr != nil {
		result.Validation.Error = validationErr.Error()
	}
	if rootErr != nil {
		return result, nil
	}
	return result, doctorRepository{manager: manager, root: root, configuration: options.configuration}.addFindings(&result)
}

// writeText reports a valid configuration with its Template drift, then the
// findings. An invalid configuration's error goes to stderr after them.
func (result doctorResult) writeText(output *commandOutput) {
	if result.invalid == nil {
		output.write("configuration is valid\n")
		for _, item := range result.TemplateDrift {
			output.write("%s\n", doctorTemplateDriftLine(item))
		}
		for _, item := range result.Skipped {
			output.write("Template skipped: %s: %s\n", item.TemplateID, item.Reason)
		}
	}
	for _, line := range result.findingLines() {
		output.write("%s\n", line)
	}
	output.write("%s\n", result.Footprint.line())
}

// footprintAfterReap waits for the reap this invocation's Open started, so
// the summary counts only what the reap could not remove.
func footprintAfterReap(ctx context.Context, host hostDirectories, configurationPath string) footprintSummary {
	if run, err := hostrun.From(ctx); err == nil {
		run.Reap()
	}
	return takeFootprint(host, configurationPath).summary()
}

func doctorTemplateDriftLine(item configuration.TemplateDrift) string {
	if item.Status == configuration.TemplateSourceUnavailable {
		return fmt.Sprintf("Template source unavailable: %s:%s %s@%s; saved instructions still run", item.Scope, item.Profile, item.TemplateID, item.TemplateRevision)
	}
	return fmt.Sprintf("Template update: %s:%s %s → %s", item.Scope, item.Profile, item.TemplateRevision, item.AvailableRevision)
}
