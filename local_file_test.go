package reviewparty

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalFileReaderRejectsFileSwappedForSymlinkBeforeOpen(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "profile.md")
	secretPath := filepath.Join(directory, "secret")
	writeTestFile(t, path, "EXPECTED PROFILE")
	writeTestFile(t, secretPath, "SECRET CONTENT")

	payload, found, err := readLocalRegularFileWith(directory, path, "test profile", maximumProfileBytes, func(root *os.Root, name string) (*os.File, error) {
		if removeErr := os.Remove(path); removeErr != nil {
			return nil, removeErr
		}
		if symlinkErr := os.Symlink(filepath.Base(secretPath), path); symlinkErr != nil {
			return nil, symlinkErr
		}
		return root.Open(name)
	})
	if err == nil {
		t.Fatalf("error = %v", err)
	}
	if found {
		t.Fatal("swapped file was reported as found")
	}
	if len(payload) != 0 {
		t.Fatalf("secret payload was returned: %q", payload)
	}
}

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
