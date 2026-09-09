package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"reviewparty/internal/engine"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

func executeHistory(ctx context.Context, options historyOptions, stdout, stderr io.Writer) int {
	query, err := validatedHistoryQuery(options)
	if err != nil {
		return printCommandError(stderr, usageExitCode, err)
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	query.Repository, err = resolvedHistoryRepository(query.Repository)
	if err != nil {
		return printFailure(stderr, err)
	}
	page, err := conductor.History(ctx, query)
	if err != nil {
		return printFailure(stderr, err)
	}
	if options.summary {
		return printHistorySummary(store.SummarizeHistory(page), options.format, stdout, stderr)
	}
	return printHistory(page, options.format, stdout, stderr)
}

type historyOptions struct {
	query         store.HistoryQuery
	format        string
	configuration string
	sinceText     string
	summary       bool
}

func validatedHistoryQuery(options historyOptions) (store.HistoryQuery, error) {
	if options.query.Limit < 1 || options.query.Limit > store.MaxHistoryLimit {
		return store.HistoryQuery{}, fmt.Errorf("history accepts --limit N and --format human|json")
	}
	if options.sinceText == "" {
		return options.query, nil
	}
	since, err := time.Parse(time.RFC3339, options.sinceText)
	if err != nil {
		return store.HistoryQuery{}, fmt.Errorf("--since must be an RFC3339 timestamp")
	}
	options.query.Since = &since
	return options.query, nil
}

func resolvedHistoryRepository(repository string) (string, error) {
	if repository == "" {
		return "", nil
	}
	return subject.ResolveRepositoryRoot(repository)
}

func printHistory(page store.HistoryPage, format string, stdout, stderr io.Writer) int {
	if page.Entries == nil {
		page.Entries = []store.HistoryEntry{}
	}
	return renderHistoryOutput(format, stdout, stderr, page, func(output *commandOutput) {
		for _, entry := range page.Entries {
			output.write("%s\n", formatHistoryEntry(entry))
		}
	})
}

func formatHistoryEntry(entry store.HistoryEntry) string {
	parts := []string{string(entry.ID), string(entry.Lifecycle), entry.Profile, entry.Reviewer, entry.Subject}
	if entry.ReplaysReviewID != nil {
		parts = append(parts, "replays "+string(*entry.ReplaysReviewID))
	}
	if entry.Termination != "" {
		parts = append(parts, string(entry.Termination))
	}
	parts = append(parts, entry.CreatedAt.UTC().Format(time.RFC3339))
	return strings.Join(parts, " · ")
}

func printHistorySummary(summary store.HistorySummary, format string, stdout, stderr io.Writer) int {
	if summary.Profiles == nil {
		summary.Profiles = []store.ProfileSummary{}
	}
	return renderHistoryOutput(format, stdout, stderr, summary, func(output *commandOutput) {
		for _, profile := range summary.Profiles {
			output.write("%s\n", formatProfileSummary(profile, summary.HasMore))
		}
	})
}

func renderHistoryOutput(format string, stdout, stderr io.Writer, jsonValue any, human func(*commandOutput)) int {
	if format == "json" {
		if err := json.NewEncoder(stdout).Encode(jsonValue); err != nil {
			return printFailure(stderr, err)
		}
		return 0
	}
	if format != "human" {
		return printFailure(stderr, fmt.Errorf("unknown output format %q", format))
	}
	return printCommandOutput(stdout, stderr, human)
}

func formatProfileSummary(profile store.ProfileSummary, hasMore bool) string {
	scope := "last " + formatRunCount(profile.Runs)
	if hasMore {
		scope += " (more in ledger)"
	}
	models := strings.Join(profile.Models, ", ")
	if models == "" {
		models = "unknown model"
	}
	return strings.Join([]string{
		profile.Profile, scope, "med " + profile.MedianDuration,
		models, formatFindingsCount(profile.TotalFindings),
	}, " · ")
}

func formatRunCount(runs int) string {
	if runs == 1 {
		return "1 run"
	}
	return fmt.Sprintf("%d runs", runs)
}

func formatFindingsCount(findings int) string {
	if findings == 1 {
		return "1 finding"
	}
	return fmt.Sprintf("%d findings", findings)
}
