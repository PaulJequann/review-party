package subject

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func TestWorkingChangesRecordsTrackedAndUntrackedSubjectFacts(t *testing.T) {
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"changed\"\n")
	writeTestFile(t, filepath.Join(repository, "new.go"), "package demo\n\nconst added = true\n")
	if err := os.WriteFile(filepath.Join(repository, "image.bin"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}

	subject, err := resolveWorkingChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	want := SubjectFacts{ChangedFiles: 3, Additions: 4, Deletions: 1, BinaryFiles: 1}
	if !reflect.DeepEqual(subject.Facts, &want) {
		t.Fatalf("facts = %#v, want %#v", subject.Facts, want)
	}
}

func TestCapturedSubjectKeepsMaterializationOpaqueAndPathIndependent(t *testing.T) {
	base := t.TempDir()
	head := t.TempDir()
	writeTestFile(t, filepath.Join(base, "review.go"), "package demo\n\nconst state = \"base\"\n")
	writeTestFile(t, filepath.Join(head, "review.go"), "package demo\n\nconst state = \"head\"\n")

	resolved, err := ResolveSubject("", model.CapturedChange(base, head))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resolved.Patch, base) || strings.Contains(resolved.Patch, head) {
		t.Fatalf("patch leaked source path: %s", resolved.Patch)
	}
	serialized, err := json.Marshal(resolved.ReviewSubject)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), head) {
		t.Fatalf("durable Subject leaked source path: %s", serialized)
	}
	otherBase := t.TempDir()
	otherHead := t.TempDir()
	writeTestFile(t, filepath.Join(otherBase, "review.go"), "package demo\n\nconst state = \"base\"\n")
	writeTestFile(t, filepath.Join(otherHead, "review.go"), "package demo\n\nconst state = \"head\"\n")
	other, err := ResolveSubject("", model.CapturedChange(otherBase, otherHead))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Identity != other.Identity {
		t.Fatalf("identity depends on source paths: %s != %s", resolved.Identity, other.Identity)
	}

}

func TestCapturedSubjectExecutionCleansItsCheckout(t *testing.T) {
	base := t.TempDir()
	head := t.TempDir()
	writeTestFile(t, filepath.Join(base, "review.go"), "package demo\n\nconst state = \"base\"\n")
	writeTestFile(t, filepath.Join(head, "review.go"), "package demo\n\nconst state = \"head\"\n")
	resolved, err := ResolveSubject("", model.CapturedChange(base, head))
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := resolved.PrepareExecution("captured-test")
	if err != nil {
		t.Fatal(err)
	}
	path := checkout.Repository
	if _, err := os.Stat(filepath.Join(path, "review.go")); err != nil {
		t.Fatal(err)
	}
	if err := checkout.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("captured checkout remains: %v", err)
	}
}

func TestWorkingChangeFactsMatchPatchWhenWorktreeChangesDuringCapture(t *testing.T) {
	repository := testRepository(t)
	path := filepath.Join(repository, "review.go")
	writeTestFile(t, path, "package demo\n\nconst state = \"captured\"\n")
	installMutatingGitWrapper(t, path)

	subject, err := resolveWorkingChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(subject.Patch, "later") {
		t.Fatalf("captured patch contains later worktree mutation:\n%s", subject.Patch)
	}
	want := SubjectFacts{ChangedFiles: 1, Additions: 1, Deletions: 1}
	if !reflect.DeepEqual(subject.Facts, &want) {
		t.Fatalf("facts = %#v, want facts from captured patch %#v", subject.Facts, want)
	}
}

func TestWorkingChangeFactsDeduplicateRecreatedTrackedPath(t *testing.T) {
	repository := testRepository(t)
	path := filepath.Join(repository, "image.bin")
	if err := os.WriteFile(path, []byte{0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, repository, "git", "add", "image.bin")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: add binary")
	runTestCommand(t, repository, "git", "rm", "--cached", "--quiet", "image.bin")
	if err := os.WriteFile(path, []byte{0, 3, 4}, 0o600); err != nil {
		t.Fatal(err)
	}

	subject, err := resolveWorkingChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(subject.ChangedPaths, []string{"image.bin"}) {
		t.Fatalf("changed paths = %#v", subject.ChangedPaths)
	}
	want := SubjectFacts{ChangedFiles: 1, BinaryFiles: 1}
	if !reflect.DeepEqual(subject.Facts, &want) {
		t.Fatalf("facts = %#v, want %#v", subject.Facts, want)
	}
}

func installMutatingGitWrapper(t *testing.T, mutateFile string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	wrapper := filepath.Join(directory, "git")
	marker := filepath.Join(directory, "mutated")
	script := `#!/bin/sh
"$REAL_GIT" "$@"
status=$?
if [ "$1" = "diff" ]; then
  if [ "$2" = "--binary" ]; then
    if [ ! -e "$MUTATION_MARKER" ]; then
      printf 'package demo\n\nconst state = "later"\nconst extra = true\n' > "$MUTATE_FILE"
      : > "$MUTATION_MARKER"
    fi
  fi
fi
exit "$status"
`
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REAL_GIT", realGit)
	t.Setenv("MUTATE_FILE", mutateFile)
	t.Setenv("MUTATION_MARKER", marker)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}
