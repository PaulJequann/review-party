package store

import (
	"reflect"
	"testing"
	"time"

	"reviewparty/internal/model"
)

var statsNow = time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)

func TestStatsSummarizesTheReviewsInTheWindow(t *testing.T) {
	stats := seededStats(t, "")
	requireStatsField(t, "per day", stats.Runs.PerDay, []DayCount{{"2026-08-09", 0}, {"2026-08-10", 3}, {"2026-08-11", 0}})
	requireStatsField(t, "by selection", stats.Runs.BySelection, []NamedCount{{"repository_default", 2}, {unbundledSelection, 1}})
	requireStatsField(t, "status", stats.Status, StatusCounts{Findings: 1, Failed: 1, Cancelled: 1})
	requireStatsField(t, "findings", stats.Findings, FindingStats{Total: 1, PerCompletedReview: statsRate(1), BySeverity: map[string]int{"high": 1}})
	requireStatsField(t, "verdicts", stats.Verdicts, VerdictStats{Judged: 1, Rejected: 1, AcceptedRate: statsRate(0), RejectedRate: statsRate(1), DeferredRate: statsRate(0)})
	requireStatsField(t, "waivers", stats.CheckpointWaivers, CheckpointWaiverSum{Total: 1, ByWaivedBy: map[string]int{"terminal": 1}})
	requireStatsField(t, "bundles", stats.Bundles, BundleStats{Total: 1, Incomplete: 1, CompletionRate: statsRate(0)})
	requireStatsField(t, "durations", stats.DurationMS, DurationStats{Samples: 1, P50: 60000, P90: 60000})
}

func TestStatsFiltersByRepository(t *testing.T) {
	stats := seededStats(t, "/other")
	requireStatsField(t, "status", stats.Status, StatusCounts{Failed: 1})
	requireStatsField(t, "findings", stats.Findings, FindingStats{BySeverity: map[string]int{}})
	requireStatsField(t, "verdicts", stats.Verdicts, VerdictStats{})
	requireStatsField(t, "waivers", stats.CheckpointWaivers, CheckpointWaiverSum{ByWaivedBy: map[string]int{}})
	requireStatsField(t, "bundles", stats.Bundles, BundleStats{})
	requireStatsField(t, "diagnostics", stats.FailureDiagnostics, []DiagnosticGroup{{Diagnostic: "timed out after <n>ms", Count: 1, LastSeen: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}})
}

func seededStats(t *testing.T, repository string) Stats {
	t.Helper()
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	seedStatsLedger(t, ledger)
	stats, err := ledger.Stats(StatsQuery{Repository: repository, Since: statsNow.AddDate(0, 0, -2), Now: statsNow})
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func requireStatsField(t *testing.T, field string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %+v, want %+v", field, got, want)
	}
}

func statsRate(value float64) *float64 {
	return &value
}

func TestStatsRejectsAnEmptyWindow(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	if _, err := ledger.Stats(StatsQuery{Since: statsNow, Now: statsNow}); err == nil {
		t.Fatal("expected an empty window to fail")
	}
}

// seedStatsLedger writes one Bundle in /repo with a findings Review and a
// cancelled Review, one failed unbundled Review in /other, and one Review
// before every window the tests use.
func seedStatsLedger(t *testing.T, ledger *LedgerRecordStore) {
	t.Helper()
	bundle, members := pendingBundleFixture()
	bundle.Selection = &model.BundleSelection{Kind: "repository_default"}
	if err := ledger.CreateReviewBundle(bundle, members); err != nil {
		t.Fatal(err)
	}
	findings := ledgerFixture(model.LifecycleCompleted)
	findings.Timings = &model.ReviewTimings{TotalMS: 60000}
	cancelled := ledgerFixture(model.LifecycleIncomplete)
	cancelled.ID = members[1].ID
	cancelled.Termination.Category = model.TerminationCancelled
	failed := ledgerFixture(model.LifecycleIncomplete)
	failed.ID = "rp_1723200000001_0123456789abcdef"
	failed.Subject.Repository = "/other"
	failed.Passes[0].Attempts[0].Outcome = model.AttemptTransientFailure
	failed.Passes[0].Attempts[0].Diagnostic = "timed out after 900000ms"
	old := ledgerFixture(model.LifecycleCompleted)
	old.ID = "rp_1723200000002_0123456789abcdef"
	old.CreatedAt = statsNow.AddDate(0, 0, -30)
	saveTestReviews(t, ledger, findings, cancelled, failed, old)
	bundle.Lifecycle = model.LifecycleIncomplete
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		t.Fatal(err)
	}
	for _, verdict := range []model.Verdict{model.VerdictAccepted, model.VerdictRejected} {
		batch := VerdictBatch{ReviewID: findings.ID, Judgments: []model.Judgment{judgment(t, 1, verdict, "real")}, RecordedBy: "test", RecordedAt: statsNow}
		if _, err := ledger.RecordVerdicts(batch); err != nil {
			t.Fatal(err)
		}
	}
	waiver := waiverFixture("wv_1", model.WaiverKey{Checkpoint: "pre-push", ContentDigest: "d"}, 0)
	waiver.Repository, waiver.CreatedAt = "/repo", statsNow.Add(-time.Hour)
	if err := ledger.RecordCheckpointWaiver(waiver); err != nil {
		t.Fatal(err)
	}
}

func TestFailureDiagnosticsGroupAfterNormalization(t *testing.T) {
	at := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	failures := []statsFailure{
		{outcome: "failed", diagnostic: "\x1b[31mreview rp_1723200000000_0123456789abcdef failed at 2026-08-10T01:02:03Z\x1b[0m", completedAt: at},
		{outcome: "failed", diagnostic: "review  rp_1723200000001_fedcba9876543210 failed at 2026-08-11 04:05:06.7 +0000 UTC", completedAt: at.Add(time.Hour)},
		{outcome: "transient_failure", completedAt: at},
	}
	want := []DiagnosticGroup{
		{Diagnostic: "review <review> failed at <time>", Count: 2, LastSeen: at.Add(time.Hour)},
		{Diagnostic: "(no diagnostic) transient_failure", Count: 1, LastSeen: at},
	}
	if got := groupFailureDiagnostics(failures); !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %+v, want %+v", got, want)
	}
}

func TestDurationPercentilesUseNearestRankOverCompletedReviews(t *testing.T) {
	reviews := []statsReview{{lifecycle: model.LifecycleIncomplete, totalMS: 999999}}
	for _, total := range []int64{50, 10, 40, 20, 30, 100, 90, 60, 80, 70} {
		reviews = append(reviews, statsReview{lifecycle: model.LifecycleCompleted, totalMS: total})
	}
	if got, want := summarizeDurations(reviews), (DurationStats{Samples: 10, P50: 50, P90: 90}); got != want {
		t.Fatalf("durations = %+v, want %+v", got, want)
	}
}
