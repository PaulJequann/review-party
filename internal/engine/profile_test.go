package engine

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"strings"
	"testing"
	"time"
)

type trackingRecordStore struct {
	saves int
}

func (store *trackingRecordStore) Save(model.ReviewRecord) error {
	store.saves++
	return nil
}

func (*trackingRecordStore) Load(model.ReviewID) (model.ReviewRecord, error) {
	return model.ReviewRecord{}, errors.New("record not found")
}

func TestProfilesReturnsBuiltInsInStableOrder(t *testing.T) {
	conductor := newTestConductorWithCatalog(t, &trackingRecordStore{}, defaultReviewerCatalog(), time.Minute)

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
	conductor := newTestConductorWithCatalog(t, store, catalog, time.Minute)

	explanation, err := conductor.Explain(context.Background(), model.ProfileSelection{Profile: "documentation"})
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

func TestProfilesUseDistinctRecipesAndPrompts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	catalog := defaultReviewerCatalog()
	bugs, err := compileSelectedTestProfile(catalog, model.ProfileSelection{Profile: "bugs", Reviewer: "grok"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		profile       string
		pass          string
		promptNeedle  string
		promptAbsence string
	}{
		{name: "documentation", profile: "documentation", pass: "documentation-review", promptNeedle: "Perform a Documentation Review", promptAbsence: "Review for material bugs"},
		{name: "code-quality", profile: "code-quality", pass: "code-quality-review", promptNeedle: "code-quality reviewer", promptAbsence: "material bugs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile, err := compileSelectedTestProfile(catalog, model.ProfileSelection{Profile: test.profile, Reviewer: "grok"}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if bugs.revision.Revision == profile.revision.Revision {
				t.Fatalf("bugs and %s produced the same Profile Revision", test.profile)
			}
			if profile.revision.Passes[0].Name != test.pass {
				t.Fatalf("pass = %#v", profile.revision.Passes[0])
			}
			prompt := profile.prompt(model.ReviewSubject{Identity: "subject", Patch: "patch"})
			if !strings.Contains(prompt, test.promptNeedle) || strings.Contains(prompt, test.promptAbsence) {
				t.Fatalf("%s prompt is not distinct:\n%s", test.profile, prompt)
			}
		})
	}
}

func TestDocumentationReviewMatchesExplainedRecipe(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := newTestConductorWithCatalog(
		t,
		&trackingRecordStore{},
		catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor}),
		time.Minute,
	)
	explanation, err := conductor.Explain(context.Background(), model.ProfileSelection{Profile: "documentation"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := conductor.Review(context.Background(), model.ReviewSelection{
		Repository: repository,
		Subject:    model.WorkingChanges(),
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
		capabilities: []model.Capability{model.CapabilityRepositoryRead},
		executor:     executor,
	}
	conductor := newTestConductorWithCatalog(t, store, newReviewerCatalog([]reviewerRegistration{registration}), time.Minute)

	_, err := conductor.Review(context.Background(), model.ReviewSelection{
		Repository: "/repository-must-not-be-resolved",
		Subject:    model.WorkingChanges(),
		Profile:    "documentation",
		Reviewer:   "restricted",
	})
	var mismatch UnsupportedCapabilitiesError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want UnsupportedCapabilitiesError", err)
	}
	assertNoReviewActivity(t, store, executor)
}

func TestCompileProfileDefinitionAlwaysChecksRequiredCapabilities(t *testing.T) {
	registration := reviewerRegistration{
		candidate:    reviewerCandidate{ID: "restricted", Model: "model"},
		capabilities: restrictedReviewCapabilities(),
		executor:     successfulExecutor(cleanReview),
	}
	definition := profileDefinition{
		name:                 "synthetic",
		defaultReviewer:      "restricted",
		requiredCapabilities: append(restrictedReviewCapabilities(), model.Capability("extra-cap")),
	}

	_, err := compileProfileDefinition(
		newReviewerCatalog([]reviewerRegistration{registration}),
		model.ProfileSelection{Profile: "synthetic", Reviewer: "restricted"},
		time.Minute,
		definition,
	)
	var mismatch UnsupportedCapabilitiesError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want UnsupportedCapabilitiesError", err)
	}
	if !reflect.DeepEqual(mismatch.Missing, []model.Capability{model.Capability("extra-cap")}) {
		t.Fatalf("missing capabilities = %#v", mismatch.Missing)
	}
}

func TestEffectiveDeadlineChangesProfileRevision(t *testing.T) {
	short := newTestConductorWithCatalog(t, &trackingRecordStore{}, defaultReviewerCatalog(), time.Minute)
	long := newTestConductorWithCatalog(t, &trackingRecordStore{}, defaultReviewerCatalog(), 2*time.Minute)

	shortExplanation, err := short.Explain(context.Background(), model.ProfileSelection{Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	longExplanation, err := long.Explain(context.Background(), model.ProfileSelection{Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if shortExplanation.ProfileRevision.Revision == longExplanation.ProfileRevision.Revision {
		t.Fatal("different execution deadlines produced the same Profile Revision")
	}
}

func TestExplainPreservesExplicitReviewer(t *testing.T) {
	conductor := newTestConductorWithCatalog(t, &trackingRecordStore{}, defaultReviewerCatalog(), time.Minute)

	explanation, err := conductor.Explain(context.Background(), model.ProfileSelection{Profile: "bugs", Reviewer: "copilot"})
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ReviewerWasDefault || explanation.ProfileRevision.Reviewer.ReviewerID != "copilot" {
		t.Fatalf("explanation = %#v", explanation)
	}
}

func TestConductorRequiresExplicitProfileConfiguration(t *testing.T) {
	_, err := newConductorWithProfiles(&trackingRecordStore{}, defaultReviewerCatalog(), profileLibrary{}, time.Minute)
	if !errors.Is(err, errProfileLibraryNotConfigured) {
		t.Fatalf("error = %v, want %v", err, errProfileLibraryNotConfigured)
	}
}

func TestZeroValueProfileLibraryReturnsConfigurationError(t *testing.T) {
	_, err := (profileLibrary{}).findProfile(profileLookup{name: "bugs"})
	if !errors.Is(err, errProfileLibraryNotConfigured) {
		t.Fatalf("error = %v, want %v", err, errProfileLibraryNotConfigured)
	}
}

func newTestConductorWithCatalog(t *testing.T, store store.RecordStore, reviewers reviewerCatalog, deadline time.Duration) *Conductor {
	t.Helper()
	conductor, err := newConductorWithProfiles(store, reviewers, newProfileLibrary(t.TempDir()), deadline)
	if err != nil {
		t.Fatal(err)
	}
	return conductor
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
