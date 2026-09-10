package store

import (
	"sort"
	"time"

	"reviewparty/internal/model"
)

// HistorySummary aggregates one page of history entries into per-profile
// execution receipts: run counts, median latency, and findings. Medians cover
// only the entries summarized, never the full ledger.
type HistorySummary struct {
	Profiles []ProfileSummary `json:"profiles"`
	Limit    int              `json:"limit"`
	HasMore  bool             `json:"has_more"`
}

// ProfileSummary is the execution receipt for one recorded Profile. Medians
// and findings cover completed runs only; pending or incomplete runs are
// counted separately so an unfinished review never renders as a clean one.
type ProfileSummary struct {
	Profile          string   `json:"profile"`
	Runs             int      `json:"runs"`
	MedianDurationMS int64    `json:"median_duration_ms"`
	MedianDuration   string   `json:"median_duration"`
	TotalFindings    int      `json:"total_findings"`
	Models           []string `json:"models"`
	IncompleteRuns   int      `json:"incomplete_runs,omitempty"`
}

// SummarizeHistory groups entries by recorded Profile in first-seen order.
func SummarizeHistory(page HistoryPage) HistorySummary {
	groups := map[string][]HistoryEntry{}
	var order []string
	for _, entry := range page.Entries {
		if _, found := groups[entry.Profile]; !found {
			order = append(order, entry.Profile)
		}
		groups[entry.Profile] = append(groups[entry.Profile], entry)
	}
	summary := HistorySummary{Limit: page.Limit, HasMore: page.HasMore}
	for _, profile := range order {
		summary.Profiles = append(summary.Profiles, summarizeProfile(profile, groups[profile]))
	}
	if summary.Profiles == nil {
		summary.Profiles = []ProfileSummary{}
	}
	return summary
}

func summarizeProfile(profile string, entries []HistoryEntry) ProfileSummary {
	completed := make([]HistoryEntry, 0, len(entries))
	summary := ProfileSummary{Profile: profile, Runs: len(entries)}
	for _, entry := range entries {
		if entry.Lifecycle != model.LifecycleCompleted {
			summary.IncompleteRuns++
			continue
		}
		completed = append(completed, entry)
	}
	if len(completed) == 0 {
		return summary
	}
	summary.MedianDurationMS, summary.MedianDuration, summary.TotalFindings, summary.Models = aggregateCompletedRuns(completed)
	return summary
}

// aggregateCompletedRuns reduces completed runs into the receipt aggregates:
// median latency, total findings, and the distinct models used.
func aggregateCompletedRuns(completed []HistoryEntry) (int64, string, int, []string) {
	durations := make([]int64, 0, len(completed))
	models := map[string]struct{}{}
	var total int
	for _, entry := range completed {
		durations = append(durations, entry.DurationMS)
		total += entry.Findings
		if entry.Model != "" {
			models[entry.Model] = struct{}{}
		}
	}
	median := medianDuration(durations)
	return median, formatDurationMS(median), total, sortedModels(models)
}

func medianDuration(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func formatDurationMS(value int64) string {
	if value <= 0 {
		return "0s"
	}
	return (time.Duration(value) * time.Millisecond).String()
}

func sortedModels(models map[string]struct{}) []string {
	result := make([]string, 0, len(models))
	for model := range models {
		result = append(result, model)
	}
	sort.Strings(result)
	return result
}
