package main

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func TestPrintBundleReportPreservesHumanPresentation(t *testing.T) {
	bundle := model.ReviewBundle{
		ID: "rb_test", Lifecycle: model.LifecycleIncomplete, Revision: "revision",
		SubjectKind: "working_changes", SubjectIdentity: "0123456789abcdef0123",
		Selection:   &model.BundleSelection{Kind: "repository_default", Source: "repository_selection", ConcurrencyLimit: 2, LimitSource: "party"},
		Members:     []model.BundleMember{{Scope: "global", Profile: "bugs", Lifecycle: model.LifecycleCompleted, ReviewID: "rp_test", Status: "clean"}},
		Termination: &model.BundleTermination{Category: model.TerminationCancelled, Message: "context canceled"},
	}
	report := bundleReport(context.Background(), fakeReviewLoader{"rp_test": largePatchRecord("rp_test", "bugs", 0)}, bundle, false)
	text := renderReport(t, report, "human")
	for _, want := range []string{
		"bundle rb_test · incomplete · 1/1 review(s) completed\n", "global:bugs · rp_test · completed · clean · 0 finding(s)\n",
		"selection: repository_default · limit 2 (party)\n", "subject: working_changes 0123456789abcdef…\n",
		"incomplete: cancelled: context canceled\n", "inspect: review-party inspect rb_test\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output = %q, want %q", text, want)
		}
	}
}

func TestPrintReportJSONCarriesTheBundleSummary(t *testing.T) {
	report := bundleReport(context.Background(), fakeReviewLoader{}, model.ReviewBundle{ID: "rb_json", Lifecycle: model.LifecycleCompleted}, false)
	requireBundleSummary(t, decodeReport(t, renderReport(t, report, "json")), "rb_json", model.LifecycleCompleted)
}

func TestExecuteRunReturnsUsageExitForIncompleteBundle(t *testing.T) {
	conductor := &fakeRunConductor{records: fakeReviewLoader{"rp_member": largePatchRecord("rp_member", "bugs", 1)}}
	conductor.runBundle = model.ReviewBundle{ID: "rb_incomplete", Lifecycle: model.LifecycleIncomplete, Members: []model.BundleMember{
		{Scope: "global", Profile: "bugs", ReviewID: "rp_member", Lifecycle: model.LifecycleCompleted},
		{Scope: "global", Profile: "security", Lifecycle: model.LifecyclePending},
	}}
	var stdout, stderr bytes.Buffer
	exit := executeRunWithConductor(context.Background(), conductor, runOptions{format: "json", subject: model.WorkingChanges()}, commandIO{output: &stdout, errors: &stderr})
	if exit != usageExitCode {
		t.Fatalf("exit = %d, want %d; stderr = %q", exit, usageExitCode, stderr.String())
	}
	report := decodeReport(t, stdout.String())
	requireBundleSummary(t, report, "rb_incomplete", model.LifecycleIncomplete)
	if got := findingCounts(report); !reflect.DeepEqual(got, []int{1, 0}) {
		t.Fatalf("finding counts = %v, want the member's finding inline and none for the unstarted member", got)
	}
}

func TestInspectDispatchesBundleIDsToBundleInspection(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	repository := testGitRepository(t)
	var initOut, initErr bytes.Buffer
	if exit := run(context.Background(), []string{"init", "--repo", repository}, &initOut, &initErr); exit != 0 {
		t.Fatalf("init exit = %d, stderr = %q", exit, initErr.String())
	}
	bundle := model.ReviewBundle{
		ID: "rb_dispatch", Lifecycle: model.LifecycleCompleted,
		Members: []model.BundleMember{}, Warnings: []model.BundleWarning{}, Deduplicated: []model.SkippedDuplicate{},
	}
	ledger, err := store.NewLedgerRecordStore(filepath.Join(stateHome, "review-party"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.CreateReviewBundle(bundle); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := executeInspect(context.Background(), inspectOptions{id: model.ReviewID(bundle.ID), format: "json"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("Bundle inspection exit = %d, stderr = %q", exit, stderr.String())
	}
	requireBundleSummary(t, decodeReport(t, stdout.String()), bundle.ID, bundle.Lifecycle)
}

func TestInspectReportsUnknownIDsWithoutSQLText(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repository := testGitRepository(t)
	runMainCommand(t, []string{"init", "--repo", repository})
	for _, id := range []string{"rp_1724232000000_0123456789abcdef", "rb_1724232000000_0123456789abcdef"} {
		var stdout, stderr bytes.Buffer
		exit := run(context.Background(), []string{"inspect", id}, &stdout, &stderr)
		want := "review-party: no review with id \"" + id + "\"\n"
		if exit != 1 || stderr.String() != want {
			t.Fatalf("inspect %s exit = %d, stderr = %q, want exit 1 and %q", id, exit, stderr.String(), want)
		}
	}
}
