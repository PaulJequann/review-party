package main

import (
	"errors"
	"sort"

	"reviewparty/internal/configuration"
)

type configurationShowReport struct {
	Repository     string                         `json:"repository"`
	Effective      effectiveConfigurationView     `json:"effective"`
	Reviews        *configuration.ResolvedReviews `json:"reviews,omitempty"`
	SelectionError string                         `json:"selection_error,omitempty"`
}

type effectiveConfigurationView struct {
	DefaultReviewer valueReport[string]                   `json:"default_reviewer"`
	StateDirectory  valueReport[string]                   `json:"state_directory"`
	Eval            valueReport[configuration.EvalPolicy] `json:"eval"`
	Reviewers       map[string]reviewerSettingsReport     `json:"reviewers"`
}

type reviewerSettingsReport struct {
	Enabled       valueReport[bool]     `json:"enabled"`
	Model         valueReport[string]   `json:"model"`
	AllowedModels valueReport[[]string] `json:"allowed_models"`
}

func executeConfigurationShow(options configurationFileOptions, streams commandIO) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		report, err := buildConfigurationShowReport(manager, options.repository)
		if err != nil {
			return 0, err
		}
		if options.format == "json" {
			if err := writeJSON(streams.output, report); err != nil {
				return 0, err
			}
			return 0, nil
		}
		if err := writeCommandOutput(streams.output, func(output *commandOutput) {
			printHumanConfigurationShow(output, report)
		}); err != nil {
			return 0, err
		}
		return 0, nil
	})
}

func buildConfigurationShowReport(manager *configuration.Manager, repository string) (configurationShowReport, error) {
	repositoryID := configuration.Repository(repository)
	snapshot, err := manager.ResolveRuntime(configuration.RunRequest{Repository: repositoryID})
	if errors.Is(err, configuration.ErrNoRepositorySelection) {
		if snapshot == nil {
			return configurationShowReport{}, err
		}
		return configurationShowReport{
			Repository: repository, Effective: effectiveConfigurationViewOf(snapshot.Effective()),
			SelectionError: err.Error(),
		}, nil
	}
	if err != nil {
		return configurationShowReport{}, err
	}
	selection := snapshot.Selection()
	report := configurationShowReport{Repository: repository, Effective: effectiveConfigurationViewOf(snapshot.Effective()), Reviews: &selection}
	return report, nil
}

func effectiveConfigurationViewOf(effective configuration.Effective) effectiveConfigurationView {
	report := effectiveConfigurationView{
		DefaultReviewer: valueReportOf(effective.DefaultReviewer),
		StateDirectory:  valueReportOf(effective.StateDirectory),
		Eval:            valueReportOf(effective.Eval),
		Reviewers:       map[string]reviewerSettingsReport{},
	}
	for _, id := range effective.ReviewerIDs() {
		settings, _ := effective.ReviewerPolicy(id)
		report.Reviewers[id] = reviewerSettingsReport{
			Enabled: valueReportOf(settings.Enabled), Model: valueReportOf(settings.Model),
			AllowedModels: valueReportOf(settings.AllowedModels),
		}
	}
	return report
}

func printHumanConfigurationShow(output *commandOutput, report configurationShowReport) {
	output.write("repository: %s\n", report.Repository)
	printHumanValue(output, "default reviewer", report.Effective.DefaultReviewer)
	printHumanValue(output, "state directory", report.Effective.StateDirectory)
	for _, id := range sortedReviewerReportIDs(report.Effective.Reviewers) {
		settings := report.Effective.Reviewers[id]
		output.write("reviewer: %s\n", id)
		printHumanValue(output, "  enabled", settings.Enabled)
		printHumanValue(output, "  model", settings.Model)
		printHumanValue(output, "  allowed models", settings.AllowedModels)
	}
	if report.Reviews == nil {
		output.write("reviews: unavailable (%s)\n", report.SelectionError)
		return
	}
	output.write("reviews: %s, limit %d (%s)\n", report.Reviews.Kind, report.Reviews.ConcurrencyLimit, report.Reviews.LimitSource)
	for _, authored := range report.Reviews.Authored {
		output.write("  selected: %s:%s\n", authored.Scope, authored.Name)
	}
	if output.err != nil {
		return
	}
	output.err = configuration.RenderResolvedReviewsHuman(output.writer, *report.Reviews)
	if output.err != nil {
		return
	}
}

func sortedReviewerReportIDs(reviewers map[string]reviewerSettingsReport) []string {
	ids := make([]string, 0, len(reviewers))
	for id := range reviewers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func executeConfigurationFileShow(options configurationFileOptions, streams commandIO) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		scope, err := parseConfigurationScope(options.scope)
		if err != nil {
			return 0, err
		}
		inspection, err := manager.InspectAuthored(configuration.Repository(options.repository), []configuration.Scope{scope})
		if err != nil {
			return 0, err
		}
		file, _ := inspection.File(scope)
		if !file.Present {
			return printMissingConfigurationFile(manager, scope, options, streams), nil
		}
		return printAuthoredPayload(file, streams), nil
	})
}

func printMissingConfigurationFile(manager *configuration.Manager, scope configuration.Scope, options configurationFileOptions, streams commandIO) int {
	path, err := manager.ConfigPath(scope, configuration.Repository(options.repository))
	if err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	status := configurationFileStatus{Scope: string(scope), Path: path}
	if options.format == "json" {
		if err := writeJSON(streams.output, status); err != nil {
			return printConfigFailure(options.format, streams.output, streams.errors, err)
		}
		return 0
	}
	if err := writeCommandOutput(streams.output, func(output *commandOutput) {
		output.write("%s Configuration is not authored at %s\n", scope, path)
	}); err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	return 0
}

func printAuthoredPayload(file configuration.AuthoredFile, streams commandIO) int {
	if err := file.WritePayload(streams.output); err != nil {
		return printFailure(streams.errors, err)
	}
	if !file.HasTrailingNewline() {
		if err := writeCommandOutput(streams.output, func(output *commandOutput) {
			output.write("\n")
		}); err != nil {
			return printFailure(streams.errors, err)
		}
	}
	return 0
}
