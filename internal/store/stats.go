package store

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"reviewparty/internal/model"
)

// StatsSchemaVersion versions the JSON shape of Stats. Additive fields keep
// the version; renamed, removed, or redefined fields bump it.
const StatsSchemaVersion = 1

// MaxStatsWindowYears bounds a Stats window, which reports one entry per day.
const MaxStatsWindowYears = 10

const (
	topFailureDiagnostics = 10
	maxDiagnosticRunes    = 200
	unbundledSelection    = "unbundled"
)

// StatsQuery selects the Reviews, Review Bundles, and Checkpoint Waivers
// created at or after Since, optionally from one repository root.
type StatsQuery struct {
	Repository string
	Since      time.Time
	Now        time.Time
}

// Stats is the read-only usage report of `review-party stats`. Days are UTC
// calendar dates. Rates are fractions from 0 to 1, or null when nothing was
// counted. Ranked lists are ordered by count, then name.
type Stats struct {
	SchemaVersion      int                 `json:"schema_version"`
	Window             StatsWindow         `json:"window"`
	Runs               RunStats            `json:"runs"`
	Status             StatusCounts        `json:"status"`
	Terminations       map[string]int      `json:"terminations"`
	DurationMS         DurationStats       `json:"duration_ms"`
	FailureDiagnostics []DiagnosticGroup   `json:"failure_diagnostics"`
	Findings           FindingStats        `json:"findings"`
	Verdicts           VerdictStats        `json:"verdicts"`
	CheckpointWaivers  CheckpointWaiverSum `json:"checkpoint_waivers"`
	Bundles            BundleStats         `json:"bundles"`
}

type StatsWindow struct {
	Since      time.Time `json:"since"`
	Until      time.Time `json:"until"`
	Repository string    `json:"repository,omitempty"`
}

// RunStats counts Reviews, one per Profile run.
type RunStats struct {
	Total        int          `json:"total"`
	PerDay       []DayCount   `json:"per_day"`
	ByRepository []NamedCount `json:"by_repository"`
	ByProfile    []NamedCount `json:"by_profile"`
	ByReviewer   []NamedCount `json:"by_reviewer"`
	BySelection  []NamedCount `json:"by_selection"`
}

type DayCount struct {
	Date string `json:"date"`
	Runs int    `json:"runs"`
}

type NamedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// StatusCounts splits Reviews into exclusive outcomes. Failed and cancelled
// together are the Reviews whose lifecycle is incomplete.
type StatusCounts struct {
	Clean      int `json:"clean"`
	Findings   int `json:"findings"`
	Failed     int `json:"failed"`
	Cancelled  int `json:"cancelled"`
	InProgress int `json:"in_progress"`
}

// DurationStats covers completed Reviews with a recorded total duration.
type DurationStats struct {
	Samples int   `json:"samples"`
	P50     int64 `json:"p50"`
	P90     int64 `json:"p90"`
}

type DiagnosticGroup struct {
	Diagnostic string    `json:"diagnostic"`
	Count      int       `json:"count"`
	LastSeen   time.Time `json:"last_seen"`
}

type FindingStats struct {
	Total              int            `json:"total"`
	PerCompletedReview *float64       `json:"per_completed_review"`
	BySeverity         map[string]int `json:"by_severity"`
}

// VerdictStats counts the current Verdict on each Finding of the Reviews in
// the window; rates divide by Judged.
type VerdictStats struct {
	Judged       int      `json:"judged"`
	Unjudged     int      `json:"unjudged"`
	Accepted     int      `json:"accepted"`
	Rejected     int      `json:"rejected"`
	Deferred     int      `json:"deferred"`
	AcceptedRate *float64 `json:"accepted_rate"`
	RejectedRate *float64 `json:"rejected_rate"`
	DeferredRate *float64 `json:"deferred_rate"`
}

type CheckpointWaiverSum struct {
	Total      int            `json:"total"`
	ByWaivedBy map[string]int `json:"by_waived_by"`
}

// BundleStats measures Review Bundles; CompletionRate divides completed by
// finished (completed plus incomplete) Bundles.
type BundleStats struct {
	Total          int      `json:"total"`
	Completed      int      `json:"completed"`
	Incomplete     int      `json:"incomplete"`
	InProgress     int      `json:"in_progress"`
	CompletionRate *float64 `json:"completion_rate"`
}

// statsReview is one Review row the report aggregates.
type statsReview struct {
	repository, profile, reviewer, selection string
	lifecycle                                model.Lifecycle
	status                                   string
	termination                              model.TerminationCategory
	totalMS                                  int64
	createdAt                                time.Time
}

type statsFailure struct {
	outcome, diagnostic string
	completedAt         time.Time
}

type statsBundle struct {
	lifecycle model.Lifecycle
}

// statsRows is everything the ledger contributes to one report.
type statsRows struct {
	reviews    []statsReview
	failures   []statsFailure
	severities map[string]int
	verdicts   map[model.Verdict]int
	waivers    map[string]int
	bundles    []statsBundle
}

func summarizeStats(query StatsQuery, rows statsRows) Stats {
	stats := Stats{
		SchemaVersion:      StatsSchemaVersion,
		Window:             StatsWindow{Since: query.Since.UTC(), Until: query.Now.UTC(), Repository: query.Repository},
		Runs:               summarizeRuns(query, rows.reviews),
		Terminations:       map[string]int{},
		DurationMS:         summarizeDurations(rows.reviews),
		FailureDiagnostics: groupFailureDiagnostics(rows.failures),
		Findings:           FindingStats{BySeverity: rows.severities},
		CheckpointWaivers:  CheckpointWaiverSum{ByWaivedBy: rows.waivers},
		Bundles:            summarizeBundles(rows.bundles),
	}
	completed := 0
	for _, review := range rows.reviews {
		countStatus(&stats.Status, review)
		if review.termination != "" {
			stats.Terminations[string(review.termination)]++
		}
		if review.lifecycle == model.LifecycleCompleted {
			completed++
		}
	}
	for _, count := range rows.severities {
		stats.Findings.Total += count
	}
	stats.Findings.PerCompletedReview = ratio(stats.Findings.Total, completed)
	stats.Verdicts = summarizeVerdicts(rows.verdicts, stats.Findings.Total)
	for _, count := range rows.waivers {
		stats.CheckpointWaivers.Total += count
	}
	return stats
}

func countStatus(status *StatusCounts, review statsReview) {
	switch {
	case review.lifecycle == model.LifecycleCompleted && review.status == string(model.ResultClean):
		status.Clean++
	case review.lifecycle == model.LifecycleCompleted:
		status.Findings++
	case review.lifecycle != model.LifecycleIncomplete:
		status.InProgress++
	case review.termination == model.TerminationCancelled:
		status.Cancelled++
	default:
		status.Failed++
	}
}

func summarizeRuns(query StatsQuery, reviews []statsReview) RunStats {
	repositories, profiles, reviewers, selections := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	days := zeroDays(query.Since, query.Now)
	for _, review := range reviews {
		repositories[review.repository]++
		profiles[review.profile]++
		reviewers[review.reviewer]++
		selections[review.selection]++
		days[review.createdAt.UTC().Format(time.DateOnly)]++
	}
	return RunStats{
		Total: len(reviews), PerDay: dayCounts(days),
		ByRepository: rankCounts(repositories), ByProfile: rankCounts(profiles),
		ByReviewer: rankCounts(reviewers), BySelection: rankCounts(selections),
	}
}

// zeroDays lists every UTC date in the window so quiet days report zero runs.
func zeroDays(since, now time.Time) map[string]int {
	days := map[string]int{}
	last := now.UTC().Format(time.DateOnly)
	for day := since.UTC(); day.Format(time.DateOnly) <= last; day = day.AddDate(0, 0, 1) {
		days[day.Format(time.DateOnly)] = 0
	}
	return days
}

func dayCounts(days map[string]int) []DayCount {
	counts := make([]DayCount, 0, len(days))
	for date, runs := range days {
		counts = append(counts, DayCount{Date: date, Runs: runs})
	}
	slices.SortFunc(counts, func(a, b DayCount) int { return strings.Compare(a.Date, b.Date) })
	return counts
}

func rankCounts(counts map[string]int) []NamedCount {
	ranked := make([]NamedCount, 0, len(counts))
	for name, count := range counts {
		ranked = append(ranked, NamedCount{Name: name, Count: count})
	}
	slices.SortFunc(ranked, func(a, b NamedCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Name, b.Name))
	})
	return ranked
}

func summarizeDurations(reviews []statsReview) DurationStats {
	var durations []int64
	for _, review := range reviews {
		if review.lifecycle == model.LifecycleCompleted && review.totalMS > 0 {
			durations = append(durations, review.totalMS)
		}
	}
	slices.Sort(durations)
	return DurationStats{Samples: len(durations), P50: nearestRank(durations, 0.5), P90: nearestRank(durations, 0.9)}
}

func nearestRank(sorted []int64, percentile float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(math.Ceil(percentile*float64(len(sorted))))-1]
}

func groupFailureDiagnostics(failures []statsFailure) []DiagnosticGroup {
	groups := map[string]*DiagnosticGroup{}
	for _, failure := range failures {
		key := normalizeDiagnostic(failure.diagnostic)
		if key == "" {
			key = "(no diagnostic) " + failure.outcome
		}
		group, found := groups[key]
		if !found {
			group = &DiagnosticGroup{Diagnostic: key}
			groups[key] = group
		}
		group.Count++
		if failure.completedAt.After(group.LastSeen) {
			group.LastSeen = failure.completedAt.UTC()
		}
	}
	ranked := make([]DiagnosticGroup, 0, len(groups))
	for _, group := range groups {
		ranked = append(ranked, *group)
	}
	slices.SortFunc(ranked, func(a, b DiagnosticGroup) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Diagnostic, b.Diagnostic))
	})
	return ranked[:min(len(ranked), topFailureDiagnostics)]
}

var diagnosticNoise = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`), ""},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z| ?[+-]\d{2}:?\d{2}( [A-Z]{3,5})?)?`), "<time>"},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<id>"},
	{regexp.MustCompile(`\brp_\d+_[0-9a-f]+\b`), "<review>"},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{12,}\b`), "<hex>"},
	{regexp.MustCompile(`\d{5,}`), "<n>"},
	{regexp.MustCompile(`\s+`), " "},
}

// normalizeDiagnostic strips terminal colors and replaces times, identifiers,
// and long numbers so repeats of one failure group together.
func normalizeDiagnostic(diagnostic string) string {
	for _, noise := range diagnosticNoise {
		diagnostic = noise.pattern.ReplaceAllString(diagnostic, noise.replacement)
	}
	diagnostic = strings.TrimSpace(diagnostic)
	if runes := []rune(diagnostic); len(runes) > maxDiagnosticRunes {
		diagnostic = string(runes[:maxDiagnosticRunes]) + "…"
	}
	return diagnostic
}

func summarizeVerdicts(verdicts map[model.Verdict]int, findings int) VerdictStats {
	stats := VerdictStats{
		Accepted: verdicts[model.VerdictAccepted],
		Rejected: verdicts[model.VerdictRejected],
		Deferred: verdicts[model.VerdictDeferred],
	}
	stats.Judged = stats.Accepted + stats.Rejected + stats.Deferred
	stats.Unjudged = max(findings-stats.Judged, 0)
	stats.AcceptedRate = ratio(stats.Accepted, stats.Judged)
	stats.RejectedRate = ratio(stats.Rejected, stats.Judged)
	stats.DeferredRate = ratio(stats.Deferred, stats.Judged)
	return stats
}

func summarizeBundles(bundles []statsBundle) BundleStats {
	stats := BundleStats{Total: len(bundles)}
	for _, bundle := range bundles {
		switch bundle.lifecycle {
		case model.LifecycleCompleted:
			stats.Completed++
		case model.LifecycleIncomplete:
			stats.Incomplete++
		case model.LifecyclePending, model.LifecycleRunning:
			stats.InProgress++
		}
	}
	stats.CompletionRate = ratio(stats.Completed, stats.Completed+stats.Incomplete)
	return stats
}

func ratio(numerator, denominator int) *float64 {
	if denominator == 0 {
		return nil
	}
	value := math.Round(float64(numerator)/float64(denominator)*10000) / 10000
	return &value
}
