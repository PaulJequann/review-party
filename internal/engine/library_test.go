package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func TestRepositoryProfileShadowsGlobalAsCompleteDefinition(t *testing.T) {
	repository := changedTestRepository(t)
	library := newProfileLibrary(t.TempDir())
	publishTestProfile(t, library.manager(), configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "GLOBAL GUIDANCE\n",
	})
	plan, err := library.manager().PlanProfileCreation(configuration.Repository(repository), configuration.ProfileDraft{
		Target: configuration.ScopeRepository, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "REPOSITORY GUIDANCE\n",
	})
	if err != nil || !plan.Valid() {
		t.Fatalf("plan error = %v, reason = %q", err, plan.Reason())
	}
	if err := library.manager().Publish(plan); err != nil {
		t.Fatal(err)
	}
	profile, err := compileTestProfile(library, "bugs", model.ReviewSubject{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	if profile.revision.Source != "repository:.reviewparty/profiles/bugs" || !strings.Contains(profile.prompt(model.ReviewSubject{}), "REPOSITORY GUIDANCE") {
		t.Fatalf("revision = %#v", profile.revision)
	}
}

func TestTemplateCannotResolveAsExecutableProfile(t *testing.T) {
	library := newProfileLibrary(t.TempDir())
	_, err := compileTestProfile(library, "bugs", model.ReviewSubject{})
	var unknown UnknownProfileError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %v", err)
	}
}

func TestUnknownProfileReportsInvalidAuthoredNames(t *testing.T) {
	root := t.TempDir()
	library := newProfileLibrary(root)
	directory := filepath.Join(root, "profiles", "broken")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), []byte("INVALID"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := compileTestProfile(library, "missing", model.ReviewSubject{})
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
	library := newProfileLibrary(t.TempDir())
	publishTestProfile(t, library.manager(), configuration.ProfileDraft{
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
	if _, err := compileTestProfile(library, "security", model.ReviewSubject{Repository: repository}); err == nil {
		t.Fatal("invalid Repository Profile fell back to Global")
	}
}

func compileTestProfile(library profileLibrary, name string, subject model.ReviewSubject) (compiledProfile, error) {
	conductor := Conductor{reviewers: defaultReviewerCatalog(), profiles: library, evalDefaultDeadline: time.Minute}
	resolved, err := library.resolve(profileRequest{repository: subject.Repository, name: name})
	if err != nil {
		return compiledProfile{}, err
	}
	return conductor.compileResolvedProfile(resolved)
}
