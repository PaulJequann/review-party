package main

import (
	"encoding/json"
	"testing"
)

func TestConfigShowReportsEffectiveViewWithoutSavedSelection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repository := t.TempDir()

	result := runConfigCommand(t, []string{"config", "show", "--repo", repository, "--format", "json"})
	requireCommandSuccess(t, result)
	var report configurationShowReport
	if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
		t.Fatalf("config show output = %q: %v", result.stdout, err)
	}
	if report.Reviews != nil || report.SelectionError == "" {
		t.Fatalf("selection report = %#v, want no saved selection", report)
	}
	if report.Effective.DefaultReviewer.Value != "grok" || report.Effective.DefaultReviewer.Source != "packaged" {
		t.Fatalf("effective default reviewer = %#v", report.Effective.DefaultReviewer)
	}
}
