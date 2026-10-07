package subject

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func TestCommittedRangeFreezesObjectsPatchAndPaths(t *testing.T) {
	repository := testRepository(t)
	base := strings.TrimSpace(string(mustGitOutput(t, repository, "rev-parse", "HEAD")))
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"head\"\n")
	runTestCommand(t, repository, "git", "add", "review.go")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: head")
	subject, err := ResolveSubject(repository, model.CommittedRange(base, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	wantHead := strings.TrimSpace(string(mustGitOutput(t, repository, "rev-parse", "HEAD")))
	writeTestFile(t, filepath.Join(repository, "review.go"), "caller mutation\n")
	if subject.BaseObject != base {
		t.Fatalf("base = %s, want %s", subject.BaseObject, base)
	}
	if subject.HeadObject != wantHead {
		t.Fatalf("head = %s, want %s", subject.HeadObject, wantHead)
	}
	if !reflect.DeepEqual(subject.ChangedPaths, []string{"review.go"}) {
		t.Fatalf("paths = %#v", subject.ChangedPaths)
	}
	if !strings.Contains(subject.Patch, "state = \"head\"") {
		t.Fatalf("patch = %s", subject.Patch)
	}
}

func TestCommittedRangeRecordsRenamesAndBinaryChanges(t *testing.T) {
	repository := testRepository(t)
	base := strings.TrimSpace(string(mustGitOutput(t, repository, "rev-parse", "HEAD")))
	runTestCommand(t, repository, "git", "mv", "review.go", "renamed.go")
	if err := os.WriteFile(filepath.Join(repository, "binary.dat"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, repository, "git", "add", "renamed.go", "binary.dat")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: rename and binary")
	subject, err := ResolveSubject(repository, model.CommittedRange(base, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	assertBinaryRenameFacts(t, subject.ReviewSubject)
	if !strings.Contains(subject.Patch, "GIT binary patch") {
		t.Fatalf("binary patch missing:\n%s", subject.Patch)
	}
}

type relativePath string
type filesystemPath string

func containsPath(paths []string, target relativePath) bool {
	for _, path := range paths {
		if path == string(target) {
			return true
		}
	}
	return false
}

func TestCommittedSubjectBuildsAGitViewWithoutTouchingTheRepository(t *testing.T) {
	repository := testRepository(t)
	base := strings.TrimSpace(string(mustGitOutput(t, repository, "rev-parse", "HEAD")))
	writeTestFile(t, filepath.Join(repository, "committed.txt"), "recorded head\n")
	runTestCommand(t, repository, "git", "add", "committed.txt")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: head")
	if err := os.WriteFile(filepath.Join(repository, ".env"), []byte("SECRET=caller\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, "node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	subject, err := ResolveSubject(repository, model.CommittedRange(base, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "committed.txt"), "caller mutation\n")
	gitDirBefore := gitDirListing(t, repository)
	if _, inPlace := subject.InPlace(); inPlace {
		t.Fatal("committed range reported as reviewed in place")
	}
	if subject.ViewKey() != "git:"+repository+":"+subject.HeadObject {
		t.Fatalf("view key = %q", subject.ViewKey())
	}
	view := t.TempDir()
	if err := subject.BuildView(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filesystemPath(filepath.Join(view, ".git", "objects", "info", "alternates")))
	content, err := os.ReadFile(filepath.Join(view, "committed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "recorded head\n" {
		t.Fatalf("view content = %q", content)
	}
	assertAbsent(t, filesystemPath(filepath.Join(view, ".env")))
	assertAbsent(t, filesystemPath(filepath.Join(view, "node_modules")))
	if got := gitDirListing(t, repository); !reflect.DeepEqual(got, gitDirBefore) {
		t.Fatalf("repository .git changed:\nbefore %v\nafter  %v", gitDirBefore, got)
	}
	if worktrees := string(mustGitOutput(t, repository, "worktree", "list", "--porcelain")); strings.Count(worktrees, "worktree ") != 1 {
		t.Fatalf("view registered as a worktree:\n%s", worktrees)
	}
}

func TestCommittedSubjectViewReportsAMissingHead(t *testing.T) {
	repository := testRepository(t)
	subject := Subject{ReviewSubject: model.ReviewSubject{Kind: model.SubjectCommittedRange, Repository: repository, HeadObject: strings.Repeat("0", 40)}}
	err := subject.BuildView(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "git -c") {
		t.Fatalf("err = %v", err)
	}
}

func gitDirListing(t *testing.T, repository string) []string {
	t.Helper()
	var listing []string
	root := filepath.Join(repository, ".git")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		listing = append(listing, relative+":"+info.ModTime().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return listing
}

func assertBinaryRenameFacts(t *testing.T, subject model.ReviewSubject) {
	t.Helper()
	if subject.Facts == nil {
		t.Fatal("Subject facts are nil")
	}
	if subject.Facts.BinaryFiles != 1 {
		t.Fatalf("binary files = %d", subject.Facts.BinaryFiles)
	}
	for _, path := range []string{"renamed.go", "binary.dat"} {
		if !containsPath(subject.ChangedPaths, relativePath(path)) {
			t.Fatalf("paths = %#v, missing %s", subject.ChangedPaths, path)
		}
	}
}

func assertExists(t *testing.T, path filesystemPath) {
	t.Helper()
	if _, err := os.Stat(string(path)); err != nil {
		t.Fatal(err)
	}
}

func assertAbsent(t *testing.T, path filesystemPath) {
	t.Helper()
	if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
		t.Fatalf("%s exists: %v", path, err)
	}
}

func mustGitOutput(t *testing.T, repository string, arguments ...string) []byte {
	t.Helper()
	value, err := gitOutput(repository, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
