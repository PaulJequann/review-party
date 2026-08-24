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
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return usageExitCode
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	query.Repository, err = resolvedHistoryRepository(query.Repository)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	page, err := conductor.History(ctx, query)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return printHistory(page, options.format, stdout, stderr)
}

type historyOptions struct {
	query         store.HistoryQuery
	format        string
	configuration string
	sinceText     string
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
	if format == "json" {
		if page.Entries == nil {
			page.Entries = []store.HistoryEntry{}
		}
		if err := json.NewEncoder(stdout).Encode(page); err != nil {
			fmt.Fprintf(stderr, "review-party: %v\n", err)
			return 1
		}
		return 0
	}
	if format != "human" {
		fmt.Fprintf(stderr, "review-party: unknown output format %q\n", format)
		return 1
	}
	for _, entry := range page.Entries {
		fmt.Fprintln(stdout, formatHistoryEntry(entry))
	}
	return 0
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
