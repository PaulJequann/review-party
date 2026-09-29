package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

const reportPatchSentinel = "PATCH_SENTINEL_LINE"

func largePatchRecord(id model.ReviewID, profile string, findings int) model.ReviewRecord {
	paths := make([]string, 25)
	for index := range paths {
		paths[index] = fmt.Sprintf("internal/file%02d.go", index)
	}
	result := &model.ReviewResult{Status: model.ResultClean, Summary: profile + " summary", Raw: "RAW_SENTINEL", Findings: []model.Finding{}}
	for ordinal := 1; ordinal <= findings; ordinal++ {
		result.Status = model.ResultFindings
		result.Findings = append(result.Findings, model.Finding{
			Ordinal: ordinal, Severity: "HIGH", Category: "correctness", Location: fmt.Sprintf("internal/file%02d.go:%d", ordinal, ordinal),
			Failure: "The changed state is not handled.", Evidence: "The caller ignores the new state.",
			Fix: "Handle the state in the caller.", Test: "Exercise the caller with the new state.",
		})
	}
	return model.ReviewRecord{
		ID: id, Lifecycle: model.LifecycleCompleted,
		Subject: model.ReviewSubject{
			Kind: "working_changes", Repository: "/repo", Identity: "0123456789abcdef0123",
			ChangedPaths: paths, Patch: strings.Repeat(reportPatchSentinel+" "+strings.Repeat("x", 80)+"\n", 512),
		},
		ProfileRevision: model.ProfileRevision{Name: profile, Revision: "rev-" + profile, Source: "global", ResultContract: "contract-v1"},
		Passes: []model.PassRecord{{Attempts: []model.AttemptRecord{{
			Provenance: model.ReviewerProvenance{ReviewerID: "codex", Model: "gpt-5", Effort: "high"},
		}}}},
		Result: result,
	}
}

func marshalReport(t *testing.T, report reviewReport) string {
	t.Helper()
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestRecordReportOmitsPatchUnlessFull(t *testing.T) {
	record := largePatchRecord("rp_patch", "bugs", 6)
	if len(record.Subject.Patch) < 50_000 {
		t.Fatalf("fixture patch is %d bytes, want at least 50 KB", len(record.Subject.Patch))
	}

	compact := recordReport(record, false)
	encoded := marshalReport(t, compact)
	if strings.Contains(encoded, reportPatchSentinel) || strings.Contains(encoded, `"changed_paths"`) {
		t.Fatalf("compact report carries subject content: %d bytes", len(encoded))
	}
	entry := compact.Reviews[0]
	if len(entry.Findings) != 6 || entry.Subject.ChangedPathCount != 25 || entry.Reviewer.Model != "gpt-5" || entry.Profile.ResultContractRevision != "contract-v1" {
		t.Fatalf("compact entry = %#v", entry)
	}

	full := marshalReport(t, recordReport(record, true))
	if !strings.Contains(full, reportPatchSentinel) || !strings.Contains(full, `"changed_paths"`) {
		t.Fatal("full report omitted the patch or changed paths")
	}
}

func TestReportFindingsAreAnArrayEvenWithoutAResult(t *testing.T) {
	record := model.ReviewRecord{ID: "rp_incomplete", Lifecycle: model.LifecycleIncomplete}
	if encoded := marshalReport(t, recordReport(record, false)); !strings.Contains(encoded, `"findings":[]`) {
		t.Fatalf("report = %s, want an empty findings array", encoded)
	}
}

type fakeReviewLoader map[model.ReviewID]model.ReviewRecord

func (loader fakeReviewLoader) Inspect(_ context.Context, id model.ReviewID) (model.ReviewRecord, error) {
	record, ok := loader[id]
	if !ok {
		return model.ReviewRecord{}, errors.New("review not found")
	}
	return record, nil
}

func TestBundleReportInlinesEveryMembersFindings(t *testing.T) {
	loader := fakeReviewLoader{
		"rp_bugs":    largePatchRecord("rp_bugs", "bugs", 2),
		"rp_quality": largePatchRecord("rp_quality", "code-quality", 0),
	}
	bundle := model.ReviewBundle{
		ID: "rb_report", Lifecycle: model.LifecycleIncomplete,
		Members: []model.BundleMember{
			{Scope: "global", Profile: "bugs", ReviewID: "rp_bugs", Lifecycle: model.LifecycleCompleted, Origin: "party:baseline"},
			{Scope: "repository", Profile: "code-quality", ReviewID: "rp_quality", Lifecycle: model.LifecycleCompleted},
			{Scope: "global", Profile: "security", Lifecycle: model.LifecyclePending},
		},
	}
	report, err := bundleReport(context.Background(), loader, bundle, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Bundle == nil || report.Bundle.ID != "rb_report" || !report.incomplete() {
		t.Fatalf("bundle summary = %#v", report.Bundle)
	}
	if len(report.Reviews) != 3 {
		t.Fatalf("reviews = %#v", report.Reviews)
	}
	bugs, quality, security := report.Reviews[0], report.Reviews[1], report.Reviews[2]
	if len(bugs.Findings) != 2 || bugs.Profile.Scope != "global" || bugs.Origin != "party:baseline" {
		t.Fatalf("bugs entry = %#v", bugs)
	}
	if quality.Status != model.ResultClean || quality.Profile.Scope != "repository" {
		t.Fatalf("quality entry = %#v", quality)
	}
	if security.ID != "" || security.Lifecycle != model.LifecyclePending || security.Subject != nil || security.Findings == nil {
		t.Fatalf("unstarted entry = %#v", security)
	}
	if encoded := marshalReport(t, report); strings.Contains(encoded, reportPatchSentinel) {
		t.Fatal("compact bundle report carries a member patch")
	}
}

func TestBundleReportFailsWhenAMemberCannotBeLoaded(t *testing.T) {
	bundle := model.ReviewBundle{ID: "rb_missing", Members: []model.BundleMember{{Scope: "global", Profile: "bugs", ReviewID: "rp_missing"}}}
	if _, err := bundleReport(context.Background(), fakeReviewLoader{}, bundle, false); err == nil || !strings.Contains(err.Error(), "rp_missing") {
		t.Fatalf("err = %v, want a member load failure naming rp_missing", err)
	}
}
