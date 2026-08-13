package engine

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"
)

type trackingRecordStore struct {
	saves int
}

func (store *trackingRecordStore) Save(ReviewRecord) error {
	store.saves++
	return nil
}

func (*trackingRecordStore) Load(ReviewID) (ReviewRecord, error) {
	return ReviewRecord{}, errors.New("record not found")
}

func TestProfilesReturnsBuiltInsInStableOrder(t *testing.T) {
	conductor := newConductorWithCatalog(&trackingRecordStore{}, defaultReviewerCatalog(), time.Minute)

	profiles, err := conductor.Profiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{profiles[0].Name, profiles[1].Name, profiles[2].Name}
	if !reflect.DeepEqual(names, SupportedProfiles()) {
		t.Fatalf("profiles = %#v", names)
	}
	for _, profile := range profiles {
		if profile.DefaultReviewer.ReviewerID != defaultReviewer {
			t.Fatalf("default Reviewer = %#v", profile.DefaultReviewer)
		}
	}
}

func TestPackagedProfilesMatchBuiltInDefinitions(t *testing.T) {
	paths, err := fs.Glob(packagedProfileFiles, "profiles/*.md")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(paths))
	for _, profilePath := range paths {
		name := strings.TrimPrefix(profilePath, "profiles/")
		names = append(names, strings.TrimSuffix(name, ".md"))
	}
	if !reflect.DeepEqual(names, SupportedProfiles()) {
		t.Fatalf("packaged profiles = %#v, definitions = %#v", names, SupportedProfiles())
	}
}

func TestExplainCompilesWithoutStartingReview(t *testing.T) {
	store := &trackingRecordStore{}
	executor := successfulExecutor(cleanReview)
	catalog := catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor})
	conductor := newConductorWithCatalog(store, catalog, time.Minute)

	explanation, err := conductor.Explain(context.Background(), ProfileSelection{Profile: "documentation"})
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ProfileRevision.Name != "documentation" {
		t.Fatalf("profile = %q", explanation.ProfileRevision.Name)
	}
	if !explanation.ReviewerWasDefault {
		t.Fatal("reviewer was not reported as the default")
	}
	assertNoReviewActivity(t, store, executor)
}

func TestDocumentationReviewUsesDistinctRecipeAndPrompt(t *testing.T) {
	catalog := defaultReviewerCatalog()
	bugs, err := compileSelectedTestProfile(catalog, ProfileSelection{Profile: "bugs", Reviewer: "grok"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	documentation, err := compileSelectedTestProfile(catalog, ProfileSelection{Profile: "documentation", Reviewer: "grok"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if bugs.revision.Revision == documentation.revision.Revision {
		t.Fatal("bugs and documentation produced the same Profile Revision")
	}
	if documentation.revision.Passes[0].Name != "documentation-review" {
		t.Fatalf("pass = %#v", documentation.revision.Passes[0])
	}
	prompt := documentation.prompt(ReviewSubject{Identity: "subject", Patch: "patch"})
	if !strings.Contains(prompt, "Perform a Documentation Review") || strings.Contains(prompt, "Review for material bugs") {
		t.Fatalf("documentation prompt is not distinct:\n%s", prompt)
	}
}

func TestCodeQualityReviewUsesDistinctRecipeAndPrompt(t *testing.T) {
	catalog := defaultReviewerCatalog()
	bugs, err := compileSelectedTestProfile(catalog, ProfileSelection{Profile: "bugs", Reviewer: "grok"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	quality, err := compileSelectedTestProfile(catalog, ProfileSelection{Profile: "code-quality", Reviewer: "grok"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if bugs.revision.Revision == quality.revision.Revision {
		t.Fatal("bugs and code-quality produced the same Profile Revision")
	}
	if quality.revision.Passes[0].Name != "code-quality-review" {
		t.Fatalf("pass = %#v", quality.revision.Passes[0])
	}
	prompt := quality.prompt(ReviewSubject{Identity: "subject", Patch: "patch"})
	if !strings.Contains(prompt, "code-quality reviewer") || strings.Contains(prompt, "material bugs") {
		t.Fatalf("code-quality prompt is not distinct:\n%s", prompt)
	}
}

func TestDocumentationReviewMatchesExplainedRecipe(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := newConductorWithCatalog(
		&trackingRecordStore{},
		catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor}),
		time.Minute,
	)
	explanation, err := conductor.Explain(context.Background(), ProfileSelection{Profile: "documentation"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := conductor.Review(context.Background(), ReviewSelection{
		Repository: repository,
		Subject:    WorkingChanges(),
		Profile:    "documentation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(record.ProfileRevision, explanation.ProfileRevision) {
		t.Fatalf("record recipe differs from explanation\nrecord: %#v\nexplanation: %#v", record.ProfileRevision, explanation.ProfileRevision)
	}
	if record.Passes[0].Name != "documentation-review" || !strings.Contains(executor.lastAttempt().Prompt, "Perform a Documentation Review") {
		t.Fatalf("record = %#v, prompt = %q", record, executor.lastAttempt().Prompt)
	}
}

func TestCapabilityMismatchPreventsReviewLifecycle(t *testing.T) {
	store := &trackingRecordStore{}
	executor := successfulExecutor(cleanReview)
	registration := reviewerRegistration{
		candidate:    reviewerCandidate{ID: "restricted"},
		capabilities: []Capability{CapabilityRepositoryRead},
		executor:     executor,
	}
	conductor := newConductorWithCatalog(store, newReviewerCatalog([]reviewerRegistration{registration}), time.Minute)

	_, err := conductor.Review(context.Background(), ReviewSelection{
		Repository: "/repository-must-not-be-resolved",
		Subject:    WorkingChanges(),
		Profile:    "documentation",
		Reviewer:   "restricted",
	})
	var mismatch UnsupportedCapabilitiesError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want UnsupportedCapabilitiesError", err)
	}
	assertNoReviewActivity(t, store, executor)
}

func TestEffectiveDeadlineChangesProfileRevision(t *testing.T) {
	short := newConductorWithCatalog(&trackingRecordStore{}, defaultReviewerCatalog(), time.Minute)
	long := newConductorWithCatalog(&trackingRecordStore{}, defaultReviewerCatalog(), 2*time.Minute)

	shortExplanation, err := short.Explain(context.Background(), ProfileSelection{Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	longExplanation, err := long.Explain(context.Background(), ProfileSelection{Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if shortExplanation.ProfileRevision.Revision == longExplanation.ProfileRevision.Revision {
		t.Fatal("different execution deadlines produced the same Profile Revision")
	}
}

func TestExplainPreservesExplicitReviewer(t *testing.T) {
	conductor := newConductorWithCatalog(&trackingRecordStore{}, defaultReviewerCatalog(), time.Minute)

	explanation, err := conductor.Explain(context.Background(), ProfileSelection{Profile: "bugs", Reviewer: "copilot"})
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ReviewerWasDefault || explanation.ProfileRevision.Reviewer.ReviewerID != "copilot" {
		t.Fatalf("explanation = %#v", explanation)
	}
}

func assertNoReviewActivity(t *testing.T, store *trackingRecordStore, executor *scriptedExecutor) {
	t.Helper()
	if store.saves != 0 {
		t.Fatalf("saves = %d, want 0", store.saves)
	}
	if executor.checkCount() != 0 {
		t.Fatalf("checks = %d, want 0", executor.checkCount())
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want 0", executor.attemptCount())
	}
}
