package subject

import (
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

func TestCommittedExecutionCheckoutOmitsCallerStateAndCleansExactWorktree(t *testing.T) {
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
	checkout, err := subject.PrepareExecution("test-owner")
	if err != nil {
		t.Fatal(err)
	}
	path := checkout.Repository
	assertExists(t, filesystemPath(filepath.Join(path, "committed.txt")))
	assertAbsent(t, filesystemPath(filepath.Join(path, ".env")))
	assertAbsent(t, filesystemPath(filepath.Join(path, "node_modules")))
	if err := checkout.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	if _, err := os.Stat(repository); err != nil {
		t.Fatalf("caller repository removed: %v", err)
	}
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

func TestCommittedExecutionReconcilesOnlyInactiveOwnedCheckout(t *testing.T) {
	repository := testRepository(t)
	base := strings.TrimSpace(string(mustGitOutput(t, repository, "rev-parse", "HEAD")))
	writeTestFile(t, filepath.Join(repository, "head.txt"), "head\n")
	runTestCommand(t, repository, "git", "add", "head.txt")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: head")
	subject, err := ResolveSubject(repository, model.CommittedRange(base, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	stale, err := subject.PrepareExecution("stale-owner")
	if err != nil {
		t.Fatal(err)
	}
	stalePath := stale.Repository
	metadata := stalePath + ".owner.json"
	if err := writeOwner(metadataPath(metadata), checkoutOwner{Repository: repository, Path: stalePath, PID: 99999999}); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(t.TempDir(), "user-worktree")
	runTestCommand(t, repository, "git", "worktree", "add", "--detach", unrelated, subject.HeadObject)
	defer runTestCommand(t, repository, "git", "worktree", "remove", "--force", unrelated)
	fresh, err := subject.PrepareExecution("fresh-owner")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fresh.Close(); err != nil {
			t.Errorf("close fresh checkout: %v", err)
		}
	}()
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale checkout remains: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated worktree removed: %v", err)
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
