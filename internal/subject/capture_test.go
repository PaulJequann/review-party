package subject

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCombineWorkingChangePatchesStitchesUntrackedDiffsInListingOrder(t *testing.T) {
	repository := testRepository(t)
	const untrackedCount = 6
	paths := make([]string, untrackedCount)
	for index := range untrackedCount {
		name := fmt.Sprintf("untracked-%02d.txt", index)
		writeTestFile(t, filepath.Join(repository, name), fmt.Sprintf("content %02d\n", index))
		paths[index] = name
	}

	patch, err := combineWorkingChangePatches(repository, nil, paths)
	if err != nil {
		t.Fatal(err)
	}

	previous := -1
	for _, path := range paths {
		marker := "+++ b/" + path
		found := strings.Index(string(patch), marker)
		if found < 0 {
			t.Fatalf("patch missing %s:\n%s", marker, patch)
		}
		if found < previous {
			t.Fatalf("patch out of listing order at %s:\n%s", marker, patch)
		}
		previous = found
	}
}

func TestCaptureWorkingChangesTreatsUntrackedFileVanishingDuringCaptureAsEmpty(t *testing.T) {
	repository := testRepository(t)
	vanishing := filepath.Join(repository, "vanish.txt")
	writeTestFile(t, vanishing, "gone before diff\n")
	writeTestFile(t, filepath.Join(repository, "keep.txt"), "stays for the diff\n")
	installDeletingGitWrapper(t, vanishing)

	subject, err := resolveWorkingChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(subject.Patch, "vanish.txt") || strings.Contains(subject.Patch, "gone before diff") {
		t.Fatalf("captured patch contains vanished file:\n%s", subject.Patch)
	}
	if !strings.Contains(subject.Patch, "+++ b/keep.txt") {
		t.Fatalf("captured patch missing surviving untracked file:\n%s", subject.Patch)
	}
	for _, path := range subject.ChangedPaths {
		if path == "vanish.txt" {
			t.Fatalf("changed paths include vanished file: %#v", subject.ChangedPaths)
		}
	}
}

// installDeletingGitWrapper removes the given file right after git lists
// untracked paths, widening the list-then-diff window so tests can exercise
// files disappearing mid-capture.
func installDeletingGitWrapper(t *testing.T, deleteFile string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	wrapper := filepath.Join(directory, "git")
	script := `#!/bin/sh
"$REAL_GIT" "$@"
status=$?
if [ "$1" = "ls-files" ]; then
  rm -f "$DELETE_FILE"
fi
exit "$status"
`
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REAL_GIT", realGit)
	t.Setenv("DELETE_FILE", deleteFile)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}
