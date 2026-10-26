package configuration

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGlobalProfilePublicationCreatesMissingAncestors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HOME/.config and XDG_CONFIG_HOME are Linux configuration paths")
	}
	for _, configHome := range []string{"", "config", "missing/nested/config"} {
		t.Run("XDG="+configHome, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			if configHome == "" {
				if err := os.Unsetenv("XDG_CONFIG_HOME"); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, configHome))
			}
			if err := os.Chmod(home, 0o755); err != nil {
				t.Fatal(err)
			}
			manager := testManager(t, "")
			root, err := manager.GlobalRoot()
			if err != nil {
				t.Fatal(err)
			}
			plan := requireProfilePlan(t, manager, ancestorProfileDraft(ScopeGlobal))
			if _, found, err := manager.LoadProfile(ScopeGlobal, "", "security"); err != nil || found {
				t.Fatalf("read before publication = found %t, error %v", found, err)
			}
			assertPublicationPathAbsent(t, filepath.Dir(root))
			if err := manager.Publish(plan); err != nil {
				t.Fatal(err)
			}
			profile := requireProfile(t, manager, ScopeGlobal, "security")
			if profile.Instructions != "Review security.\n" || profile.Reviewer != "codex" || profile.Model != "gpt-5.6-luna" {
				t.Fatalf("published Profile = %#v", profile)
			}
			assertPublicationTreeModes(t, home, 0o700, 0o600)
			info, err := os.Stat(home)
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("existing home permissions changed: info %v, error %v", info, err)
			}
		})
	}
}

func TestGlobalConfigurationPublicationCreatesMissingAncestors(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "missing", "config", "review-party")
	manager := testManager(t, root)
	state := filepath.Join(home, "state")
	plan, err := manager.Plan("", []Intent{SetStateDirectory{Directory: state}})
	plan = requireValidPlan(t, "configuration", plan, err)
	assertPublicationPathAbsent(t, filepath.Join(home, "missing"))
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	effective := requireEffective(t, manager)
	if effective.StateDirectory.Value != state {
		t.Fatalf("state directory = %#v", effective.StateDirectory)
	}
	assertPublicationTreeModes(t, home, 0o700, 0o600)
}

func TestGlobalPublicationPreservesExistingIntermediateAncestorPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not supported on Windows")
	}
	base := t.TempDir()
	existing := filepath.Join(base, "shared", "config")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o750); err != nil {
		t.Fatal(err)
	}
	manager := testManager(t, filepath.Join(existing, "missing", "nested", "review-party"))
	plan := requireProfilePlan(t, manager, ancestorProfileDraft(ScopeGlobal))
	assertPublicationPathAbsent(t, filepath.Join(existing, "missing"))
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(existing)
	if err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("existing intermediate ancestor permissions changed: info %v, error %v", info, err)
	}
	assertPublicationTreeModes(t, existing, 0o700, 0o600)
}

func TestInvalidProfilePlanDoesNotCreateAncestors(t *testing.T) {
	home := t.TempDir()
	manager := testManager(t, filepath.Join(home, "missing", "config", "review-party"))
	draft := ancestorProfileDraft(ScopeGlobal)
	draft.Model = ""
	plan, err := manager.PlanProfileCreation("", draft)
	if err != nil || plan.Valid() {
		t.Fatalf("invalid draft plan = valid %t, error %v", plan.Valid(), err)
	}
	if err := manager.Publish(plan); err == nil {
		t.Fatal("invalid plan published")
	}
	assertPublicationPathAbsent(t, filepath.Join(home, "missing"))
}

func TestRepositoryProfilePublicationCreatesConfigurationDirectory(t *testing.T) {
	repository := t.TempDir()
	manager := testManager(t, t.TempDir())
	plan, err := manager.PlanProfileCreation(Repository(repository), ancestorProfileDraft(ScopeRepository))
	plan = requireValidPlan(t, "repository Profile", plan, err)
	assertPublicationPathAbsent(t, filepath.Join(repository, ".reviewparty"))
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	profile, found, err := manager.LoadProfile(ScopeRepository, Repository(repository), "security")
	if err != nil || !found || profile.Instructions != "Review security.\n" {
		t.Fatalf("repository Profile = %#v, found %t, error %v", profile, found, err)
	}
	assertPublicationTreeModes(t, repository, 0o755, 0o644)
}

func ancestorProfileDraft(scope Scope) ProfileDraft {
	return ProfileDraft{
		Target: scope, Name: "security", Reviewer: "codex", Model: "gpt-5.6-luna",
		ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review security.\n",
	}
}

func assertPublicationPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%q should not exist: %v", path, err)
	}
}

func assertPublicationTreeModes(t *testing.T, base string, directoryMode, fileMode fs.FileMode) {
	t.Helper()
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == base {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		want := fileMode
		if entry.IsDir() {
			want = directoryMode
		}
		if (runtime.GOOS != "windows" && info.Mode().Perm() != want) || strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("published path %q has mode %v, want %v and no staging debris", path, info.Mode().Perm(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
