package main

import (
	"bytes"
	"strings"
	"testing"

	"reviewparty"
)

func TestPrintRecordUsesActualAttemptProvenance(t *testing.T) {
	record := reviewparty.ReviewRecord{
		Lifecycle:       reviewparty.LifecycleCompleted,
		ProfileRevision: reviewparty.ProfileRevision{ReviewerID: "copilot", Model: "auto", Effort: "auto"},
		Passes: []reviewparty.PassRecord{{Attempts: []reviewparty.AttemptRecord{{
			Provenance: reviewparty.ReviewerProvenance{ReviewerID: "copilot", Model: "gpt-5-mini", Effort: "high"},
		}}}},
	}
	var output bytes.Buffer
	if err := printRecord(&output, record, "human"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "copilot/gpt-5-mini (high)") {
		t.Fatalf("output = %q", output.String())
	}
}
