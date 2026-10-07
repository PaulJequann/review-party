package engine

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func TestRepositoryProfileShadowsGlobalAsCompleteDefinition(t *testing.T) {
	repository := changedTestRepository(t)
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"grok", "opencode", "copilot", "codex"}})
	publishTestProfile(t, manager, configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "GLOBAL GUIDANCE\n",
	})
	publishRepositoryTestProfile(t, manager, repository, configuration.ProfileDraft{
		Target: configuration.ScopeRepository, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "REPOSITORY GUIDANCE\n",
	})
	profile, err := compileTestProfile(manager, "bugs", model.ReviewSubject{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	if profile.revision.Source != "repository:.reviewparty/profiles/bugs" || !strings.Contains(profile.prompt(model.ReviewSubject{}), "REPOSITORY GUIDANCE") {
		t.Fatalf("revision = %#v", profile.revision)
	}
}

func TestTemplateCannotResolveAsExecutableProfile(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"grok", "opencode", "copilot", "codex"}})
	_, err := compileTestProfile(manager, "bugs", model.ReviewSubject{})
	var unknown UnknownProfileError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %v", err)
	}
}

func TestUnknownProfileWithNoAuthoredProfilesSaysSo(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}})
	_, err := compileTestProfile(manager, "missing", model.ReviewSubject{})
	if err == nil || err.Error() != `unknown review profile "missing"; no Profiles are configured` {
		t.Fatalf("error = %v", err)
	}
}

func TestUnknownProfileReportsInvalidAuthoredNames(t *testing.T) {
	root := t.TempDir()
	manager := configuration.NewManager(configuration.Options{GlobalRoot: root, Reviewers: []string{"grok", "opencode", "copilot", "codex"}})
	directory := filepath.Join(root, "profiles", "broken")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), []byte("INVALID"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := compileTestProfile(manager, "missing", model.ReviewSubject{})
	var unknown UnknownProfileError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %v", err)
	}
	if len(unknown.Available) != 1 {
		t.Fatalf("available = %#v", unknown.Available)
	}
	if unknown.Available[0] != "broken" {
		t.Fatalf("available = %#v", unknown.Available)
	}
}

func TestInvalidProfileMetadataFailsWithoutFallback(t *testing.T) {
	repository := changedTestRepository(t)
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"grok", "opencode", "copilot", "codex"}})
	publishTestProfile(t, manager, configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "security", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "GLOBAL\n",
	})
	directory := filepath.Join(repository, ".reviewparty", "profiles", "security")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), []byte(`{"schema_version":1,"name":"security"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "instructions.md"), []byte("REPOSITORY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := compileTestProfile(manager, "security", model.ReviewSubject{Repository: repository}); err == nil {
		t.Fatal("invalid Repository Profile fell back to Global")
	}
}

func TestScopedProfileMissListsOnlyThatScopesProfiles(t *testing.T) {
	repository := changedTestRepository(t)
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"grok"}})
	publishTestProfile(t, manager, configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "GLOBAL\n",
	})
	publishRepositoryTestProfile(t, manager, repository, configuration.ProfileDraft{
		Target: configuration.ScopeRepository, Name: "local", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "LOCAL\n",
	})
	_, err := compileTestProfile(manager, "repository:bugs", model.ReviewSubject{Repository: repository})
	want := `unknown review profile "bugs" in repository Configuration; expected local`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func publishRepositoryTestProfile(t *testing.T, manager *configuration.Manager, repository string, draft configuration.ProfileDraft) {
	t.Helper()
	plan, err := manager.PlanProfileCreation(configuration.Repository(repository), draft)
	if err != nil || !plan.Valid() {
		t.Fatalf("plan error = %v, reason = %q", err, plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func compileTestProfile(manager *configuration.Manager, name string, subject model.ReviewSubject) (compiledProfile, error) {
	conductor, err := newConductorWithManager(nil, defaultReviewerCatalog(), manager, time.Minute)
	if err != nil {
		return compiledProfile{}, err
	}
	resolved, err := conductor.resolveProfile(profileRequest{repository: subject.Repository, name: name})
	if err != nil {
		return compiledProfile{}, err
	}
	return conductor.compileResolvedProfile(resolved)
}

func TestPackagedBaselineIsTheFourTemplatesAndPlansAsProfiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	options := reviewPartyConfigurationOptions()
	options.GlobalRoot = t.TempDir()
	manager := configuration.NewManager(options)
	baseline, err := manager.Baseline("")
	if err != nil {
		t.Fatal(err)
	}
	if names := configuration.BaselineNames(baseline.Members); !slices.Equal(names, []string{"bugs", "code-quality", "documentation", "test-audit"}) {
		t.Fatalf("baseline = %v", names)
	}
	plan, err := manager.PlanBaselineProfiles("", configuration.ProfileExecution{Reviewer: "grok", Model: "grok-4.5", ReasoningEffort: "high", AttemptDeadline: "8m"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("baseline Profiles plan invalid: %s", plan.Reason())
	}
	if changes := plan.Changes(); len(changes) != len(baseline.Members) {
		t.Fatalf("changes = %v", changes)
	}
}
