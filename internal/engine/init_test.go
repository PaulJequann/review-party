package engine

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWriteNewProfileFileDoesNotPublishFailedWrite(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "bugs.md")
	created, err := writeNewProfileFileWith(destination, []byte("complete profile"), 0o600, func(file *os.File, payload []byte) error {
		if _, writeErr := file.Write(payload[:4]); writeErr != nil {
			return writeErr
		}
		return errors.New("injected write failure")
	})
	if err == nil || created {
		t.Fatalf("created = %t, error = %v", created, err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination survived failed write: %v", statErr)
	}
}

func TestInitializeProfilesCreatesStarterWithoutOverwriting(t *testing.T) {
	repository := testRepository(t)
	first, err := InitializeProfiles(ProfileInitialization{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	assertInitializationCounts(t, first, 2, 0)
	promptPath := filepath.Join(repository, ".reviewparty", "profiles", "bugs.md")
	custom := []byte("MY CUSTOM BUG REVIEW\n")
	if err := os.WriteFile(promptPath, custom, 0o600); err != nil {
		t.Fatal(err)
	}

	second, err := InitializeProfiles(ProfileInitialization{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	assertInitializationCounts(t, second, 0, 2)
	payload, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload, custom) {
		t.Fatalf("prompt was overwritten: %q", payload)
	}
}

func TestInitializeProfilesCreatesGlobalStarterInMissingDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "nested", "global-library")
	result, err := InitializeProfiles(ProfileInitialization{Global: true, GlobalDirectory: directory})
	if err != nil {
		t.Fatal(err)
	}
	assertInitializationCounts(t, result, 2, 0)
	for _, path := range []string{filepath.Join(directory, "profiles", "bugs.md"), filepath.Join(directory, "profiles", "documentation.md")} {
		if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("starter file %q: info = %v, error = %v", path, info, statErr)
		}
	}
}

func TestCreateProfileRequiresExplicitStartingPointAndCopiesPackagedBytes(t *testing.T) {
	repository := testRepository(t)
	if _, err := CreateProfile(ProfileCreation{Name: "security", Repository: repository}); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("missing starting point error = %v", err)
	}
	result, err := CreateProfile(ProfileCreation{Name: "security", Repository: repository, PackagedProfile: "documentation"})
	if err != nil {
		t.Fatal(err)
	}
	want, err := packagedProfileFiles.ReadFile("profiles/documentation.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(result.Created[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("created Profile does not match packaged source")
	}
}

func TestInitializeProfilesInstallsCompleteStarterSetWithoutConfiguration(t *testing.T) {
	repository := testRepository(t)
	result, err := InitializeProfiles(ProfileInitialization{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	assertInitializationCounts(t, result, 2, 0)
	if _, err := os.Stat(filepath.Join(repository, ".reviewparty", "config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("starter installation changed default selection: %v", err)
	}
}

func assertInitializationCounts(t *testing.T, result ProfileInitializationResult, created, existing int) {
	t.Helper()
	if len(result.Created) != created {
		t.Fatalf("created files = %#v, want %d", result.Created, created)
	}
	if len(result.Existing) != existing {
		t.Fatalf("existing files = %#v, want %d", result.Existing, existing)
	}
}

func TestInitializeProfilesRejectsExistingDirectory(t *testing.T) {
	repository, profilePath := blockingProfilePath(t)
	if err := os.Mkdir(profilePath, 0o755); err != nil {
		t.Fatal(err)
	}
	assertProfileInitializationBlocked(t, repository)
}

func TestInitializeProfilesRejectsExistingSymlink(t *testing.T) {
	repository, profilePath := blockingProfilePath(t)
	if err := os.Symlink(filepath.Join(repository, "missing-target"), profilePath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	assertProfileInitializationBlocked(t, repository)
}

func TestInitializeProfilesRejectsSymlinkedDirectoriesWithoutWritingOutside(t *testing.T) {
	cases := []struct {
		name      string
		global    bool
		component string
	}{
		{name: "repository library", component: ".reviewparty"},
		{name: "repository profiles", component: filepath.Join(".reviewparty", "profiles")},
		{name: "global library", global: true, component: "library"},
		{name: "global profiles", global: true, component: filepath.Join("library", "profiles")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, initialization := symlinkedInitializationFixture(t, test.global)
			assertSymlinkedInitializationRejected(t, root, test.component, initialization)
		})
	}
}

func symlinkedInitializationFixture(t *testing.T, global bool) (string, ProfileInitialization) {
	t.Helper()
	if global {
		root := t.TempDir()
		return root, ProfileInitialization{Global: true, GlobalDirectory: filepath.Join(root, "library")}
	}
	repository := testRepository(t)
	return repository, ProfileInitialization{Repository: repository}
}

func assertSymlinkedInitializationRejected(t *testing.T, root, component string, initialization ProfileInitialization) {
	t.Helper()
	external := t.TempDir()
	link := filepath.Join(root, component)
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	result, err := InitializeProfiles(initialization)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	for _, name := range []string{"config.json", "bugs.md"} {
		if _, statErr := os.Stat(filepath.Join(external, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("external file %q was created: %v", name, statErr)
		}
	}
}

func blockingProfilePath(t *testing.T) (string, string) {
	t.Helper()
	repository := testRepository(t)
	profilePath := filepath.Join(repository, ".reviewparty", "profiles", "bugs.md")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	return repository, profilePath
}

func assertProfileInitializationBlocked(t *testing.T, repository string) {
	t.Helper()
	result, err := InitializeProfiles(ProfileInitialization{Repository: repository})
	if err == nil || !strings.Contains(err.Error(), "must be a regular file") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}
