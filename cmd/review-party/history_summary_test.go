package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func TestHistorySummaryReportsPerProfileReceipts(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	saveSummaryFixture(t, stateHome)
	summary := requireSummaryJSON(t)
	requireSummaryReceipt(t, summary)
	requireSummaryHuman(t)
}

func saveSummaryFixture(t *testing.T, stateHome string) {
	t.Helper()
	ledger, err := store.NewLedgerRecordStore(filepath.Join(stateHome, "review-party"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ledger.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	first := model.ReviewRecord{
		SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: "rp_1723200000000_aaaaaaaaaaaaaaaa",
		Lifecycle: model.LifecycleCompleted, ProfileRevision: model.ProfileRevision{Name: "bugs", ReviewerID: "opencode", Model: "grok-4.5"},
		Result:    &model.ReviewResult{Status: model.ResultFindings, Findings: []model.Finding{{Ordinal: 1}}},
		Timings:   &model.ReviewTimings{TotalMS: 60000},
		CreatedAt: now, UpdatedAt: now,
	}
	second := first
	second.ID = "rp_1723200000000_bbbbbbbbbbbbbbbb"
	second.Timings = &model.ReviewTimings{TotalMS: 180000}
	if err := ledger.Save(first); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Save(second); err != nil {
		t.Fatal(err)
	}
}

func requireSummaryJSON(t *testing.T) store.HistorySummary {
	t.Helper()
	jsonOutput := runMainCommand(t, []string{"history", "--summary", "--format", "json"})
	var summary store.HistorySummary
	if err := json.Unmarshal([]byte(jsonOutput), &summary); err != nil {
		t.Fatalf("summary = %q: %v", jsonOutput, err)
	}
	return summary
}

func requireSummaryReceipt(t *testing.T, summary store.HistorySummary) {
	t.Helper()
	if len(summary.Profiles) != 1 {
		t.Fatalf("profiles = %#v", summary.Profiles)
	}
	profile := summary.Profiles[0]
	if profile.Profile != "bugs" {
		t.Fatalf("profile = %#v", profile)
	}
	if profile.Runs != 2 {
		t.Fatalf("runs = %d", profile.Runs)
	}
	if profile.TotalFindings != 2 {
		t.Fatalf("findings = %d", profile.TotalFindings)
	}
	if profile.MedianDurationMS != 120000 {
		t.Fatalf("median = %d", profile.MedianDurationMS)
	}
}

func requireSummaryHuman(t *testing.T) {
	t.Helper()
	human := runMainCommand(t, []string{"history", "--summary"})
	for _, want := range []string{"bugs", "last 2 runs", "med 2m0s", "grok-4.5", "2 findings"} {
		if !strings.Contains(human, want) {
			t.Fatalf("human = %q, missing %q", human, want)
		}
	}
}

func TestReceiptFromSummaryFindsProfile(t *testing.T) {
	summary := store.HistorySummary{Profiles: []store.ProfileSummary{
		{Profile: "bugs", Runs: 2, MedianDuration: "2m0s", TotalFindings: 3},
	}}
	receipt, found := receiptFromSummary(summary, "bugs")
	if !found {
		t.Fatal("receipt not found")
	}
	if receipt.Runs != 2 {
		t.Fatalf("runs = %d", receipt.Runs)
	}
	if receipt.MedianDuration != "2m0s" {
		t.Fatalf("median = %q", receipt.MedianDuration)
	}
	if receipt.TotalFindings != 3 {
		t.Fatalf("findings = %d", receipt.TotalFindings)
	}
	if _, found := receiptFromSummary(summary, "missing"); found {
		t.Fatal("missing profile unexpectedly found")
	}
}
