package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileReaderRejectsSymlinkedLibraryDirectory(t *testing.T) {
	repository := testRepository(t)
	outside := t.TempDir()
	writeProfileFixture(t, filepath.Join(outside, "bugs.md"), "OUT_OF_TREE")
	libraryDirectory := filepath.Join(repository, ".reviewparty")
	if err := os.MkdirAll(libraryDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(libraryDirectory, "profiles")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	profile, err := compileTestProfile(newProfileLibrary(t.TempDir()), "bugs", "grok", ReviewSubject{Repository: repository})
	if err == nil || !strings.Contains(err.Error(), "must not traverse symlink") {
		t.Fatalf("profile = %#v, error = %v", profile, err)
	}
	promptContainsOutside := profile.buildPrompt != nil && strings.Contains(profile.prompt(ReviewSubject{}), "OUT_OF_TREE")
	if promptContainsOutside || strings.Contains(profile.snapshot.Instructions, "OUT_OF_TREE") {
		t.Fatalf("out-of-tree content entered compiled profile: %#v", profile)
	}
}
