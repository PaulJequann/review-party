package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepositoryProfileShadowsGlobalAsWholeDefinition(t *testing.T) {
	repository := changedTestRepository(t)
	globalDirectory := t.TempDir()
	writeProfileFixture(t, filepath.Join(globalDirectory, "profiles", "bugs.md"), "GLOBAL UNIQUE GUIDANCE")
	writeProfileFixture(t, filepath.Join(repository, ".reviewparty", "profiles", "bugs.md"), "REPOSITORY UNIQUE GUIDANCE")

	profile, err := compileTestProfile(newProfileLibrary(globalDirectory), "bugs", "grok", ReviewSubject{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	prompt := profile.prompt(ReviewSubject{Repository: repository})
	if !strings.Contains(prompt, "REPOSITORY UNIQUE GUIDANCE") || strings.Contains(prompt, "GLOBAL UNIQUE GUIDANCE") {
		t.Fatalf("compiled prompt did not use the repository definition as a whole:\n%s", prompt)
	}
	assertPromptOmits(t, prompt, "Report a pre-existing defect only when", "Consolidate duplicate symptoms")
	assertPromptContains(t, prompt, "Use repository-scoped read and search tools only", "BEGIN_REVIEW", "Review Subject identity:")
	if profile.revision.Source != "repository:.reviewparty/profiles/bugs.md" {
		t.Fatalf("source = %q", profile.revision.Source)
	}
	if profile.revision.Passes[0].PromptRevision == "bugs-v4" || profile.revision.Purpose == "Find material defects in the Review Subject." {
		t.Fatalf("repository override retained packaged recipe metadata: %#v", profile.revision)
	}
}

func TestGlobalDefaultsSelectProfileAndReviewer(t *testing.T) {
	repository := changedTestRepository(t)
	globalDirectory := t.TempDir()
	writeProfileConfigFixture(t, filepath.Join(globalDirectory, "config.json"), `{"schema":1,"defaultProfile":"security","defaultReviewer":"copilot"}`)
	writeProfileFixture(t, filepath.Join(globalDirectory, "profiles", "security.md"), "GLOBAL SECURITY GUIDANCE")

	grok := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(context.Context, attemptSpec) attemptExecution {
			t.Fatal("grok ran despite the global reviewer default")
			return attemptExecution{}
		},
	}
	copilot := successfulExecutor(cleanReview)
	store, err := newLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	conductor := newConductorWithProfiles(store, catalogWithExecutors(map[string]attemptExecutor{
		"grok":    grok,
		"copilot": copilot,
	}), newProfileLibrary(globalDirectory), time.Second)

	record, err := conductor.Review(context.Background(), ReviewSelection{Repository: repository, Subject: WorkingChanges()})
	if err != nil {
		t.Fatal(err)
	}
	assertGlobalDefaultReview(t, record, grok, copilot)
}

func TestInvalidRepositoryProfileFailsBeforeLaunchWithoutFallback(t *testing.T) {
	repository := changedTestRepository(t)
	globalDirectory := t.TempDir()
	writeProfileConfigFixture(t, filepath.Join(repository, ".reviewparty", "config.json"), `{"schema":1,"defaultProfile":"security"}`)
	writeProfileFixture(t, filepath.Join(repository, ".reviewparty", "profiles", "security.md"), "   \n")
	writeProfileFixture(t, filepath.Join(globalDirectory, "profiles", "security.md"), "VALID GLOBAL FALLBACK THAT MUST NOT RUN")
	executor := successfulExecutor(cleanReview)
	store, err := newLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	conductor := newConductorWithProfiles(store, catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor}), newProfileLibrary(globalDirectory), time.Second)

	_, err = conductor.Review(context.Background(), ReviewSelection{Repository: repository, Subject: WorkingChanges()})
	if err == nil {
		t.Fatalf("error = %v", err)
	}
	for _, expected := range []string{"repository:.reviewparty/profiles/security.md", "is empty"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not contain %q", err, expected)
		}
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want zero", executor.attemptCount())
	}
}

func assertGlobalDefaultReview(t *testing.T, record ReviewRecord, grok, copilot *scriptedExecutor) {
	t.Helper()
	if record.ProfileRevision.Name != "security" || record.ProfileRevision.ReviewerID != "copilot" {
		t.Fatalf("revision = %#v", record.ProfileRevision)
	}
	if record.ProfileRevision.Source != "global:profiles/security.md" || !strings.Contains(record.ProfileSnapshot.Instructions, "GLOBAL SECURITY GUIDANCE") {
		t.Fatalf("profile provenance = %#v, snapshot = %#v", record.ProfileRevision, record.ProfileSnapshot)
	}
	if grok.attemptCount() != 0 || copilot.attemptCount() != 1 {
		t.Fatalf("grok attempts = %d, copilot attempts = %d", grok.attemptCount(), copilot.attemptCount())
	}
}

func TestRepositoryProfileSymlinkFailsWithoutFallback(t *testing.T) {
	repository := testRepository(t)
	globalDirectory := t.TempDir()
	globalPath := filepath.Join(globalDirectory, "profiles", "security.md")
	writeProfileFixture(t, globalPath, "GLOBAL FALLBACK THAT MUST NOT RUN")
	repositoryPath := filepath.Join(repository, ".reviewparty", "profiles", "security.md")
	if err := os.MkdirAll(filepath.Dir(repositoryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(globalPath, repositoryPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := compileTestProfile(newProfileLibrary(globalDirectory), "security", "grok", ReviewSubject{Repository: repository})
	if err == nil || !strings.Contains(err.Error(), "must be a regular file") {
		t.Fatalf("error = %v", err)
	}
}

func TestMissingProfileExplainsSearchAndAvailableNames(t *testing.T) {
	repository := testRepository(t)
	globalDirectory := t.TempDir()
	writeProfileFixture(t, filepath.Join(globalDirectory, "profiles", "security.md"), "Security guidance")

	_, err := compileTestProfile(newProfileLibrary(globalDirectory), "architecture", "grok", ReviewSubject{Repository: repository})
	if err == nil {
		t.Fatal("missing Profile compiled")
	}
	for _, expected := range []string{
		"repository:.reviewparty/profiles/architecture.md",
		"global:profiles/architecture.md",
		"packaged:profiles/architecture.md",
		"available: bugs, documentation, security",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not contain %q", err, expected)
		}
	}
}

func TestProfileConfigRejectsUnknownFieldsAndUnsafeNames(t *testing.T) {
	for name, payload := range map[string]string{
		"unknown field": `{"schema":1,"defaultProfil":"bugs"}`,
		"unsafe name":   `{"schema":1,"defaultProfile":"../bugs"}`,
	} {
		t.Run(name, func(t *testing.T) {
			globalDirectory := t.TempDir()
			writeProfileConfigFixture(t, filepath.Join(globalDirectory, "config.json"), payload)
			_, err := compileTestProfile(newProfileLibrary(globalDirectory), "", "", ReviewSubject{})
			if err == nil || !strings.Contains(err.Error(), "profile config") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProfileRevisionChangesWithMarkdown(t *testing.T) {
	repository := testRepository(t)
	path := filepath.Join(repository, ".reviewparty", "profiles", "bugs.md")
	writeProfileFixture(t, path, "FIRST GUIDANCE")
	library := profileLibrary{}
	first, err := compileTestProfile(library, "bugs", "grok", ReviewSubject{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("SECOND GUIDANCE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := compileTestProfile(library, "bugs", "grok", ReviewSubject{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	if first.revision.Revision == second.revision.Revision || first.revision.SourceDigest == second.revision.SourceDigest {
		t.Fatalf("first = %#v, second = %#v", first.revision, second.revision)
	}
}

func TestPackagedBugsProfileRemainsZeroConfigurationDefault(t *testing.T) {
	profile, err := compileTestProfile(profileLibrary{}, "", "", ReviewSubject{})
	if err != nil {
		t.Fatal(err)
	}
	if profile.revision.Name != "bugs" || profile.revision.Source != "packaged:profiles/bugs.md" {
		t.Fatalf("revision = %#v", profile.revision)
	}
	assertPromptContains(t, profile.prompt(ReviewSubject{}),
		"Report only the highest-risk Findings that could justify changing or delaying",
		"Report a pre-existing defect only when",
		"Identify a concrete failing path or violated invariant",
		"For a test gap, explain the false-green or regression",
		"Consolidate duplicate symptoms under their root cause",
		"Drop speculative, low-confidence, purely stylistic",
	)
}

func assertPromptContains(t *testing.T, prompt string, expected ...string) {
	t.Helper()
	for _, fragment := range expected {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("compiled prompt omits %q:\n%s", fragment, prompt)
		}
	}
}

func assertPromptOmits(t *testing.T, prompt string, unexpected ...string) {
	t.Helper()
	for _, fragment := range unexpected {
		if strings.Contains(prompt, fragment) {
			t.Fatalf("compiled prompt silently includes %q:\n%s", fragment, prompt)
		}
	}
}

func writeProfileFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, content)
}

func writeProfileConfigFixture(t *testing.T, path, content string) {
	t.Helper()
	writeProfileFixture(t, path, content)
}

func compileTestProfile(library profileLibrary, name, reviewer string, subject ReviewSubject) (compiledProfile, error) {
	conductor := Conductor{reviewers: defaultReviewerCatalog(), profiles: library, attemptDeadline: 10 * time.Minute}
	return conductor.compileFilesystemProfile(ProfileSelection{Profile: name, Reviewer: reviewer}, subject.Repository)
}

func compileSelectedTestProfile(catalog reviewerCatalog, selection ProfileSelection, deadline time.Duration) (compiledProfile, error) {
	conductor := Conductor{reviewers: catalog, profiles: profileLibrary{}, attemptDeadline: deadline}
	return conductor.compileFilesystemProfile(selection, "")
}
