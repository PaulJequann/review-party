package main

import (
	"io"
	"strings"

	"reviewparty/internal/model"
)

func printHumanReport(output io.Writer, report reviewReport, configuration string) error {
	return writeCommandOutput(output, func(output *commandOutput) {
		if report.Bundle != nil {
			writeHumanBundle(output, *report.Bundle, report.Reviews, configuration)
			return
		}
		for _, entry := range report.Reviews {
			writeHumanEntry(output, "review "+string(entry.ID), entry)
			writeInspectHint(output, string(entry.ID), configuration)
		}
	})
}

func writeHumanEntry(output *commandOutput, label string, entry reviewEntry) {
	if entry.Lifecycle == lifecycleUnreadable {
		output.write("%s · unreadable\nread error: %s\n", label, entry.ReadError)
		writeHumanMisses(output, entry.Misses)
		return
	}
	output.write("%s · %s", label, entry.Lifecycle)
	if entry.Status != "" {
		output.write(" · %s", entry.Status)
	}
	output.write(" · %d finding(s)\n", len(entry.Findings))
	if entry.Summary != "" {
		output.write("%s\n", entry.Summary)
	}
	for _, finding := range entry.Findings {
		writeHumanFinding(output, finding)
	}
	writeHumanMisses(output, entry.Misses)
	writeEntryProvenance(output, entry)
	if entry.Record != nil {
		writeFullRecord(output, *entry.Record)
	}
}

func writeHumanMisses(output *commandOutput, misses []model.Miss) {
	for _, miss := range misses {
		output.write("  miss: %s · %s · %s (%s, recorded by %s)\n", miss.Location, miss.Source, miss.Description, miss.ID, miss.RecordedBy)
	}
}

func writeHumanFinding(output *commandOutput, finding model.Finding) {
	const indent = "     "
	output.write("  %d. %s · %s · %s\n", finding.Ordinal, finding.Severity, finding.Category, finding.Location)
	for _, field := range []struct{ label, value string }{
		{"", finding.Failure}, {"evidence: ", finding.Evidence}, {"fix: ", finding.Fix}, {"test: ", finding.Test},
	} {
		if field.value != "" {
			output.write("%s%s%s\n", indent, field.label, strings.ReplaceAll(strings.TrimRight(field.value, "\n"), "\n", "\n"+indent))
		}
	}
}

func writeEntryProvenance(output *commandOutput, entry reviewEntry) {
	profile := entry.Profile.Name
	if entry.Profile.Scope != "" {
		profile = entry.Profile.Scope + ":" + profile
	}
	if entry.Profile.Source != "" {
		profile += " · " + entry.Profile.Source
	}
	output.write("profile: %s\n", profile)
	output.write("reviewer: %s/%s (%s)\n", entry.Reviewer.ReviewerID, entry.Reviewer.Model, entry.Reviewer.Effort)
	output.write("subject: %s %s · %d changed path(s)\n", entry.Subject.Kind, shortIdentity(entry.Subject.Identity), entry.Subject.ChangedPathCount)
	if entry.ReplaysReviewID != nil {
		output.write("replays: %s\n", *entry.ReplaysReviewID)
	}
	if entry.Termination != nil {
		output.write("incomplete: %s at %s: %s\n", entry.Termination.Category, entry.Termination.Phase, entry.Termination.Message)
	}
}

func writeFullRecord(output *commandOutput, record model.ReviewRecord) {
	if record.Result != nil && record.Result.Raw != "" {
		output.write("raw:\n%s\n", strings.TrimRight(record.Result.Raw, "\n"))
	}
	writeArtifactReferences(output, record)
	if len(record.Subject.ChangedPaths) > 0 {
		output.write("changed paths:\n")
		for _, path := range record.Subject.ChangedPaths {
			output.write("  %s\n", path)
		}
	}
	if record.Subject.Patch != "" {
		output.write("patch:\n%s\n", strings.TrimRight(record.Subject.Patch, "\n"))
	}
}

func writeInspectHint(output *commandOutput, id, configuration string) {
	output.write("inspect: review-party inspect %s%s\n", id, configurationArgument(configuration))
}

// configurationArgument is the --config suffix a printed follow-up command
// needs to reach the same Global Configuration, or "" for the default.
func configurationArgument(configuration string) string {
	if configuration == "" || configuration == defaultUserConfigurationPath() {
		return ""
	}
	return " --config " + shellQuoteArgument(configuration)
}

func shortIdentity(identity string) string {
	if len(identity) <= 16 {
		return identity
	}
	return identity[:16] + "…"
}

func shellQuoteArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
