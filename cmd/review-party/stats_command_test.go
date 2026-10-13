package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/store"
)

func TestStatsReportsTheLedgerAsJSONAndText(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	saveSummaryFixture(t, stateHome)

	jsonOutput := runMainCommand(t, []string{"stats", "--since", "2026-08-01T00:00:00Z", "--format", "json"})
	var stats store.Stats
	if err := json.Unmarshal([]byte(jsonOutput), &stats); err != nil {
		t.Fatalf("stats = %q: %v", jsonOutput, err)
	}
	if got, want := [3]int{stats.SchemaVersion, stats.Runs.Total, stats.Status.Findings}, [3]int{store.StatsSchemaVersion, 2, 2}; got != want {
		t.Fatalf("schema, runs, findings reviews = %v, want %v", got, want)
	}
	if want := (store.DurationStats{Samples: 2, P50: 60000, P90: 180000}); stats.DurationMS != want {
		t.Fatalf("durations = %+v, want %+v", stats.DurationMS, want)
	}
	human := runMainCommand(t, []string{"stats", "--since", "2026-08-01T00:00:00Z"})
	for _, want := range []string{"Runs: 2 · clean 0 · findings 2", "p50 1m0s · p90 3m0s", "  2026-08-11  2\n", "   2  unbundled"} {
		if !strings.Contains(human, want) {
			t.Errorf("human output missing %q:\n%s", want, human)
		}
	}
}

func TestStatsRejectsAnInvalidWindowAsUsage(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"stats", "--since", "-3d"}, &stdout, &stderr); exit != usageExitCode {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
}

func TestParseStatsSince(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	for text, want := range map[string]time.Time{
		"24h":                  now.Add(-24 * time.Hour),
		"90m":                  now.Add(-90 * time.Minute),
		"7d":                   now.AddDate(0, 0, -7),
		"2w":                   now.AddDate(0, 0, -14),
		"520w":                 now.AddDate(0, 0, -3640),
		"2026-08-01T00:00:00Z": time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	} {
		if got, err := parseStatsSince(text, now); err != nil || !got.Equal(want) {
			t.Errorf("parseStatsSince(%q) = %v, %v; want %v", text, got, err, want)
		}
	}
	for _, text := range []string{"", "0d", "-1h", "0s", "7 days", "2027-01-01T00:00:00Z", "1000000000d", "3653d", "2000-01-01T00:00:00Z"} {
		if _, err := parseStatsSince(text, now); err == nil {
			t.Errorf("parseStatsSince(%q) succeeded, want an error", text)
		}
	}
}
