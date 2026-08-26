package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestExperimentCompilationUsesFrozenExperimentDeadline(t *testing.T) {
	conductor := &Conductor{reviewers: defaultReviewerCatalog(), evalDefaultDeadline: 3 * time.Minute}
	resolved := resolvedProfile{
		name: "bugs", instructions: "Review bugs.", digest: "source", reviewer: defaultReviewer,
		model: "grok-4.5", effort: "high", deadline: time.Minute,
	}
	compiled, err := conductor.compilePreparedProfile(model.ReviewSelection{Profile: "bugs"}, resolved, true)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.deadline != 3*time.Minute {
		t.Fatalf("deadline = %s", compiled.deadline)
	}
	if compiled.revision.ExecutionDeadline != "3m0s" {
		t.Fatalf("revision deadline = %q", compiled.revision.ExecutionDeadline)
	}
}

func TestSavedProfileCompilesOneStablePass(t *testing.T) {
	library := newTestProfileLibrary(t)
	profile, err := compileTestProfile(library, "bugs", model.ReviewSubject{})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.revision.Passes) != 1 || profile.revision.Passes[0].Name != "bug-review" {
		t.Fatalf("passes = %#v", profile.revision.Passes)
	}
	if profile.revision.ReviewerID != "grok" || profile.revision.ExecutionDeadline != "1s" {
		t.Fatalf("revision = %#v", profile.revision)
	}
}

func TestSavedInstructionsReachPromptWithoutTemplateInheritance(t *testing.T) {
	library := newTestProfileLibrary(t)
	profile, err := compileTestProfile(library, "documentation", model.ReviewSubject{})
	if err != nil {
		t.Fatal(err)
	}
	prompt := profile.prompt(model.ReviewSubject{})
	if !strings.Contains(prompt, "Review documentation concerns.") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestZeroValueProfileLibraryReturnsConfigurationError(t *testing.T) {
	_, err := (profileLibrary{}).findProfile(profileLookup{name: "bugs"})
	if !errors.Is(err, errProfileLibraryNotConfigured) {
		t.Fatalf("error = %v", err)
	}
}
