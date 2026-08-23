package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type authoredLibraryCase struct {
	name         string
	kind         LibraryKind
	logical      string
	wantSource   string
	wantPath     string
	wantBytes    string
	wantPersonal string
}

func TestAuthoredLibraryPreservesPrecedenceAndProvenance(t *testing.T) {
	personalRoot := t.TempDir()
	repository := t.TempDir()
	writeDocument(t, filepath.Join(personalRoot, "profiles", "security.md"), "PERSONAL PROFILE")
	writeDocument(t, filepath.Join(repository, ".reviewparty", "profiles", "security.md"), "REPOSITORY PROFILE")
	writeDocument(t, filepath.Join(personalRoot, "parties", "release.json"), "PERSONAL PARTY")
	writeDocument(t, filepath.Join(repository, ".reviewparty", "parties", "release.json"), "REPOSITORY PARTY")
	manager := testManager(t, personalRoot)

	tests := []authoredLibraryCase{
		{
			name:         "profiles",
			kind:         LibraryProfiles,
			logical:      "security",
			wantSource:   "repository:.reviewparty/profiles/security.md",
			wantPath:     filepath.Join(repository, ".reviewparty", "profiles", "security.md"),
			wantBytes:    "REPOSITORY PROFILE",
			wantPersonal: "PERSONAL PROFILE",
		},
		{
			name:         "parties",
			kind:         LibraryParties,
			logical:      "release",
			wantSource:   "repository:.reviewparty/parties/release.json",
			wantPath:     filepath.Join(repository, ".reviewparty", "parties", "release.json"),
			wantBytes:    "REPOSITORY PARTY",
			wantPersonal: "PERSONAL PARTY",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertAuthoredLibraryCase(t, manager, Repository(repository), test)
		})
	}
}

func TestAuthoredLibraryEntriesExcludeDefinitionDirectories(t *testing.T) {
	personalRoot := t.TempDir()
	repository := t.TempDir()
	directory := filepath.Join(repository, ".reviewparty", "profiles", "ignored.md")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}

	library, err := testManager(t, personalRoot).AuthoredLibrary(LibraryProfiles, Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := library.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want directories excluded", entries)
	}
}

func assertAuthoredLibraryCase(t *testing.T, manager *Manager, repository Repository, test authoredLibraryCase) {
	t.Helper()
	library, err := manager.AuthoredLibrary(test.kind, repository)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := library.Entries()
	if err != nil {
		t.Fatal(err)
	}
	assertAuthoredEntries(t, entries, test.logical)

	assertAuthoredRead(t, library, test)

	assertPersonalRead(t, library, entries[1], test.wantPersonal)
}

func assertAuthoredRead(t *testing.T, library AuthoredLibrary, test authoredLibraryCase) {
	t.Helper()
	entry, payload, found, err := library.Read(test.logical)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("read = %#v, %q, %t; want a repository entry", entry, payload, found)
	}
	if entry.Source != test.wantSource {
		t.Fatalf("source = %q, want %q", entry.Source, test.wantSource)
	}
	if entry.Path != test.wantPath {
		t.Fatalf("path = %q, want %q", entry.Path, test.wantPath)
	}
	if string(payload) != test.wantBytes {
		t.Fatalf("payload = %q, want %q", payload, test.wantBytes)
	}
}

func assertPersonalRead(t *testing.T, library AuthoredLibrary, entry AuthoredEntry, want string) {
	t.Helper()
	payload, found, err := library.ReadEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("personal read = %q, %t; want a file", payload, found)
	}
	if string(payload) != want {
		t.Fatalf("personal payload = %q, want %q", payload, want)
	}
}

func assertAuthoredEntries(t *testing.T, entries []AuthoredEntry, logical string) {
	t.Helper()
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want repository and personal entries", entries)
	}
	if entries[0].Scope != ScopeRepository {
		t.Fatalf("entries = %#v, want repository then personal", entries)
	}
	if entries[1].Scope != ScopePersonal {
		t.Fatalf("entries = %#v, want repository then personal", entries)
	}
	for _, entry := range entries {
		if entry.Name != logical {
			t.Fatalf("entry = %#v, want name %q", entry, logical)
		}
		if entry.Source == "" {
			t.Fatalf("entry = %#v, want source provenance", entry)
		}
		if entry.Path == "" {
			t.Fatalf("entry = %#v, want provenance", entry)
		}
	}
}

func TestAuthoredLibraryEnforcesPerKindByteLimits(t *testing.T) {
	tests := []struct {
		name      string
		kind      LibraryKind
		directory string
		extension string
		limit     int
	}{
		{name: "profiles", kind: LibraryProfiles, directory: "profiles", extension: ".md", limit: 64 * 1024},
		{name: "parties", kind: LibraryParties, directory: "parties", extension: ".json", limit: 16 * 1024},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			personalRoot := t.TempDir()
			repository := t.TempDir()
			path := filepath.Join(repository, ".reviewparty", test.directory, "large"+test.extension)
			writeDocument(t, path, strings.Repeat("x", test.limit+1))
			library, err := testManager(t, personalRoot).AuthoredLibrary(test.kind, Repository(repository))
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, err = library.Read("large")
			if err == nil || !strings.Contains(err.Error(), "exceeds") {
				t.Fatalf("error = %v, want bounded-read error", err)
			}
		})
	}
}

func TestAuthoredLibraryRejectsSymlinkedFiles(t *testing.T) {
	personalRoot := t.TempDir()
	repository := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	writeDocument(t, secret, "OUTSIDE CONTENT")
	path := filepath.Join(repository, ".reviewparty", "parties", "linked.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	library, err := testManager(t, personalRoot).AuthoredLibrary(LibraryParties, Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = library.Read("linked")
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("error = %v, want regular-file safety error", err)
	}
}

func TestRegularFileReaderRejectsFileSwappedForSymlinkBeforeOpen(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "profile.md")
	secretPath := filepath.Join(directory, "secret")
	writeDocument(t, path, "EXPECTED PROFILE")
	writeDocument(t, secretPath, "SECRET CONTENT")

	payload, found, err := readRegularFileWith(directory, path, "test profile", MaximumDocumentBytes, func(root *os.Root, name string) (*os.File, error) {
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
