package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func TestPrintBundlePreservesHumanPresentation(t *testing.T) {
	bundle := model.ReviewBundle{
		ID: "rb_test", Lifecycle: model.LifecycleIncomplete, Revision: "revision",
		SubjectKind: "working_changes", SubjectIdentity: "0123456789abcdef0123",
		Selection:   &model.BundleSelection{Kind: "repository_default", Source: "repository_selection", ConcurrencyLimit: 2, LimitSource: "party"},
		Members:     []model.BundleMember{{Scope: "global", Profile: "bugs", Lifecycle: model.LifecycleCompleted, ReviewID: "rp_test", Status: "clean"}},
		Termination: &model.BundleTermination{Category: model.TerminationCancelled, Message: "context canceled"},
	}
	var output bytes.Buffer
	if err := printBundle(&output, bundle, "human"); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{"bundle rb_test", "incomplete", "global:bugs", "inspect: review-party inspect rb_test"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output = %q, want %q", text, want)
		}
	}
}

func TestPrintBundleJSONIsTheBundleDocument(t *testing.T) {
	bundle := model.ReviewBundle{ID: "rb_json", Lifecycle: model.LifecycleCompleted}
	var output bytes.Buffer
	if err := printBundle(&output, bundle, "json"); err != nil {
		t.Fatal(err)
	}
	var decoded model.ReviewBundle
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != bundle.ID || decoded.Lifecycle != bundle.Lifecycle {
		t.Fatalf("decoded = %#v, want %#v", decoded, bundle)
	}
}

func TestExecuteRunReturnsUsageExitForIncompleteBundle(t *testing.T) {
	conductor := &fakeRunConductor{}
	conductor.runBundle = model.ReviewBundle{ID: "rb_incomplete", Lifecycle: model.LifecycleIncomplete}
	var stdout, stderr bytes.Buffer
	exit := executeRunWithConductor(context.Background(), conductor, runOptions{format: "json", subject: model.WorkingChanges()}, commandIO{output: &stdout, errors: &stderr})
	if exit != usageExitCode {
		t.Fatalf("exit = %d, want %d; stderr = %q", exit, usageExitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "rb_incomplete") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestInspectDispatchesBundleIDsToBundleInspection(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repository := testGitRepository(t)
	var initOut, initErr bytes.Buffer
	if exit := run(context.Background(), []string{"init", "--repo", repository}, &initOut, &initErr); exit != 0 {
		t.Fatalf("init exit = %d, stderr = %q", exit, initErr.String())
	}
	var stdout, stderr bytes.Buffer
	exit := executeInspect(context.Background(), inspectOptions{id: "rb_missing", format: "json"}, &stdout, &stderr)
	if exit == 0 {
		t.Fatal("missing Bundle inspection returned success")
	}
	if stderr.Len() == 0 {
		t.Fatal("Bundle inspection failure produced no diagnostic")
	}
}
