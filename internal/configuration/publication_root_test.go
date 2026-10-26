package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPublicationRootPreservesExistingSymlinkPrefixes(t *testing.T) {
	for _, suffix := range []string{"", "missing/config"} {
		t.Run(suffix, func(t *testing.T) {
			base, target := t.TempDir(), t.TempDir()
			link := filepath.Join(base, "linked")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			manager := testManager(t, filepath.Join(link, suffix, "review-party"))
			plan := requireProfilePlan(t, manager, ancestorProfileDraft(ScopeGlobal))
			if err := manager.Publish(plan); err != nil {
				t.Fatal(err)
			}
			requireProfile(t, manager, ScopeGlobal, "security")
			if runtime.GOOS != "windows" {
				assertPrivateFile(t, filepath.Join(target, suffix, "review-party", "profiles", "security", "profile.json"))
			}
		})
	}
}

func TestPublicationRootRejectsObstructedAncestors(t *testing.T) {
	for _, obstruction := range []string{"file", "dangling-symlink"} {
		t.Run(obstruction, func(t *testing.T) {
			base := t.TempDir()
			blocked := filepath.Join(base, "blocked")
			var err error
			if obstruction == "file" {
				err = os.WriteFile(blocked, []byte("keep"), 0o600)
			} else {
				err = os.Symlink(filepath.Join(base, "absent"), blocked)
				if err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			root, err := openPublicationRoot(ScopeGlobal, filepath.Join(blocked, "config"))
			if root != nil || err == nil {
				t.Fatalf("obstructed ancestor = %v, error %v", root, err)
			}
			assertPublicationPathAbsent(t, filepath.Join(base, "absent"))
		})
	}
}

func TestPublicationChildRejectsSuffixSymlinks(t *testing.T) {
	for _, target := range []string{".", "absent", "outside"} {
		t.Run(target, func(t *testing.T) {
			base := t.TempDir()
			linkTarget := target
			if target == "outside" {
				linkTarget = t.TempDir()
			}
			if err := os.Symlink(linkTarget, filepath.Join(base, "child")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			parent := requirePublicationRoot(t, ScopeGlobal, base)
			child, err := openPublicationChild(parent, "child")
			if child != nil || err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("suffix symlink = %v, error %v", child, err)
			}
		})
	}
}

func TestPublicationChildRetainsParentAfterPathReplacement(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	path := filepath.Join(base, "parent")
	parent := requirePublicationRoot(t, ScopeGlobal, path)
	retained := filepath.Join(base, "retained")
	if err := os.Rename(path, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	child, err := openPublicationChild(parent, "config")
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(retained, "config")); err != nil {
		t.Fatal(err)
	}
	assertPublicationPathAbsent(t, filepath.Join(outside, "config"))
}

func TestPublicationRootDoesNotCreateRepositoryAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent-repository")
	root, err := openPublicationRoot(ScopeRepository, path)
	if root != nil || !os.IsNotExist(err) {
		t.Fatalf("missing repository = %v, error %v", root, err)
	}
	assertPublicationPathAbsent(t, path)
}

func TestGlobalPublicationReportsAncestorPermissionFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not supported on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	base := t.TempDir()
	manager := testManager(t, filepath.Join(base, "missing", "config", "review-party"))
	plan := requireProfilePlan(t, manager, ancestorProfileDraft(ScopeGlobal))
	if err := os.Chmod(base, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(base, 0o700); err != nil {
			t.Error(err)
		}
	})
	if err := manager.Publish(plan); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("publication error = %v, want permission failure", err)
	}
	assertPublicationPathAbsent(t, filepath.Join(base, "missing"))
}

func TestGlobalPublicationRollsBackProfilesAfterCreatingAncestors(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "missing", "config", "review-party")
	manager := testManager(t, root)
	first := requireProfilePlan(t, manager, ancestorProfileDraft(ScopeGlobal))
	draft := ancestorProfileDraft(ScopeGlobal)
	draft.Name = "second"
	second := requireProfilePlan(t, manager, draft)
	first.state.publication.profiles = append(first.state.publication.profiles, second.state.publication.profiles...)
	forced := errors.New("forced second Profile failure")
	manager.publication.writeProfile = func(profile *pendingProfilePublication) error {
		if filepath.Base(profile.directory) == "second" {
			return forced
		}
		return writeProfileAtomically(profile)
	}
	if err := manager.Publish(first); !errors.Is(err, forced) {
		t.Fatalf("publication error = %v, want forced failure", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("Profiles or staging survived rollback: %v, error %v", entries, err)
	}
}

func requirePublicationRoot(t *testing.T, scope Scope, path string) *os.Root {
	t.Helper()
	root, err := openPublicationRoot(scope, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}
