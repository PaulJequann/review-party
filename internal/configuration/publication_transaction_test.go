package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConcurrentProfilePlansPreserveFirstPublication(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	first := requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "FIRST\n",
	})
	second := requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "SECOND\n",
	})
	if err := manager.Publish(first); err != nil {
		t.Fatal(err)
	}
	if err := manager.Publish(second); err == nil {
		t.Fatal("stale concurrent Profile plan unexpectedly published")
	}
	instructions, err := os.ReadFile(filepath.Join(root, "profiles", "security", "instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(instructions) != "FIRST\n" {
		t.Fatalf("instructions = %q", instructions)
	}
}

func TestPublicationRollsBackMixedFileAndProfileTransaction(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	configPlan, err := manager.Plan("", []Intent{SetStateDirectory{Directory: filepath.Join(root, "state")}})
	if err != nil || !configPlan.Valid() {
		t.Fatalf("config plan = valid %t, reason %q, error %v", configPlan.Valid(), configPlan.Reason(), err)
	}
	profilePlan, err := manager.PlanProfileCreation("", ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "Review security boundaries.\n",
	})
	if err != nil || !profilePlan.Valid() {
		t.Fatalf("Profile plan = valid %t, reason %q, error %v", profilePlan.Valid(), profilePlan.Reason(), err)
	}
	plan := Plan{state: &planState{owner: manager, valid: true, publication: publicationPlan{
		files: configPlan.state.publication.files, profiles: profilePlan.state.publication.profiles,
	}}}
	manager.publication.writeProfile = func(*pendingProfilePublication) error {
		return errors.New("forced aggregate publication failure")
	}
	if err := manager.Publish(plan); err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("configuration survived rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", "security")); !os.IsNotExist(err) {
		t.Fatalf("Profile survived rollback: %v", err)
	}
}

func TestPublicationClosesOpenedRoots(t *testing.T) {
	for _, failure := range []string{"none", "file-staging", "file-committed", "profile-staging", "profile-committed", "rollback"} {
		t.Run(failure, func(t *testing.T) {
			anchor := t.TempDir()
			plan := publicationPlan{
				files: []pendingWrite{{scope: ScopeGlobal, anchor: anchor, path: filepath.Join(anchor, "config.json"), payload: []byte("config")}},
				profiles: []pendingProfilePublication{{
					scope: ScopeGlobal, anchor: anchor, directory: filepath.Join(anchor, "profiles", "security"),
					metadata: []byte("metadata"), instructions: []byte("instructions"),
				}},
			}
			forced := errors.New("forced post-commit failure")
			var roots []*os.Root
			module := newPublicationModule()
			module.writeFile = func(write *pendingWrite) error {
				if failure == "file-staging" {
					if err := os.Mkdir(write.path, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				err := writeAtomically(write)
				roots = append(roots, write.root)
				if err == nil && failure == "file-committed" {
					return forced
				}
				return err
			}
			module.writeProfile = func(profile *pendingProfilePublication) error {
				if failure == "profile-staging" {
					writeDocument(t, filepath.Dir(profile.directory), "obstructed")
				}
				err := writeProfileAtomically(profile)
				roots = append(roots, profile.root)
				if err == nil && failure == "rollback" {
					if err := profile.root.Close(); err != nil {
						t.Fatal(err)
					}
					return forced
				}
				if err == nil && failure == "profile-committed" {
					return forced
				}
				return err
			}
			err := module.publish(plan)
			if (err == nil) != (failure == "none") {
				t.Fatalf("publication error = %v for failure %q", err, failure)
			}
			if failure == "rollback" && (!errors.Is(err, forced) || !errors.Is(err, os.ErrClosed)) {
				t.Fatalf("publication error = %v, want commit and rollback failures", err)
			}
			for index, root := range roots {
				if root == nil {
					t.Fatalf("write %d did not open a root", index)
				}
				if _, err := root.Stat("."); !errors.Is(err, os.ErrClosed) {
					t.Errorf("root %d survived publication: %v", index, err)
				}
			}
			if plan.files[0].root != nil || plan.profiles[0].root != nil || plan.files[0].complete || plan.profiles[0].complete {
				t.Fatal("publication mutated the reusable plan")
			}
		})
	}
}
