package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reviewparty/internal/model"
	"testing"
	"time"
)

func TestNewDefersReviewerPolicyValidationUntilRepositoryResolution(t *testing.T) {
	personalPath := filepath.Join(t.TempDir(), "config.json")
	writeProfileConfigFixture(t, personalPath, `{
  "schema_version": 1,
  "reviewers": {"grok": {"allowed_models": ["repo-model"]}}
}`)
	repository := t.TempDir()
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{
  "schema_version": 1,
  "reviewers": {"grok": {"model": "repo-model"}}
}`)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	conductor, err := New(Config{UserConfigurationPath: personalPath})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := conductor.compileFilesystemProfile(model.ProfileSelection{Profile: "bugs"}, repository)
	if err != nil {
		t.Fatal(err)
	}
	if profile.reviewer.candidate.Model != "repo-model" {
		t.Fatalf("model = %q, want repository model", profile.reviewer.candidate.Model)
	}
}

func TestExplicitReviewerUsesResolvedRepositoryConfiguration(t *testing.T) {
	repository := changedTestRepository(t)
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{
  "schema_version": 1,
  "reviewers": {"grok": {"enabled": false}}
}`)

	assertDisabledReviewerReview(t, model.ReviewSelection{
		Repository: repository,
		Subject:    model.WorkingChanges(),
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

	assertDisabledReviewerReview(t, model.ReviewSelection{
		Repository: repository,
		Subject:    model.WorkingChanges(),
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

	assertDisabledReviewerReview(t, model.ReviewSelection{
		Repository: repository,
		Subject:    model.CapturedChange(base, head),
		Profile:    "bugs",
		Reviewer:   "grok",
	})
}

func assertDisabledReviewerReview(t *testing.T, selection model.ReviewSelection) {
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
