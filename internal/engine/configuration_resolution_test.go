package engine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestExplicitReviewerUsesResolvedRepositoryConfiguration(t *testing.T) {
	repository := changedTestRepository(t)
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{
  "schema_version": 1,
  "reviewers": {"grok": {"enabled": false}}
}`)

	previousResolver := resolveRepositoryRoot
	resolveRepositoryRoot = func(string) (string, error) { return repository, nil }
	t.Cleanup(func() { resolveRepositoryRoot = previousResolver })

	assertDisabledReviewerReview(t, ReviewSelection{
		Repository: "relative/repository",
		Subject:    WorkingChanges(),
		Profile:    "bugs",
		Reviewer:   defaultReviewer,
	})
}

func TestExplicitReviewerValidationPrecedesRepositoryError(t *testing.T) {
	repository := t.TempDir()
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{
  "schema_version": 1,
  "reviewers": {"grok": {"enabled": false}}
}`)

	previousResolver := resolveRepositoryRoot
	resolveRepositoryRoot = func(string) (string, error) { return "", errors.New("repository root unavailable") }
	t.Cleanup(func() { resolveRepositoryRoot = previousResolver })

	assertDisabledReviewerReview(t, ReviewSelection{
		Repository: repository,
		Subject:    WorkingChanges(),
		Profile:    "bugs",
		Reviewer:   defaultReviewer,
	})
}

func TestCapturedChangeUsesRepositoryForProfileAndConfiguration(t *testing.T) {
	repository := t.TempDir()
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{
  "schema_version": 1,
  "reviewers": {"grok": {"enabled": false}}
}`)
	writeProfileFixture(t, filepath.Join(repository, ".reviewparty", "profiles", "bugs.md"), "REPOSITORY CAPTURED PROFILE")
	base := t.TempDir()
	head := t.TempDir()
	writeTestFile(t, filepath.Join(base, "review.go"), "package demo\n\nconst state = \"base\"\n")
	writeTestFile(t, filepath.Join(head, "review.go"), "package demo\n\nconst state = \"head\"\n")

	assertDisabledReviewerReview(t, ReviewSelection{
		Repository: repository,
		Subject:    CapturedChange(base, head),
		Profile:    "bugs",
		Reviewer:   "grok",
	})
}

func assertDisabledReviewerReview(t *testing.T, selection ReviewSelection) {
	t.Helper()
	store := &trackingRecordStore{}
	executor := successfulExecutor(cleanReview)
	conductor := newTestConductorWithCatalog(t, store, catalogWithExecutors(map[string]attemptExecutor{
		selection.Reviewer: executor,
	}), time.Minute)

	_, err := conductor.Review(context.Background(), selection)
	var disabled DisabledReviewerError
	if !errors.As(err, &disabled) {
		t.Fatalf("error = %v, want DisabledReviewerError", err)
	}
	assertNoReviewActivity(t, store, executor)
}
