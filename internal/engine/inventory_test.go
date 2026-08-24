package engine

import (
	"path/filepath"
	"reviewparty/internal/model"
	"strings"
	"testing"
)

func TestMissingProfileErrorIsNotReplacedByInvalidPeerProfile(t *testing.T) {
	repository := testRepository(t)
	writeProfileFixture(t, filepath.Join(repository, ".reviewparty", "profiles", "broken.md"), "  \n")

	_, err := compileTestProfile(newProfileLibrary(t.TempDir()), "architecture", "grok", model.ReviewSubject{Repository: repository})
	if err == nil || !strings.Contains(err.Error(), `profile "architecture" was not found`) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "is empty") {
		t.Fatalf("unrelated invalid profile replaced missing-profile error: %v", err)
	}
	if strings.Contains(err.Error(), "available: broken") || strings.Contains(err.Error(), ", broken") {
		t.Fatalf("invalid peer was advertised as available: %v", err)
	}
}

func TestProfilesReturnsValidEntriesAndMarksInvalidEntries(t *testing.T) {
	repository := testRepository(t)
	writeProfileFixture(t, filepath.Join(repository, ".reviewparty", "profiles", "security.md"), "  \n")

	profiles, err := newProfileLibrary(t.TempDir()).list(repository)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]model.ProfileSummary, len(profiles))
	for _, profile := range profiles {
		byName[profile.Name] = profile
	}
	if byName["bugs"].Error != "" || byName["bugs"].Source != "packaged:profiles/bugs.md" {
		t.Fatalf("bugs summary = %#v", byName["bugs"])
	}
	if !strings.Contains(byName["security"].Error, "is empty") {
		t.Fatalf("security summary = %#v", byName["security"])
	}
}

func TestProfilesReturnsValidEntriesAndMarksUnsafeNames(t *testing.T) {
	repository := testRepository(t)
	directory := filepath.Join(repository, ".reviewparty", "profiles")
	writeProfileFixture(t, filepath.Join(directory, "security.md"), "SECURITY REVIEW")
	writeProfileFixture(t, filepath.Join(directory, "README.md"), "NOT A PROFILE NAME")

	profiles, err := newProfileLibrary(t.TempDir()).list(repository)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]model.ProfileSummary, len(profiles))
	for _, profile := range profiles {
		byName[profile.Name] = profile
	}
	if byName["security"].Error != "" {
		t.Fatalf("security summary = %#v", byName["security"])
	}
	if !strings.Contains(byName["README"].Error, "must match") {
		t.Fatalf("README summary = %#v", byName["README"])
	}
}
