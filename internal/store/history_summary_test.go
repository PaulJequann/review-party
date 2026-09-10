package store

import (
	"testing"

	"reviewparty/internal/model"
)

func TestSummarizeHistoryGroupsByProfile(t *testing.T) {
	page := HistoryPage{Limit: 20, Entries: []HistoryEntry{
		{Lifecycle: model.LifecycleCompleted, Profile: "bugs", Model: "grok-4.5", DurationMS: 1000, Findings: 2},
		{Lifecycle: model.LifecycleCompleted, Profile: "style", Model: "grok-4.5", DurationMS: 500, Findings: 0},
		{Lifecycle: model.LifecycleCompleted, Profile: "bugs", Model: "grok-4.6", DurationMS: 3000, Findings: 1},
	}}
	summary := SummarizeHistory(page)
	requireProfileOrder(t, summary)
	requireBugsReceipt(t, summary.Profiles[0])
}

func requireProfileOrder(t *testing.T, summary HistorySummary) {
	t.Helper()
	if len(summary.Profiles) != 2 {
		t.Fatalf("profiles = %#v", summary.Profiles)
	}
	if summary.Profiles[0].Profile != "bugs" || summary.Profiles[1].Profile != "style" {
		t.Fatalf("order = %#v", summary.Profiles)
	}
}

func requireBugsReceipt(t *testing.T, bugs ProfileSummary) {
	t.Helper()
	if bugs.Runs != 2 {
		t.Fatalf("runs = %d", bugs.Runs)
	}
	if bugs.TotalFindings != 3 {
		t.Fatalf("findings = %d", bugs.TotalFindings)
	}
	if bugs.MedianDurationMS != 2000 {
		t.Fatalf("median = %d, want 2000", bugs.MedianDurationMS)
	}
	requireBugsModels(t, bugs)
}

func requireBugsModels(t *testing.T, bugs ProfileSummary) {
	t.Helper()
	if len(bugs.Models) != 2 {
		t.Fatalf("models = %#v", bugs.Models)
	}
	if bugs.Models[0] != "grok-4.5" || bugs.Models[1] != "grok-4.6" {
		t.Fatalf("models = %#v", bugs.Models)
	}
}

func TestSummarizeHistoryUsesOddMedian(t *testing.T) {
	page := HistoryPage{Entries: []HistoryEntry{
		{Lifecycle: model.LifecycleCompleted, Profile: "bugs", DurationMS: 3000},
		{Lifecycle: model.LifecycleCompleted, Profile: "bugs", DurationMS: 1000},
		{Lifecycle: model.LifecycleCompleted, Profile: "bugs", DurationMS: 2000},
	}}
	summary := SummarizeHistory(page)
	if summary.Profiles[0].MedianDurationMS != 2000 {
		t.Fatalf("median = %d, want 2000", summary.Profiles[0].MedianDurationMS)
	}
}

func TestSummarizeHistoryExcludesIncompleteRunsFromAggregates(t *testing.T) {
	page := HistoryPage{Entries: []HistoryEntry{
		{Lifecycle: model.LifecycleCompleted, Profile: "bugs", Model: "grok-4.5", DurationMS: 60000, Findings: 2},
		{Lifecycle: model.LifecycleRunning, Profile: "bugs"},
		{Lifecycle: model.LifecycleIncomplete, Profile: "bugs", DurationMS: 45000},
	}}
	summary := SummarizeHistory(page)
	if len(summary.Profiles) != 1 {
		t.Fatalf("profiles = %#v", summary.Profiles)
	}
	bugs := summary.Profiles[0]
	if bugs.Runs != 3 || bugs.IncompleteRuns != 2 {
		t.Fatalf("runs = %d, incomplete = %d, want 3/2", bugs.Runs, bugs.IncompleteRuns)
	}
	if bugs.MedianDurationMS != 60000 {
		t.Fatalf("median = %d, want 60000 (incomplete durations excluded)", bugs.MedianDurationMS)
	}
}

func TestSummarizeHistoryCompletedRunsCarryFindingsAndModels(t *testing.T) {
	page := HistoryPage{Entries: []HistoryEntry{
		{Lifecycle: model.LifecycleCompleted, Profile: "bugs", Model: "grok-4.5", DurationMS: 60000, Findings: 2},
		{Lifecycle: model.LifecycleIncomplete, Profile: "bugs", DurationMS: 45000},
	}}
	summary := SummarizeHistory(page)
	if len(summary.Profiles) != 1 {
		t.Fatalf("profiles = %#v", summary.Profiles)
	}
	bugs := summary.Profiles[0]
	if bugs.TotalFindings != 2 {
		t.Fatalf("findings = %d, want 2", bugs.TotalFindings)
	}
	if len(bugs.Models) != 1 || bugs.Models[0] != "grok-4.5" {
		t.Fatalf("models = %#v", bugs.Models)
	}
}

func TestSummarizeHistoryOnlyIncompleteRunsHasNoAggregate(t *testing.T) {
	summary := SummarizeHistory(HistoryPage{Entries: []HistoryEntry{
		{Lifecycle: model.LifecyclePending, Profile: "ghost"},
	}})
	if len(summary.Profiles) != 1 {
		t.Fatalf("profiles = %#v", summary.Profiles)
	}
	ghost := summary.Profiles[0]
	if ghost.Runs != 1 || ghost.IncompleteRuns != 1 {
		t.Fatalf("runs = %d, incomplete = %d, want 1/1", ghost.Runs, ghost.IncompleteRuns)
	}
	if ghost.MedianDurationMS != 0 || ghost.MedianDuration != "" {
		t.Fatalf("median = %d %q, want empty (no completed runs)", ghost.MedianDurationMS, ghost.MedianDuration)
	}
	if ghost.TotalFindings != 0 || len(ghost.Models) != 0 {
		t.Fatalf("findings = %d, models = %#v, want empty", ghost.TotalFindings, ghost.Models)
	}
}

func TestSummarizeHistoryEmptyPage(t *testing.T) {
	summary := SummarizeHistory(HistoryPage{})
	if len(summary.Profiles) != 0 {
		t.Fatalf("profiles = %#v", summary.Profiles)
	}
}

func TestLedgerHistoryReportsExecutionReceipts(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	record := ledgerFixture(model.LifecycleCompleted)
	record.ProfileRevision.Model = "grok-4.5"
	record.Timings = &model.ReviewTimings{TotalMS: 250000}
	saveTestReviews(t, ledger, record)
	page := loadTestHistory(t, ledger, HistoryQuery{Limit: 10})
	if len(page.Entries) != 1 {
		t.Fatalf("entries = %#v", page.Entries)
	}
	entry := page.Entries[0]
	if entry.Model != "grok-4.5" {
		t.Fatalf("model = %q", entry.Model)
	}
	if entry.DurationMS != 250000 {
		t.Fatalf("duration = %d", entry.DurationMS)
	}
	if entry.Findings != 1 {
		t.Fatalf("findings = %d", entry.Findings)
	}
	if entry.Status != string(model.ResultFindings) {
		t.Fatalf("status = %q", entry.Status)
	}
}
