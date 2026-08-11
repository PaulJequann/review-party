package subject

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Helpers for subject tests — duplicated from engine's test helpers to avoid
// cross-package import (subject is a leaf package).
func testRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runTestCommand(t, repository, "git", "init", "--quiet")
	runTestCommand(t, repository, "git", "config", "user.email", "review-party@example.invalid")
	runTestCommand(t, repository, "git", "config", "user.name", "Review Party Test")
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"base\"\n")
	runTestCommand(t, repository, "git", "add", "review.go")
	runTestCommand(t, repository, "git", "commit", "--quiet", "-m", "test: establish base")
	return repository
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
}

func runTestCommand(t *testing.T, directory, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("command %s %v failed: %v\n%s", name, arguments, err, output)
	}
}

// Alias for private function called by tests (resolveWorkingChanges was renamed
// to exported ResolveWorkingChanges; keep lower-case alias for tests).
var resolveWorkingChanges = ResolveWorkingChanges
