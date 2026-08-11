package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
)

func TestPrintRecordUsesActualAttemptProvenance(t *testing.T) {
	record := model.ReviewRecord{
		Lifecycle:       model.LifecycleCompleted,
		ProfileRevision: model.ProfileRevision{ReviewerID: "copilot", Model: "auto", Effort: "auto"},
		Passes: []model.PassRecord{{Attempts: []model.AttemptRecord{{
			Provenance: model.ReviewerProvenance{ReviewerID: "copilot", Model: "gpt-5-mini", Effort: "high"},
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

func TestUsageListsEverySupportedReviewer(t *testing.T) {
	var output bytes.Buffer
	printUsage(&output)
	for _, reviewer := range engine.SupportedReviewers() {
		if !strings.Contains(output.String(), reviewer) {
			t.Fatalf("usage omits supported reviewer %q:\n%s", reviewer, output.String())
		}
	}
}

func TestPrintRecordJSONIncludesStructuredFindings(t *testing.T) {
	record := model.ReviewRecord{Result: &model.ReviewResult{
		Status: model.ResultFindings,
		Findings: []model.Finding{{
			Ordinal:  1,
			Severity: "HIGH",
			Category: "correctness",
			Location: "review.go:3",
			Failure:  "The changed state is not handled.",
			Evidence: "The Subject changes the state without updating its caller.",
			Fix:      "Update the caller with the state change.",
			Test:     "Exercise the caller with the changed state.",
		}},
	}}
	var output bytes.Buffer
	if err := printRecord(&output, record, "json"); err != nil {
		t.Fatal(err)
	}
	var inspected model.ReviewRecord
	if err := json.Unmarshal(output.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.Result == nil || len(inspected.Result.Findings) != 1 || inspected.Result.Findings[0].Evidence != record.Result.Findings[0].Evidence {
		t.Fatalf("inspected result = %#v, want structured finding", inspected.Result)
	}
}

func TestPrintRecordListsArtifactReferencesWithoutContents(t *testing.T) {
	record := model.ReviewRecord{Passes: []model.PassRecord{{Attempts: []model.AttemptRecord{{Artifacts: []model.ArtifactReference{{
		Kind: "assistant-text", Path: "artifacts/rp_1/1/assistant-text.txt", Size: 12, Digest: "digest",
	}}}}}}}
	var output bytes.Buffer
	if err := printRecord(&output, record, "human"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "artifact: assistant-text · artifacts/rp_1/1/assistant-text.txt · 12 bytes · sha256:digest") {
		t.Fatalf("output = %q, want artifact reference", output.String())
	}
}
