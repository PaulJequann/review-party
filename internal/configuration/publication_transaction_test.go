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
	plan := Plan{state: &planState{valid: true, publication: publicationPlan{
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
