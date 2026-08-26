package configuration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPartyCreationRejectsPayloadBeyondReadLimit(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	profile := requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "bugs", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "Review bugs.\n",
	})
	if err := manager.Publish(profile); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.PlanPartyCreation("", PartyDraft{
		Target: ScopeGlobal, Name: "large", Description: strings.Repeat("x", maximumPartyBytes),
		ConcurrencyLimit: 1, Profiles: []ProfileReference{{Scope: ScopeGlobal, Profile: "bugs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("oversized Party plan unexpectedly valid")
	}
	if !strings.Contains(plan.Reason(), "exceeds") {
		t.Fatalf("reason = %q", plan.Reason())
	}
}

func TestPartyInventoryReportsMissingProfileReference(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, filepath.Join(root, "parties", "broken.json"), `{
  "schema_version": 1,
  "name": "broken",
  "concurrency_limit": 1,
  "profiles": [{"scope":"global","profile":"missing"}]
}`)
	inventory, err := testManager(t, root).PartyInventory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 1 {
		t.Fatalf("inventory = %#v", inventory)
	}
	if inventory[0].Err == nil {
		t.Fatal("missing Profile reference was accepted")
	}
	if !strings.Contains(inventory[0].Err.Error(), `global Profile "missing" was not found`) {
		t.Fatalf("definition error = %v", inventory[0].Err)
	}
}
