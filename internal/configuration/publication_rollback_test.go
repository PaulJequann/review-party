package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProfileRollbackRetainsRootAfterPathReplacement(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "missing-root"
		if existing {
			name = "existing-root"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "config", "review-party")
			if existing {
				if err := os.MkdirAll(root, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			manager := testManager(t, root)
			plan := requireProfilePlan(t, manager, ancestorProfileDraft(ScopeGlobal))
			draft := ancestorProfileDraft(ScopeGlobal)
			draft.Name = "second"
			second := requireProfilePlan(t, manager, draft)
			plan.state.publication.profiles = append(plan.state.publication.profiles, second.state.publication.profiles...)
			forced := errors.New("forced second Profile failure")
			var retained string
			manager.publication.writeProfile = func(profile *pendingProfilePublication) error {
				if filepath.Base(profile.directory) == "second" {
					retained = replacePublicationRoot(t, root)
					return forced
				}
				return writeProfileAtomically(profile)
			}
			if err := manager.Publish(plan); !errors.Is(err, forced) {
				t.Fatalf("publication error = %v, want forced failure", err)
			}
			assertReplacementPublicationIntact(t, root)
			entries, err := os.ReadDir(filepath.Join(retained, "profiles"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("Profiles or staging survived rollback in renamed root: %v, error %v", entries, err)
			}
		})
	}
}

func TestFileRollbackRetainsRootAfterPathReplacement(t *testing.T) {
	for _, initial := range []string{"missing-root", "existing-root", "existing-file"} {
		for _, failure := range []string{"file", "profile"} {
			t.Run(initial+"/"+failure, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "config", "review-party")
				repository := t.TempDir()
				if initial != "missing-root" {
					if err := os.MkdirAll(root, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				const original = `{"schema_version":1,"defaults":{"reviewer":"opencode"}}`
				if initial == "existing-file" {
					writeDocument(t, filepath.Join(root, "config.json"), original)
				}
				manager := testManager(t, root)
				plan, err := manager.Plan(Repository(repository), []Intent{
					SetStateDirectory{Directory: filepath.Join(root, "state")},
					SetDefaultReviewer{Target: ScopeRepository, Reviewer: "codex"},
				})
				plan = requireValidPlan(t, "configuration", plan, err)
				profile := requireProfilePlan(t, manager, ancestorProfileDraft(ScopeGlobal))
				plan.state.publication.profiles = profile.state.publication.profiles
				forced := errors.New("forced later publication failure")
				var retained string
				fail := func() error {
					retained = replacePublicationRoot(t, root)
					return forced
				}
				manager.publication.writeFile = func(write *pendingWrite) error {
					if write.scope == ScopeRepository && failure == "file" {
						return fail()
					}
					return writeAtomically(write)
				}
				manager.publication.writeProfile = func(*pendingProfilePublication) error { return fail() }
				if err := manager.Publish(plan); !errors.Is(err, forced) {
					t.Fatalf("publication error = %v, want forced failure", err)
				}
				assertReplacementPublicationIntact(t, root)
				assertPublicationPathAbsent(t, filepath.Join(repository, ".reviewparty", "config.json"))
				path := filepath.Join(retained, "config.json")
				if initial == "existing-file" {
					payload, err := os.ReadFile(path)
					if err != nil || string(payload) != original {
						t.Fatalf("configuration in renamed root = %q, error %v, want %q", payload, err, original)
					}
				} else {
					assertPublicationPathAbsent(t, path)
				}
			})
		}
	}
}

func replacePublicationRoot(t *testing.T, root string) string {
	t.Helper()
	anchor := filepath.Dir(root)
	retained := anchor + "-retained"
	if err := os.Rename(anchor, retained); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, filepath.Join(root, "config.json"), "unrelated configuration")
	writeDocument(t, filepath.Join(root, "profiles", "security", "profile.json"), "unrelated metadata")
	writeDocument(t, filepath.Join(root, "profiles", "security", "instructions.md"), "unrelated instructions")
	return filepath.Join(retained, filepath.Base(root))
}

func assertReplacementPublicationIntact(t *testing.T, root string) {
	t.Helper()
	for path, want := range map[string]string{
		"config.json": "unrelated configuration",
		filepath.Join("profiles", "security", "profile.json"):    "unrelated metadata",
		filepath.Join("profiles", "security", "instructions.md"): "unrelated instructions",
	} {
		payload, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(payload) != want {
			t.Errorf("unrelated replacement %q = %q, error %v, want %q", path, payload, err, want)
		}
	}
}
