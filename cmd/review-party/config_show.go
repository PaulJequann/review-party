package main

import (
	"errors"
	"fmt"
	"io"
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
		printHumanConfigurationShow(streams.output, report)
		return 0, nil
	})
}

func buildConfigurationShowReport(manager *configuration.Manager, repository string) (configurationShowReport, error) {
	repositoryID := configuration.Repository(repository)
	loaded, err := manager.Load(repositoryID)
	if err != nil {
		return configurationShowReport{}, err
	}
	effective, err := manager.ResolveLoaded(configuration.Request{Repository: repositoryID}, loaded)
	if err != nil {
		return configurationShowReport{}, err
	}
	report := configurationShowReport{Repository: repository, Effective: effectiveConfigurationViewOf(effective)}
	selection, err := manager.ResolveRunLoaded(configuration.RunRequest{Repository: repositoryID}, loaded)
	if errors.Is(err, configuration.ErrNoRepositorySelection) {
		report.SelectionError = err.Error()
		return report, nil
	}
	if err != nil {
		return configurationShowReport{}, err
	}
	report.Reviews = &selection
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

func printHumanConfigurationShow(output io.Writer, report configurationShowReport) {
	fmt.Fprintf(output, "repository: %s\n", report.Repository)
	printHumanValue(output, "default reviewer", report.Effective.DefaultReviewer)
	printHumanValue(output, "state directory", report.Effective.StateDirectory)
	for _, id := range sortedReviewerReportIDs(report.Effective.Reviewers) {
		settings := report.Effective.Reviewers[id]
		fmt.Fprintf(output, "reviewer: %s\n", id)
		printHumanValue(output, "  enabled", settings.Enabled)
		printHumanValue(output, "  model", settings.Model)
		printHumanValue(output, "  allowed models", settings.AllowedModels)
	}
	if report.Reviews == nil {
		fmt.Fprintf(output, "reviews: unavailable (%s)\n", report.SelectionError)
		return
	}
	fmt.Fprintf(output, "reviews: %s, limit %d (%s)\n", report.Reviews.Kind, report.Reviews.ConcurrencyLimit, report.Reviews.LimitSource)
	for _, authored := range report.Reviews.Authored {
		fmt.Fprintf(output, "  selected: %s:%s\n", authored.Scope, authored.Name)
	}
	for _, expanded := range report.Reviews.Expanded {
		fmt.Fprintf(output, "  run: %s:%s (%s)\n", expanded.Scope, expanded.Profile, expanded.Origin)
	}
	for _, duplicate := range report.Reviews.Deduplicated {
		fmt.Fprintf(output, "  deduplicated: %s:%s (%s, kept %s)\n", duplicate.Scope, duplicate.Profile, duplicate.Origin, duplicate.KeptOrigin)
	}
	for _, warning := range report.Reviews.Warnings {
		fmt.Fprintf(output, "  warning: %s\n", warning.Message)
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
		loaded, err := manager.LoadScope(scope, configuration.Repository(options.repository))
		if err != nil {
			return 0, err
		}
		if !loaded.Present {
			return printMissingConfigurationFile(manager, scope, options, streams), nil
		}
		return printAuthoredPayload(loaded.Payload(), streams), nil
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
	fmt.Fprintf(streams.output, "%s Configuration is not authored at %s\n", scope, path)
	return 0
}

func printAuthoredPayload(payload []byte, streams commandIO) int {
	if _, err := streams.output.Write(payload); err != nil {
		return printFailure(streams.errors, err)
	}
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		_, _ = io.WriteString(streams.output, "\n")
	}
	return 0
}
