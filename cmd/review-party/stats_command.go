package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"reviewparty/internal/engine"
	"reviewparty/internal/store"
)

const defaultStatsWindow = "7d"

func newStatsCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Summarize recorded Review usage, outcomes, and failures",
		Long: "Stats reads the Review ledger and reports runs per day, repository, Profile, Reviewer, and selection; outcomes; " +
			"duration percentiles; grouped failure diagnostics; findings; Verdict rates; Checkpoint Waivers; and Review Bundle completion. " +
			"It never writes state. Days are UTC dates. The JSON shape is versioned by schema_version and documented in docs/stats-json.md.",
		Example: "  review-party stats\n  review-party stats --since 24h --format json\n  review-party stats --since 30d --repo .",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := statsOptions{
				sinceText: stringFlag(cmd, "since"), repository: stringFlag(cmd, "repo"),
				format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"), now: time.Now(),
			}
			return commandResult(executeStats(cmd.Context(), options, streams.output, streams.errors))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	cmd.Flags().String("since", defaultStatsWindow, "Window start: a duration back from now (24h, 7d, 2w) or an RFC3339 timestamp")
	cmd.Flags().String("repo", "", "Only count Reviews, Bundles, and Waivers from this Git repository")
	return cmd
}

type statsOptions struct {
	sinceText     string
	repository    string
	format        string
	configuration string
	now           time.Time
}

func executeStats(ctx context.Context, options statsOptions, stdout, stderr io.Writer) int {
	since, err := parseStatsSince(options.sinceText, options.now)
	if err != nil {
		return printCommandError(stderr, usageExitCode, err)
	}
	repository, err := resolvedHistoryRepository(options.repository)
	if err != nil {
		return printFailure(stderr, err)
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		return printFailure(stderr, err)
	}
	stats, err := conductor.Stats(ctx, store.StatsQuery{Repository: repository, Since: since, Now: options.now})
	if err != nil {
		return printFailure(stderr, err)
	}
	return renderLedgerOutput(options.format, stdout, stderr, stats, func(output *commandOutput) {
		writeStats(output, stats)
	})
}

var (
	statsDayWindow = regexp.MustCompile(`^(\d+)([dw])$`)
	statsDayUnits  = map[string]int{"d": 1, "w": 7}
)

// parseStatsSince accepts a Go duration, a whole number of days or weeks, or
// an RFC3339 timestamp, and returns a window start before now.
func parseStatsSince(text string, now time.Time) (time.Time, error) {
	since, err := statsWindowStart(text, now)
	if err != nil || !since.Before(now) {
		return time.Time{}, fmt.Errorf("--since %q must be a positive duration such as 24h, 7d, or 2w, or a past RFC3339 timestamp", text)
	}
	return since, nil
}

func statsWindowStart(text string, now time.Time) (time.Time, error) {
	if match := statsDayWindow.FindStringSubmatch(text); match != nil {
		count, err := strconv.Atoi(match[1])
		return now.AddDate(0, 0, -count*statsDayUnits[match[2]]), err
	}
	if duration, err := time.ParseDuration(text); err == nil {
		return now.Add(-duration), nil
	}
	return time.Parse(time.RFC3339, text)
}

func writeStats(output *commandOutput, stats store.Stats) {
	scope := "all repositories"
	if stats.Window.Repository != "" {
		scope = stats.Window.Repository
	}
	output.write("Review Party stats · %s → %s UTC · %s\n", stats.Window.Since.Format(time.DateTime), stats.Window.Until.Format(time.DateTime), scope)
	status := stats.Status
	output.write("Runs: %d · clean %d · findings %d · failed %d · cancelled %d · in progress %d\n",
		stats.Runs.Total, status.Clean, status.Findings, status.Failed, status.Cancelled, status.InProgress)
	output.write("Duration (completed, n=%d): p50 %s · p90 %s\n", stats.DurationMS.Samples, formatStatsDuration(stats.DurationMS.P50), formatStatsDuration(stats.DurationMS.P90))
	writeStatsDays(output, stats.Runs.PerDay)
	for _, section := range []struct {
		title  string
		counts []store.NamedCount
	}{{"By repository", stats.Runs.ByRepository}, {"By Profile", stats.Runs.ByProfile}, {"By Reviewer", stats.Runs.ByReviewer}, {"By selection", stats.Runs.BySelection}} {
		writeStatsCounts(output, section.title, section.counts)
	}
	output.write("Terminations: %s\n", formatStatsMap(stats.Terminations))
	writeStatsDiagnostics(output, stats.FailureDiagnostics)
	writeStatsOutcomes(output, stats)
}

func writeStatsDays(output *commandOutput, days []store.DayCount) {
	output.write("Per day (UTC, days without runs omitted):\n")
	for _, day := range days {
		if day.Runs > 0 {
			output.write("  %s  %d\n", day.Date, day.Runs)
		}
	}
}

func writeStatsCounts(output *commandOutput, title string, counts []store.NamedCount) {
	if len(counts) == 0 {
		output.write("%s: none\n", title)
		return
	}
	output.write("%s:\n", title)
	for _, count := range counts {
		output.write("  %4d  %s\n", count.Count, count.Name)
	}
}

func writeStatsDiagnostics(output *commandOutput, groups []store.DiagnosticGroup) {
	if len(groups) == 0 {
		output.write("Top failure diagnostics: none\n")
		return
	}
	output.write("Top failure diagnostics:\n")
	for _, group := range groups {
		output.write("  %4d× %s (last %s)\n", group.Count, group.Diagnostic, group.LastSeen.Format(time.DateOnly))
	}
}

func writeStatsOutcomes(output *commandOutput, stats store.Stats) {
	findings := stats.Findings
	output.write("Findings: %d (%s per completed Review) · %s\n", findings.Total, formatStatsNumber(findings.PerCompletedReview), formatStatsMap(findings.BySeverity))
	verdicts := stats.Verdicts
	output.write("Verdicts: %d judged, %d unjudged · accepted %d (%s) · rejected %d (%s) · deferred %d (%s)\n",
		verdicts.Judged, verdicts.Unjudged, verdicts.Accepted, formatStatsRate(verdicts.AcceptedRate),
		verdicts.Rejected, formatStatsRate(verdicts.RejectedRate), verdicts.Deferred, formatStatsRate(verdicts.DeferredRate))
	output.write("Checkpoint Waivers: %d · %s\n", stats.CheckpointWaivers.Total, formatStatsMap(stats.CheckpointWaivers.ByWaivedBy))
	bundles := stats.Bundles
	output.write("Review Bundles: %d · completed %d · incomplete %d · in progress %d · completion %s\n",
		bundles.Total, bundles.Completed, bundles.Incomplete, bundles.InProgress, formatStatsRate(bundles.CompletionRate))
}

func formatStatsMap(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(counts))
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, fmt.Sprintf("%s %d", name, counts[name]))
	}
	return strings.Join(parts, ", ")
}

func formatStatsDuration(milliseconds int64) string {
	if milliseconds == 0 {
		return "—"
	}
	return (time.Duration(milliseconds) * time.Millisecond).Round(time.Second).String()
}

func formatStatsRate(rate *float64) string {
	if rate == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *rate*100)
}

func formatStatsNumber(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f", *value)
}
