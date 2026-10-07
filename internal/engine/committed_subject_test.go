package engine

import (
	"context"
	"os"
	"path/filepath"
	"reviewparty/internal/hostrun"
	"reviewparty/internal/model"
	"strings"
	"testing"
	"time"
)

func TestCommittedReviewExecutesAtRecordedHeadInARuntimeView(t *testing.T) {
	repository, base, head := committedReviewFixture(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"caller-mutation\"\n")
	if err := os.WriteFile(filepath.Join(repository, ".env"), []byte("UNRELATED_SECRET=caller\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var executionPath string
	executor := &scriptedExecutor{availability: availability{Available: true}, execute: func(_ context.Context, spec attemptSpec) attemptExecution {
		executionPath = spec.Repository
		assertCommittedExecutionView(t, spec.Repository)
		return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
	}}
	run := testRun(t)
	record, err := testConductor(t, executor, time.Second).Review(hostrun.WithRun(context.Background(), run), model.RunSelection{Repository: repository, Subject: model.CommittedRange(base, head), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	assertCommittedRecord(t, record, base, head)
	assertRuntimeView(t, run, executionPath)
	caller, err := os.ReadFile(filepath.Join(repository, "review.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(caller), "caller-mutation") {
		t.Fatalf("caller changed: %q", caller)
	}
}

func TestInvalidCommittedRangePreventsHarnessLaunch(t *testing.T) {
	repository := testRepository(t)
	executor := successfulExecutor(cleanReview)
	_, err := testConductor(t, executor, time.Second).Review(testContext(t), model.RunSelection{Repository: repository, Subject: model.CommittedRange("missing-base", "HEAD"), Profile: "bugs"})
	if err == nil {
		t.Fatal("expected invalid base error")
	}
	if executor.checkCount() != 0 {
		t.Fatalf("checks = %d", executor.checkCount())
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d", executor.attemptCount())
	}
}

func TestCommittedReviewViewOutlivesIncompleteAttemptsUntilTheRunCloses(t *testing.T) {
	cases := map[string]func(context.Context) attemptExecution{
		"launch failure": func(context.Context) attemptExecution {
			return failedExecution(model.AttemptReviewerUnavailable, model.TerminationReviewerUnavailable, model.PhaseHarnessLaunch, "launch failed")
		},
		"deadline": func(ctx context.Context) attemptExecution { <-ctx.Done(); return contextExecution(ctx.Err()) },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) { assertIncompleteViewRetained(t, run) })
	}
}

func committedReviewFixture(t *testing.T) (repository, base, head string) {
	t.Helper()
	repository = testRepository(t)
	base = strings.TrimSpace(runTestCommandOutput(t, repository, "git", "rev-parse", "HEAD"))
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"recorded-head\"\n")
	runTestCommand(t, repository, "git", "add", "review.go")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: recorded head")
	head = strings.TrimSpace(runTestCommandOutput(t, repository, "git", "rev-parse", "HEAD"))
	return repository, base, head
}

func assertIncompleteViewRetained(t *testing.T, execute func(context.Context) attemptExecution) {
	t.Helper()
	repository, base, _ := committedReviewFixture(t)
	var executionPath string
	executor := &scriptedExecutor{availability: availability{Available: true}, execute: func(ctx context.Context, spec attemptSpec) attemptExecution {
		executionPath = spec.Repository
		return execute(ctx)
	}}
	run := testRun(t)
	// The attempt deadline also bounds building the view, so it must leave
	// room for git on a slow runner before the Reviewer can start.
	record, err := testConductor(t, executor, time.Second).Review(hostrun.WithRun(context.Background(), run), model.RunSelection{Repository: repository, Subject: model.CommittedRange(base, "HEAD"), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("lifecycle = %s", record.Lifecycle)
	}
	assertRuntimeView(t, run, executionPath)
}

// assertRuntimeView checks that the Reviewer read a view inside the run's
// directory, that it is still there after the Review, and that closing the
// run removes it.
func assertRuntimeView(t *testing.T, run *hostrun.Run, executionPath string) {
	t.Helper()
	runDir, err := run.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if relative, err := filepath.Rel(filepath.Dir(runDir), executionPath); err != nil || strings.HasPrefix(relative, "..") {
		t.Fatalf("execution path %s is outside the run directory %s", executionPath, filepath.Dir(runDir))
	}
	if _, err := os.Stat(executionPath); err != nil {
		t.Fatalf("view removed before the run closed: %v", err)
	}
	run.Close()
	assertPathAbsent(t, executionPath)
}

func assertCommittedExecutionView(t *testing.T, repository string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repository, "review.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "recorded-head") {
		t.Fatalf("execution content = %q", content)
	}
	assertPathAbsent(t, filepath.Join(repository, ".env"))
}

func assertCommittedRecord(t *testing.T, record model.ReviewRecord, base, head string) {
	t.Helper()
	if record.Subject.BaseObject != base {
		t.Fatalf("base = %s, want %s", record.Subject.BaseObject, base)
	}
	if record.Subject.HeadObject != head {
		t.Fatalf("head = %s, want %s", record.Subject.HeadObject, head)
	}
	if record.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("lifecycle = %s", record.Lifecycle)
	}
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %s remains: %v", path, err)
	}
}
