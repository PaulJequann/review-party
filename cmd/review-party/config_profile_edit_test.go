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

func TestConfigProfileEditUpdatesModel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requireConfigSuccess(t, []string{
		"config", "profile", "create", "bugs", "--blank", "--instructions", "Find bugs.\n", "--reviewer", "grok",
		"--model", "grok-4.5", "--effort", "high", "--deadline", "1m", "--yes", "--format", "json",
	})
	result := runConfigCommand(t, []string{
		"config", "profile", "edit", "bugs", "--model", "grok-4.6", "--yes", "--format", "json",
	})
	requireCommandSuccess(t, result)
	var plan configurationPlanResult
	if err := json.Unmarshal([]byte(result.stdout), &plan); err != nil {
		t.Fatalf("plan output = %q: %v", result.stdout, err)
	}
	if !plan.Valid {
		t.Fatalf("plan = %#v", plan)
	}
	if !plan.Published {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Changes[0].Field != "profiles.bugs.model" {
		t.Fatalf("changes = %#v", plan.Changes)
	}
}

func TestConfigProfileEditRequiresAField(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	result := runConfigCommand(t, []string{"config", "profile", "edit", "bugs", "--yes", "--format", "json"})
	if result.exitCode == 0 {
		t.Fatal("fieldless edit succeeded")
	}
	if !strings.Contains(result.stdout+result.stderr, "at least one") {
		t.Fatalf("error = %q %q", result.stdout, result.stderr)
	}
}

func TestConfigProfileEditExposesFlags(t *testing.T) {
	root := newRootCommand(productionCommandIO(nil, nil, nil))
	command, _, err := root.Find([]string{"config", "profile", "edit"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"scope", "reviewer", "model", "effort", "deadline", "repo", "format", "config", "yes"} {
		if command.Flags().Lookup(flag) == nil {
			t.Errorf("edit does not expose --%s", flag)
		}
	}
}

func TestConfigProfileEditWarnsWithRecordedReceipt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	requireConfigSuccess(t, []string{
		"config", "profile", "create", "bugs", "--blank", "--instructions", "Find bugs.\n", "--reviewer", "grok",
		"--model", "grok-4.5", "--effort", "high", "--deadline", "1m", "--yes", "--format", "json",
	})
	saveEditReceiptFixture(t, stateHome)
	result := runConfigCommand(t, []string{
		"config", "profile", "edit", "bugs", "--model", "grok-4.6", "--yes", "--format", "json",
	})
	requireCommandSuccess(t, result)
	var plan configurationPlanResult
	if err := json.Unmarshal([]byte(result.stdout), &plan); err != nil {
		t.Fatalf("plan output = %q: %v", result.stdout, err)
	}
	requireReceiptWarning(t, plan.Warnings)
}

func saveEditReceiptFixture(t *testing.T, stateHome string) {
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
	record := model.ReviewRecord{
		SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: "rp_1723200000000_0123456789abcdef",
		Lifecycle: model.LifecycleCompleted, ProfileRevision: model.ProfileRevision{Name: "bugs", ReviewerID: "grok", Model: "grok-4.5"},
		Result:    &model.ReviewResult{Status: model.ResultFindings, Findings: []model.Finding{{Ordinal: 1}}},
		Timings:   &model.ReviewTimings{TotalMS: 60000},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := ledger.Save(record); err != nil {
		t.Fatal(err)
	}
}

func requireReceiptWarning(t *testing.T, warnings []string) {
	t.Helper()
	for _, warning := range warnings {
		if strings.Contains(warning, "last 1 run") && strings.Contains(warning, "history --profile bugs") {
			return
		}
	}
	t.Fatalf("warnings = %#v, missing execution receipt", warnings)
}
