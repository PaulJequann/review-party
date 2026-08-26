package engine

import (
	"errors"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

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
