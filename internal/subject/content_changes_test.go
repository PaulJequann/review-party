package subject

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestContentChangesIgnoreCommitIdentity(t *testing.T) {
	repository := testRepository(t)
	base := gitText(t, repository, "rev-parse", "HEAD")
	commitOnBranch := func(branch, message string) string {
		runTestCommand(t, repository, "git", "checkout", "--quiet", "-b", branch, base)
		writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"head\"\n")
		runTestCommand(t, repository, "git", "add", "review.go")
		runTestCommand(t, repository, "git", "commit", "--quiet", "-m", message)
		return gitText(t, repository, "rev-parse", "HEAD")
	}
	first := commitOnBranch("first", "first wording")
	second := commitOnBranch("second", "second wording")
	if first == second {
		t.Fatal("fixture commits share an ID")
	}

	firstSubject, err := ResolveSubject(repository, model.CommittedRange(base, first))
	if err != nil {
		t.Fatal(err)
	}
	secondSubject, err := ResolveSubject(repository, model.CommittedRange(base, second))
	if err != nil {
		t.Fatal(err)
	}
	if firstSubject.Identity == secondSubject.Identity {
		t.Fatal("Subject identities should still differ by commit")
	}
	want := []model.ContentChange{{
		Path:   "review.go",
		Before: gitText(t, repository, "rev-parse", base+":review.go"),
		After:  gitText(t, repository, "rev-parse", first+":review.go"),
	}}
	if !reflect.DeepEqual(firstSubject.ContentChanges, want) || !reflect.DeepEqual(secondSubject.ContentChanges, want) {
		t.Fatalf("content changes = %#v and %#v, want %#v", firstSubject.ContentChanges, secondSubject.ContentChanges, want)
	}
}

func TestCommittedRangeContentChangesSplitsCommits(t *testing.T) {
	repository := testRepository(t)
	root := gitText(t, repository, "rev-parse", "HEAD")
	writeTestFile(t, filepath.Join(repository, "b.go"), "package demo\n")
	runTestCommand(t, repository, "git", "add", "b.go")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "add b")
	runTestCommand(t, repository, "git", "commit", "--quiet", "--allow-empty", "-m", "empty")
	writeTestFile(t, filepath.Join(repository, "a.go"), "package demo\n")
	runTestCommand(t, repository, "git", "rm", "--quiet", "review.go")
	runTestCommand(t, repository, "git", "add", "a.go")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "add a, remove review")
	blob := gitText(t, repository, "rev-parse", "HEAD:a.go")
	reviewBlob := gitText(t, repository, "rev-parse", root+":review.go")

	changes, err := CommittedRangeContentChanges(repository, model.CommittedRange(root, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if changes.Base != root || changes.Head != gitText(t, repository, "rev-parse", "HEAD") {
		t.Fatalf("range = %s..%s", changes.Base, changes.Head)
	}
	wantWhole := []model.ContentChange{
		{Path: "a.go", Before: model.ZeroObjectID, After: blob},
		{Path: "b.go", Before: model.ZeroObjectID, After: blob},
		{Path: "review.go", Before: reviewBlob, After: model.ZeroObjectID},
	}
	if !reflect.DeepEqual(changes.Changes, wantWhole) {
		t.Fatalf("whole range = %#v, want %#v", changes.Changes, wantWhole)
	}
	wantCommits := []CommitContentChanges{
		{Commit: gitText(t, repository, "rev-parse", "HEAD~2"), Changes: wantWhole[1:2]},
		{Commit: gitText(t, repository, "rev-parse", "HEAD"), Changes: []model.ContentChange{wantWhole[0], wantWhole[2]}},
	}
	if !reflect.DeepEqual(changes.Commits, wantCommits) {
		t.Fatalf("commits = %#v, want %#v", changes.Commits, wantCommits)
	}
	wantLines := LineCounts{"a.go": {Added: 1}, "b.go": {Added: 1}, "review.go": {Deleted: 3}}
	if !reflect.DeepEqual(changes.Lines, wantLines) {
		t.Fatalf("lines = %#v, want %#v", changes.Lines, wantLines)
	}
}

func TestRootCommitContentChangesUseEmptyTree(t *testing.T) {
	repository := testRepository(t)
	head := gitText(t, repository, "rev-parse", "HEAD")
	commits, err := repositoryRoot(repository).commitContentChanges(commitObject(emptyGitTree), commitObject(head))
	if err != nil {
		t.Fatal(err)
	}
	want := []CommitContentChanges{{Commit: head, Changes: []model.ContentChange{{
		Path: "review.go", Before: model.ZeroObjectID, After: gitText(t, repository, "rev-parse", "HEAD:review.go"),
	}}}}
	if !reflect.DeepEqual(commits, want) {
		t.Fatalf("root commit = %#v, want %#v", commits, want)
	}
}

func TestWorkingChangesContentMatchesStagingEverything(t *testing.T) {
	repository := testRepository(t)
	makeEveryKindOfWorkingChange(t, repository)

	working, err := WorkingContentChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	if got := contentChangePaths(working); !reflect.DeepEqual(got, []string{"gone.go", "link", "review.go", "untracked.go"}) {
		t.Fatalf("working change paths = %#v", got)
	}
	resolved, err := resolveWorkingChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.ContentChanges, working) {
		t.Fatalf("Working Changes Subject carries %#v, want %#v", resolved.ContentChanges, working)
	}
	runTestCommand(t, repository, "git", "add", "-A")
	staged, err := StagedContentChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(working, staged) {
		t.Fatalf("working changes %#v differ from the same content staged %#v", working, staged)
	}
}

// makeEveryKindOfWorkingChange leaves an edit, an untracked file, a deletion,
// a symlink to a missing target, and a file that is only stat-dirty.
func makeEveryKindOfWorkingChange(t *testing.T, repository string) {
	t.Helper()
	writeTestFile(t, filepath.Join(repository, "kept.go"), "package demo\n")
	writeTestFile(t, filepath.Join(repository, "gone.go"), "package demo\n")
	if err := os.Symlink("kept.go", filepath.Join(repository, "link")); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, repository, "git", "add", "kept.go", "gone.go", "link")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "more files")

	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"edited\"\n")
	writeTestFile(t, filepath.Join(repository, "untracked.go"), "package demo\n\nvar fresh = 1\n")
	if err := os.Remove(filepath.Join(repository, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repository, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(repository, "link")); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(repository, "kept.go"), later, later); err != nil {
		t.Fatal(err)
	}
}

func TestUntrackedContentHashesLikeGitHashObject(t *testing.T) {
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "fresh.txt"), "fresh content\n")
	changes, err := WorkingContentChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.ContentChange{{Path: "fresh.txt", Before: model.ZeroObjectID, After: gitText(t, repository, "hash-object", "fresh.txt")}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("untracked changes = %#v, want %#v", changes, want)
	}
}

func TestStagedContentChangesExcludeUnstagedEdits(t *testing.T) {
	repository := testRepository(t)
	path := filepath.Join(repository, "review.go")
	writeTestFile(t, path, "package demo\n\nconst state = \"staged\"\n")
	runTestCommand(t, repository, "git", "add", "review.go")
	stagedBlob := gitText(t, repository, "rev-parse", ":review.go")
	writeTestFile(t, path, "package demo\n\nconst state = \"unstaged\"\n")
	writeTestFile(t, filepath.Join(repository, "untracked.go"), "package demo\n")

	staged, err := StagedContentChanges(repository)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.ContentChange{{Path: "review.go", Before: gitText(t, repository, "rev-parse", "HEAD:review.go"), After: stagedBlob}}
	if !reflect.DeepEqual(staged, want) {
		t.Fatalf("staged = %#v, want %#v", staged, want)
	}
}

func TestStagedLineCountsCountEachPathAndMarkBinaries(t *testing.T) {
	repository := testRepository(t)
	runTestCommand(t, repository, "git", "mv", "review.go", "moved.go")
	if err := os.WriteFile(filepath.Join(repository, "image.bin"), []byte{0, 1, 2, 0, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, repository, "git", "add", "image.bin")
	writeTestFile(t, filepath.Join(repository, "unstaged.go"), "package demo\n")

	lines, err := StagedLineCounts(repository)
	if err != nil {
		t.Fatal(err)
	}
	want := LineCounts{"image.bin": {Binary: true}, "moved.go": {Added: 3}, "review.go": {Deleted: 3}}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("staged lines = %#v, want %#v", lines, want)
	}
}

func contentChangePaths(changes []model.ContentChange) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	return paths
}

func gitText(t *testing.T, repository string, arguments ...string) string {
	t.Helper()
	return strings.TrimSpace(string(mustGitOutput(t, repository, arguments...)))
}
