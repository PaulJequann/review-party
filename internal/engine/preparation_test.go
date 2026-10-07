package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func TestReviewPreparationUsesCanonicalRepositoryForCompletion(t *testing.T) {
	skipPreparationSymlinkOnWindows(t)
	repository := changedTestRepository(t)
	alias := filepath.Join(t.TempDir(), "repository")
	requirePreparationNoError(t, os.Symlink(repository, alias))
	conductor := testConductor(t, successfulExecutor(cleanReview), time.Second)

	preparation, err := conductor.startReviewPreparation(alias, model.WorkingChanges())
	requirePreparationNoError(t, err)
	requirePreparationEqual(t, "canonical repository", preparation.repository, repository)
	requirePreparationNoError(t, os.Remove(alias))
	prepared, err := conductor.completeReviewPreparation(preparation)
	requirePreparationNoError(t, err)
	requirePreparationEqual(t, "prepared Subject repository", prepared.value.Repository, repository)
}

func TestReviewPreparationBuildsSharedPreparedReviews(t *testing.T) {
	conductor := testConductor(t, successfulExecutor(cleanReview), time.Second)
	instant := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	conductor.now = func() time.Time {
		instant = instant.Add(time.Millisecond)
		return instant
	}
	preparation, err := conductor.startReviewPreparation(changedTestRepository(t), model.WorkingChanges())
	requirePreparationNoError(t, err)
	prepared, err := conductor.completeReviewPreparation(preparation)
	requirePreparationNoError(t, err)
	requirePreparationEqual(t, "Subject resolution timing", prepared.subjectResolutionMS, int64(2))

	profile := compiledProfile{deadline: time.Minute}
	first := prepared.review(profile, model.ReviewTimings{ProfileCompilationMS: 7})
	second := prepared.review(profile, model.ReviewTimings{ProfileCompilationMS: 11})
	requirePreparationEqual(t, "prepared Subject identity", first.subject.Identity, second.subject.Identity)
	requirePreparationEqual(t, "first Subject timing", first.timings.SubjectResolutionMS, prepared.subjectResolutionMS)
	requirePreparationEqual(t, "second Subject timing", second.timings.SubjectResolutionMS, prepared.subjectResolutionMS)
	requirePreparationEqual(t, "first Profile timing", first.timings.ProfileCompilationMS, int64(7))
	requirePreparationEqual(t, "second Profile timing", second.timings.ProfileCompilationMS, int64(11))
	requirePreparationEqual(t, "first deadline", first.deadline, time.Minute)
	requirePreparationEqual(t, "second deadline", second.deadline, time.Minute)
}

func TestReviewPreparationFailurePrecedesReviewerLaunch(t *testing.T) {
	executor := successfulExecutor(cleanReview)
	conductor := testConductor(t, executor, time.Second)
	_, err := conductor.Review(testContext(t), model.RunSelection{
		Repository: testRepository(t), Subject: model.WorkingChanges(), Profile: "bugs",
	})
	requirePreparationErrorContains(t, err, "working changes are empty")
	requirePreparationEqual(t, "availability checks", executor.checkCount(), 0)
	requirePreparationEqual(t, "review attempts", executor.attemptCount(), 0)
}

func TestBundleMembersSharePreparedSubjectAndTiming(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}, {Profile: "code-quality"}},
		Repository:       []configuration.SelectionItem{},
	})
	instant := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	conductor.now = func() time.Time {
		instant = instant.Add(time.Millisecond)
		return instant
	}
	bundle, err := conductor.Run(testContext(t), model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})
	requirePreparationNoError(t, err)
	requirePreparationEqual(t, "bundle members", len(bundle.Members), 2)
	first, err := conductor.Inspect(context.Background(), bundle.Members[0].ReviewID)
	requirePreparationNoError(t, err)
	second, err := conductor.Inspect(context.Background(), bundle.Members[1].ReviewID)
	requirePreparationNoError(t, err)
	requirePreparationEqual(t, "first and second Subject identities", first.Subject.Identity, second.Subject.Identity)
	requirePreparationEqual(t, "bundle Subject identity", first.Subject.Identity, bundle.SubjectIdentity)
	requirePreparationEqual(t, "Subject timing", first.Timings.SubjectResolutionMS, second.Timings.SubjectResolutionMS)
	requirePreparationPositive(t, "Subject timing", first.Timings.SubjectResolutionMS)
}

func TestEvalRetryReusesPreparedSubject(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "prepared-retry"}})
	base := filepath.Join(suite, "cases", "prepared-retry", "base")
	head := filepath.Join(suite, "cases", "prepared-retry", "head")
	executions := 0
	executor := &scriptedExecutor{availability: availability{Available: true}}
	outcomes := []attemptExecution{
		failedExecution(model.AttemptTransientFailure, model.TerminationTransportFailure, model.PhaseReviewerExecution, "temporary transport failure"),
		{AssistantText: cleanReview, Outcome: model.AttemptCompleted},
	}
	executor.execute = func(context.Context, attemptSpec) attemptExecution {
		executions++
		removePreparationDirectory(t, base)
		return outcomes[executions-1]
	}
	conductor := testEvalConductor(t, executor)
	conductor.wait = func(context.Context, time.Duration) error { return nil }
	selection := model.ReviewSelection{
		Repository: suite, Subject: model.CapturedChange(base, head), Profile: "bugs",
		Reviewer: defaultReviewer, Model: "grok-code-fast-1", Effort: "high",
	}
	record, err := conductor.reviewEvalCase(testContext(t), selection, model.RetryPolicy{
		MaxAttempts: 2, InitialBackoff: "1ms", MaxBackoff: "1ms",
	})
	requirePreparationNoError(t, err)
	requirePreparationEqual(t, "record lifecycle", record.Lifecycle, model.LifecycleCompleted)
	requirePreparationEqual(t, "record attempts", record.AttemptCount(), 2)
	requirePreparationEqual(t, "review executions", executions, 2)
}

func skipPreparationSymlinkOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("directory symlinks require platform-specific privileges on Windows")
	}
}

func requirePreparationNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func requirePreparationEqual[T comparable](t *testing.T, label string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func requirePreparationErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error is nil, want %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func requirePreparationPositive(t *testing.T, label string, value int64) {
	t.Helper()
	if value <= 0 {
		t.Fatalf("%s = %d, want positive value", label, value)
	}
}

func removePreparationDirectory(t *testing.T, path string) {
	t.Helper()
	requirePreparationNoError(t, os.RemoveAll(path))
}
