package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommittedReviewExecutesAtRecordedHeadAndCleansCheckout(t *testing.T) {
	repository, base, head := committedReviewFixture(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"caller-mutation\"\n")
	if err := os.WriteFile(filepath.Join(repository, ".env"), []byte("UNRELATED_SECRET=caller\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var executionPath string
	executor := &scriptedExecutor{availability: availability{Available: true}, execute: func(_ context.Context, spec attemptSpec) attemptExecution {
		executionPath = spec.Repository
		assertCommittedExecutionView(t, spec.Repository)
		return attemptExecution{AssistantText: cleanReview, Outcome: AttemptCompleted}
	}}
	record, err := testConductor(t, executor, time.Second).Review(context.Background(), ReviewSelection{Repository: repository, Subject: CommittedRange(base, head), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	assertCommittedRecord(t, record, base, head)
	assertPathAbsent(t, executionPath)
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
	_, err := testConductor(t, executor, time.Second).Review(context.Background(), ReviewSelection{Repository: repository, Subject: CommittedRange("missing-base", "HEAD"), Profile: "bugs"})
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

func TestCommittedReviewCleansCheckoutAfterIncompleteAndDeadline(t *testing.T) {
	cases := map[string]func(context.Context) attemptExecution{
		"launch failure": func(context.Context) attemptExecution {
			return failedExecution(AttemptReviewerUnavailable, TerminationReviewerUnavailable, PhaseHarnessLaunch, "launch failed")
		},
		"deadline": func(ctx context.Context) attemptExecution { <-ctx.Done(); return contextExecution(ctx.Err()) },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) { assertIncompleteCheckoutCleanup(t, run) })
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

func assertIncompleteCheckoutCleanup(t *testing.T, run func(context.Context) attemptExecution) {
	t.Helper()
	repository, base, _ := committedReviewFixture(t)
	var executionPath string
	executor := &scriptedExecutor{availability: availability{Available: true}, execute: func(ctx context.Context, spec attemptSpec) attemptExecution {
		executionPath = spec.Repository
		return run(ctx)
	}}
	record, err := testConductor(t, executor, 10*time.Millisecond).Review(context.Background(), ReviewSelection{Repository: repository, Subject: CommittedRange(base, "HEAD"), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != LifecycleIncomplete {
		t.Fatalf("lifecycle = %s", record.Lifecycle)
	}
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

func assertCommittedRecord(t *testing.T, record ReviewRecord, base, head string) {
	t.Helper()
	if record.Subject.BaseObject != base {
		t.Fatalf("base = %s, want %s", record.Subject.BaseObject, base)
	}
	if record.Subject.HeadObject != head {
		t.Fatalf("head = %s, want %s", record.Subject.HeadObject, head)
	}
	if record.Lifecycle != LifecycleCompleted {
		t.Fatalf("lifecycle = %s", record.Lifecycle)
	}
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %s remains: %v", path, err)
	}
}
