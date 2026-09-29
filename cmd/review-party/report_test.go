package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
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
			Artifacts:  []model.ArtifactReference{{Kind: "assistant-text", Path: "artifacts/" + string(id) + "/assistant-text.txt", Size: 12, Digest: "digest"}},
		}}}},
		Result: result,
	}
}

func renderReport(t *testing.T, report reviewReport, format string) string {
	t.Helper()
	var output bytes.Buffer
	if err := printReport(&output, report, reportOptions{format: format, configuration: defaultUserConfigurationPath()}); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func decodeReport(t *testing.T, output string) reviewReport {
	t.Helper()
	var report reviewReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("output = %q: %v", output, err)
	}
	return report
}

func requireBundleSummary(t *testing.T, report reviewReport, id model.ReviewBundleID, lifecycle model.Lifecycle) {
	t.Helper()
	if report.Bundle == nil {
		t.Fatalf("report = %#v, want bundle %s", report, id)
	}
	if got, want := [2]string{string(report.Bundle.ID), string(report.Bundle.Lifecycle)}, [2]string{string(id), string(lifecycle)}; got != want {
		t.Fatalf("bundle = %v, want %v", got, want)
	}
}

func findingCounts(report reviewReport) []int {
	counts := make([]int, len(report.Reviews))
	for index, entry := range report.Reviews {
		counts[index] = len(entry.Findings)
	}
	return counts
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
	got := []any{len(entry.Findings), entry.Subject.ChangedPathCount, entry.Reviewer.Model, entry.Profile.ResultContractRevision}
	if want := []any{6, 25, "gpt-5", "contract-v1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("compact entry facts = %v, want %v", got, want)
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
	report := bundleReport(context.Background(), loader, bundle, false)
	requireBundleSummary(t, report, "rb_report", model.LifecycleIncomplete)
	if !report.incomplete() {
		t.Fatal("an incomplete bundle report is not incomplete")
	}
	type entryFacts struct {
		id            model.ReviewID
		lifecycle     model.Lifecycle
		status        model.ResultStatus
		scope, origin string
		findings      int
		nullFindings  bool
		hasSubject    bool
	}
	var got []entryFacts
	for _, entry := range report.Reviews {
		got = append(got, entryFacts{entry.ID, entry.Lifecycle, entry.Status, entry.Profile.Scope, entry.Origin, len(entry.Findings), entry.Findings == nil, entry.Subject != nil})
	}
	want := []entryFacts{
		{"rp_bugs", model.LifecycleCompleted, model.ResultFindings, "global", "party:baseline", 2, false, true},
		{"rp_quality", model.LifecycleCompleted, model.ResultClean, "repository", "", 0, false, true},
		{"", model.LifecyclePending, "", "global", "", 0, false, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %+v, want %+v", got, want)
	}
	if encoded := marshalReport(t, report); strings.Contains(encoded, reportPatchSentinel) {
		t.Fatal("compact bundle report carries a member patch")
	}
}

func TestPrintReportKeepsPatchRawAndArtifactsBehindFull(t *testing.T) {
	record := largePatchRecord("rp_patch", "bugs", 1)
	for _, format := range []string{"json", "human"} {
		for _, full := range []bool{false, true} {
			output := renderReport(t, recordReport(record, full), format)
			for _, marker := range []string{reportPatchSentinel, "RAW_SENTINEL", "artifacts/rp_patch/assistant-text.txt"} {
				if strings.Contains(output, marker) != full {
					t.Errorf("%s output with full=%t: contains %q = %t", format, full, marker, !full)
				}
			}
			if !strings.Contains(output, "internal/file01.go:1") {
				t.Errorf("%s output with full=%t omitted the finding location", format, full)
			}
		}
	}
}

func findingsBundleReport(t *testing.T) reviewReport {
	t.Helper()
	loader := fakeReviewLoader{
		"rp_bugs":     largePatchRecord("rp_bugs", "bugs", 1),
		"rp_security": largePatchRecord("rp_security", "security", 2),
	}
	bundle := model.ReviewBundle{
		ID: "rb_findings", Lifecycle: model.LifecycleCompleted, Revision: "bundle-revision",
		SubjectKind: "working_changes", SubjectIdentity: "0123456789abcdef0123",
		Members: []model.BundleMember{
			{Scope: "global", Profile: "bugs", ReviewID: "rp_bugs", Lifecycle: model.LifecycleCompleted},
			{Scope: "repository", Profile: "security", ReviewID: "rp_security", Lifecycle: model.LifecycleCompleted},
		},
	}
	report := bundleReport(context.Background(), loader, bundle, false)
	return report
}

func TestPrintBundleReportJSONInlinesMemberFindings(t *testing.T) {
	decoded := decodeReport(t, renderReport(t, findingsBundleReport(t), "json"))
	var locations []string
	for _, entry := range decoded.Reviews {
		for _, finding := range entry.Findings {
			locations = append(locations, string(entry.ID)+" "+finding.Location)
		}
	}
	if want := []string{"rp_bugs internal/file01.go:1", "rp_security internal/file01.go:1", "rp_security internal/file02.go:2"}; !reflect.DeepEqual(locations, want) {
		t.Fatalf("JSON finding locations = %v, want %v", locations, want)
	}
}

func TestPrintBundleReportHumanShowsFindingsBeforeMetadata(t *testing.T) {
	human := renderReport(t, findingsBundleReport(t), "human")
	metadata := strings.Index(human, "revision: bundle-revision\n")
	if metadata < 0 {
		t.Fatalf("human output = %q, missing the revision line", human)
	}
	for _, line := range []string{
		"bundle rb_findings · completed · 2/2 review(s) completed\n",
		"global:bugs · rp_bugs · completed · findings · 1 finding(s)\n",
		"repository:security · rp_security · completed · findings · 2 finding(s)\n",
		"  2. HIGH · correctness · internal/file02.go:2\n",
		"     evidence: The caller ignores the new state.\n",
	} {
		if index := strings.Index(human, line); index < 0 || index > metadata {
			t.Fatalf("human output = %q, want %q before the revision line", human, line)
		}
	}
}

func TestInspectBundleCommandInlinesMemberFindings(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	members := []model.ReviewRecord{
		largePatchRecord("rp_1723200000000_aaaaaaaaaaaaaaaa", "bugs", 1),
		largePatchRecord("rp_1723200000000_bbbbbbbbbbbbbbbb", "security", 2),
	}
	saveInspectBundleFixture(t, stateHome, model.ReviewBundle{ID: "rb_1723200000000_cccccccccccccccc"}, members...)

	compact := inspectBundleJSON(t, "rb_1723200000000_cccccccccccccccc")
	full := inspectBundleJSON(t, "rb_1723200000000_cccccccccccccccc", "--full")
	if got := findingCounts(compact); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("finding counts = %v, want [1 2]", got)
	}
	if len(full.Reviews) != len(members) {
		t.Fatalf("full reviews = %d, want %d", len(full.Reviews), len(members))
	}
	for index, member := range members {
		if compact.Reviews[index].Record != nil {
			t.Fatalf("review %s carries the full record without --full", member.ID)
		}
		if record := full.Reviews[index].Record; record == nil || record.Subject.Patch != member.Subject.Patch {
			t.Fatalf("review %s with --full omitted the subject patch", member.ID)
		}
	}
}

func saveInspectBundleFixture(t *testing.T, stateHome string, bundle model.ReviewBundle, members ...model.ReviewRecord) {
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
	dangling := bundle.Members
	bundle.Members = nil
	bundle.Lifecycle, bundle.Warnings, bundle.Deduplicated = model.LifecycleCompleted, []model.BundleWarning{}, []model.SkippedDuplicate{}
	for _, record := range members {
		record.SchemaVersion = model.CurrentReviewRecordSchemaVersion
		record.CreatedAt, record.UpdatedAt = now, now
		if err := ledger.Save(record); err != nil {
			t.Fatal(err)
		}
		bundle.Members = append(bundle.Members, model.BundleMember{Scope: "global", Profile: record.ProfileRevision.Name, ReviewID: record.ID, Lifecycle: record.Lifecycle})
	}
	bundle.Members = append(bundle.Members, dangling...)
	if err := ledger.CreateReviewBundle(bundle, nil); err != nil {
		t.Fatal(err)
	}
}

func inspectBundleJSON(t *testing.T, id model.ReviewBundleID, flags ...string) reviewReport {
	t.Helper()
	report := decodeReport(t, runMainCommand(t, append([]string{"inspect", string(id), "--format", "json"}, flags...)))
	requireBundleSummary(t, report, id, model.LifecycleCompleted)
	return report
}

func TestInspectBundleReportsAnUnreadableMemberBesideTheOthers(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	const missing = model.ReviewID("rp_1723200000000_ffffffffffffffff")
	bundle := model.ReviewBundle{ID: "rb_1723200000000_dddddddddddddddd", Members: []model.BundleMember{
		{Scope: "global", Profile: "security", ReviewID: missing, Lifecycle: model.LifecycleCompleted, Status: "clean"},
	}}
	saveInspectBundleFixture(t, stateHome, bundle, largePatchRecord("rp_1723200000000_eeeeeeeeeeeeeeee", "bugs", 2))

	report := decodeReport(t, inspectWithReadFailure(t, bundle.ID, missing, "json"))
	if got := findingCounts(report); !reflect.DeepEqual(got, []int{2, 0}) {
		t.Fatalf("finding counts = %v, want the healthy member's 2 findings beside the unreadable member", got)
	}
	unreadable := report.Reviews[1]
	if got, want := []any{unreadable.ID, unreadable.Lifecycle, unreadable.Status}, []any{missing, lifecycleUnreadable, model.ResultStatus("")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unreadable member id, lifecycle, status = %v, want %v", got, want)
	}
	if !strings.Contains(unreadable.ReadError, string(missing)) {
		t.Fatalf("read error = %q, want the cause naming %s", unreadable.ReadError, missing)
	}
	human := inspectWithReadFailure(t, bundle.ID, missing, "human")
	for _, line := range []string{"global:bugs · rp_1723200000000_eeeeeeeeeeeeeeee · completed", "internal/file02.go:2", "global:security · " + string(missing) + " · unreadable\n"} {
		if !strings.Contains(human, line) {
			t.Fatalf("human output = %q, missing %q", human, line)
		}
	}
}

func inspectWithReadFailure(t *testing.T, id model.ReviewBundleID, missing model.ReviewID, format string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"inspect", string(id), "--format", format}, &stdout, &stderr); exit != 1 || !strings.Contains(stderr.String(), string(missing)) {
		t.Fatalf("%s inspect exit = %d, stderr = %q; want exit 1 naming %s", format, exit, stderr.String(), missing)
	}
	return stdout.String()
}
