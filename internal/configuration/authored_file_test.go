package configuration

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type authoredFileExpectation struct {
	scope   Scope
	path    string
	present bool
}

type authoredFileDiagnosticCase struct {
	name    string
	payload string
	reason  string
}

func TestInspectAuthoredReportsAbsentPathsWithoutCreatingFiles(t *testing.T) {
	globalRoot := t.TempDir()
	repository := t.TempDir()
	manager := testManager(t, globalRoot)

	inspection, err := manager.InspectAuthored(Repository(repository), []Scope{ScopeGlobal, ScopeRepository})
	if err != nil {
		t.Fatal(err)
	}

	global, found := inspection.File(ScopeGlobal)
	if !found {
		t.Fatal("global scope was not inspected")
	}
	assertAuthoredFileFacts(t, global, authoredFileExpectation{
		scope: ScopeGlobal, path: filepath.Join(globalRoot, "config.json"), present: false,
	})
	repositoryFile, found := inspection.File(ScopeRepository)
	if !found {
		t.Fatal("repository scope was not inspected")
	}
	assertAuthoredFileFacts(t, repositoryFile, authoredFileExpectation{
		scope: ScopeRepository, path: filepath.Join(repository, ".reviewparty", "config.json"), present: false,
	})
	for _, path := range []string{
		filepath.Join(globalRoot, "config.json"),
		filepath.Join(repository, ".reviewparty", "config.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("authored file %q was created: %v", path, err)
		}
	}
}

func TestInspectAuthoredWritesExactValidatedPayload(t *testing.T) {
	globalRoot := t.TempDir()
	repository := t.TempDir()
	path := filepath.Join(repository, ".reviewparty", "config.json")
	payload := "{\n  \"schema_version\": 1\n}\n"
	writeDocument(t, path, payload)

	file, err := inspectSingleAuthoredFile(t, testManager(t, globalRoot), ScopeRepository, Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	assertAuthoredFileFacts(t, file, authoredFileExpectation{
		scope: ScopeRepository, path: path, present: true,
	})

	var output bytes.Buffer
	if err := file.WritePayload(&output); err != nil {
		t.Fatal(err)
	}
	if output.String() != payload {
		t.Fatalf("payload = %q, want %q", output.String(), payload)
	}
}

func TestInspectAuthoredPreservesDocumentDiagnostics(t *testing.T) {
	tests := []authoredFileDiagnosticCase{
		{name: "malformed", payload: `{"schema_version":1,"unexpected":true}`, reason: "unknown field"},
		{name: "invalid", payload: `{"schema_version":2}`, reason: "unsupported schema_version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertInvalidAuthoredFile(t, test)
		})
	}
}

func assertInvalidAuthoredFile(t *testing.T, test authoredFileDiagnosticCase) {
	t.Helper()
	globalRoot := t.TempDir()
	repository := t.TempDir()
	path := filepath.Join(repository, ".reviewparty", "config.json")
	writeDocument(t, path, test.payload)

	file, err := inspectSingleAuthoredFile(t, testManager(t, globalRoot), ScopeRepository, Repository(repository))
	if err == nil {
		t.Fatal("authored document unexpectedly succeeded")
	}
	assertInvalidDocumentError(t, err, path, test.reason)
	assertAuthoredFileFacts(t, file, authoredFileExpectation{
		scope: ScopeRepository, path: path, present: true,
	})
}

func assertInvalidDocumentError(t *testing.T, err error, path, reason string) {
	t.Helper()
	var invalid InvalidDocumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidDocumentError", err)
	}
	if invalid.Scope != ScopeRepository {
		t.Fatalf("invalid document scope = %q", invalid.Scope)
	}
	if invalid.Path != path {
		t.Fatalf("invalid document path = %q, want %q", invalid.Path, path)
	}
	if !strings.Contains(invalid.Reason, reason) {
		t.Fatalf("invalid document reason = %q, want %q", invalid.Reason, reason)
	}
}

func TestInspectAuthoredPreservesRootedSafetyAndByteLimit(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		globalRoot := t.TempDir()
		repository := t.TempDir()
		path := filepath.Join(repository, ".reviewparty", "config.json")
		writeDocument(t, path, strings.Repeat("x", MaximumDocumentBytes+1))

		_, err := inspectSingleAuthoredFile(t, testManager(t, globalRoot), ScopeRepository, Repository(repository))
		if err == nil {
			t.Fatal("oversized authored file unexpectedly succeeded")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error = %v, want byte-limit error", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		globalRoot := t.TempDir()
		repository := t.TempDir()
		outside := filepath.Join(t.TempDir(), "config.json")
		writeDocument(t, outside, `{"schema_version":1}`)
		path := filepath.Join(repository, ".reviewparty", "config.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		_, err := inspectSingleAuthoredFile(t, testManager(t, globalRoot), ScopeRepository, Repository(repository))
		if err == nil {
			t.Fatal("symlinked authored file unexpectedly succeeded")
		}
		if !strings.Contains(err.Error(), "must be a regular file") {
			t.Fatalf("error = %v, want rooted symlink error", err)
		}
	})
}

func inspectSingleAuthoredFile(t *testing.T, manager *Manager, scope Scope, repository Repository) (AuthoredFile, error) {
	t.Helper()
	inspection, err := manager.InspectAuthored(repository, []Scope{scope})
	file, found := inspection.File(scope)
	if !found {
		t.Fatalf("scope %q was not inspected", scope)
	}
	return file, err
}

func assertAuthoredFileFacts(t *testing.T, file AuthoredFile, expected authoredFileExpectation) {
	t.Helper()
	if file.Scope != expected.scope {
		t.Fatalf("authored file scope = %q, want %q", file.Scope, expected.scope)
	}
	if file.Path != expected.path {
		t.Fatalf("authored file path = %q, want %q", file.Path, expected.path)
	}
	if file.Present != expected.present {
		t.Fatalf("authored file present = %t, want %t", file.Present, expected.present)
	}
}
