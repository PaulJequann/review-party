package configuration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveRuntimeKeepsSelectionAndProfileMaterialTogether(t *testing.T) {
	root := t.TempDir()
	repository := Repository(t.TempDir())
	manager := testManager(t, root)
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"defaults":{"reviewer":"grok"}}`)
	publishRuntimeProfile(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "bugs", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "old instructions\n",
	})
	writeRuntimeSelection(t, repository, ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []SelectionItem{{Profile: "bugs"}},
	})

	snapshot, err := manager.ResolveRuntime(RunRequest{Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	selection := snapshot.Selection()
	slot := requireRuntimeSlot(t, selection, "bugs")
	oldProfile := requireRuntimeProfile(t, snapshot, slot)
	if oldProfile.Instructions != "old instructions\n" {
		t.Fatalf("captured instructions = %q", oldProfile.Instructions)
	}

	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"defaults":{"reviewer":"opencode"}}`)
	writeDocument(t, filepath.Join(root, "profiles", "bugs", "instructions.md"), "new instructions\n")
	writeRuntimeSelection(t, repository, ReviewSelection{ConcurrencyLimit: 1})

	if got := snapshot.Effective().DefaultReviewer.Value; got != "grok" {
		t.Fatalf("captured default reviewer = %q, want grok", got)
	}
	requireRuntimeSlot(t, snapshot.Selection(), "bugs")
	currentProfile := requireRuntimeProfile(t, snapshot, slot)
	if currentProfile.Instructions != oldProfile.Instructions {
		t.Fatalf("captured instructions changed = %q, want %q", currentProfile.Instructions, oldProfile.Instructions)
	}
	if currentProfile.SourceDigest != oldProfile.SourceDigest {
		t.Fatalf("captured digest changed = %q, want %q", currentProfile.SourceDigest, oldProfile.SourceDigest)
	}
}

func TestResolveRuntimePreservesExplicitProfileSelection(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.profile(t, ScopeGlobal, "documentation")
	fixture.party(t, partySpec{
		scope: ScopeGlobal, name: "baseline", limit: 4,
		members: []ProfileReference{{Scope: ScopeGlobal, Profile: "documentation"}, {Scope: ScopeGlobal, Profile: "bugs"}},
	})
	fixture.reviews(t, ReviewSelection{ConcurrencyLimit: 1, Global: []SelectionItem{{Profile: "bugs"}}, Repository: []SelectionItem{}})

	snapshot, err := fixture.manager.ResolveRuntime(RunRequest{Repository: fixture.repository, Profile: "global:documentation"})
	if err != nil {
		t.Fatal(err)
	}
	selection := snapshot.Selection()
	requireRuntimeSelectionFacts(t, selection, facts{kind: SelectionExplicitProfile, source: LimitExplicitProfile, limit: 1})
	requireRuntimeSlot(t, selection, "documentation")
}

func TestResolveRuntimePreservesExplicitPartySelection(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.profile(t, ScopeGlobal, "documentation")
	fixture.party(t, partySpec{
		scope: ScopeGlobal, name: "baseline", limit: 4,
		members: []ProfileReference{{Scope: ScopeGlobal, Profile: "documentation"}, {Scope: ScopeGlobal, Profile: "bugs"}},
	})

	snapshot, err := fixture.manager.ResolveRuntime(RunRequest{Repository: fixture.repository, Party: "global:baseline"})
	if err != nil {
		t.Fatal(err)
	}
	selection := snapshot.Selection()
	requireRuntimeSelectionFacts(t, selection, facts{kind: SelectionExplicitParty, source: LimitParty, limit: 4})
	requireRuntimeSlot(t, selection, "documentation")
	requireRuntimeSlot(t, selection, "bugs")
	for _, slot := range selection.Expanded {
		requireRuntimeProfile(t, snapshot, slot)
	}
}

func TestResolveRuntimeMatchesExistingSelectionSemantics(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "shared")
	fixture.profile(t, ScopeRepository, "shared")
	fixture.party(t, partySpec{
		scope: ScopeGlobal, name: "baseline", limit: 2,
		members: []ProfileReference{{Scope: ScopeGlobal, Profile: "shared"}},
	})
	fixture.reviews(t, ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []SelectionItem{{Party: "baseline"}},
		Repository:       []SelectionItem{{Profile: "shared"}},
	})

	requests := []RunRequest{
		{Repository: fixture.repository},
		{Repository: fixture.repository, Profile: "shared"},
		{Repository: fixture.repository, Party: "global:baseline"},
	}
	for _, request := range requests {
		snapshot, runtimeErr := fixture.manager.ResolveRuntime(request)
		if runtimeErr != nil {
			t.Fatalf("ResolveRuntime(%#v) error = %v", request, runtimeErr)
		}
		legacy, legacyErr := fixture.manager.ResolveRun(request)
		if legacyErr != nil {
			t.Fatalf("ResolveRun(%#v) error = %v", request, legacyErr)
		}
		if !reflect.DeepEqual(snapshot.Selection(), legacy) {
			t.Fatalf("selection mismatch for %#v\nruntime = %#v\nlegacy = %#v", request, snapshot.Selection(), legacy)
		}
	}
}

func TestResolveRuntimeFailsClosedForMissingSavedProfile(t *testing.T) {
	fixture := newSelectionFixture(t)
	fixture.profile(t, ScopeGlobal, "bugs")
	fixture.reviews(t, ReviewSelection{ConcurrencyLimit: 1, Global: []SelectionItem{{Profile: "ghosts"}}, Repository: []SelectionItem{}})

	snapshot, err := fixture.manager.ResolveRuntime(RunRequest{Repository: fixture.repository})
	if snapshot != nil {
		t.Fatal("missing Profile returned a runtime snapshot")
	}
	var unresolved UnresolvedReferenceError
	if !errors.As(err, &unresolved) {
		t.Fatalf("error = %v, want UnresolvedReferenceError", err)
	}
	if unresolved.Name != "ghosts" {
		t.Fatalf("unresolved name = %q, want ghosts", unresolved.Name)
	}
	if unresolved.SelectedBy != "reviews.global[0]" {
		t.Fatalf("unresolved selectedBy = %q", unresolved.SelectedBy)
	}
	if !containsName(unresolved.Available, "bugs") {
		t.Fatalf("unresolved = %#v", unresolved)
	}
}

func TestResolveRuntimeFailsClosedForIncompleteSavedProfile(t *testing.T) {
	root := t.TempDir()
	repository := Repository(t.TempDir())
	manager := testManager(t, root)
	profileDirectory := filepath.Join(root, "profiles", "broken")
	metadata, err := json.Marshal(Profile{
		SchemaVersion: 1, Name: "broken", Reviewer: "grok", Model: "grok-4.5",
		ReasoningEffort: "high", AttemptDeadline: "1m",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, filepath.Join(profileDirectory, "profile.json"), string(metadata))
	writeRuntimeSelection(t, repository, ReviewSelection{ConcurrencyLimit: 1, Global: []SelectionItem{{Profile: "broken"}}})

	snapshot, err := manager.ResolveRuntime(RunRequest{Repository: repository})
	if snapshot != nil {
		t.Fatal("incomplete Profile returned a runtime snapshot")
	}
	if err == nil {
		t.Fatal("incomplete Profile resolution succeeded")
	}
	if !strings.Contains(err.Error(), `Profile "broken" is incomplete: instructions.md is missing`) {
		t.Fatalf("error = %v, want incomplete Profile failure", err)
	}
}

func TestResolveRuntimeKeepsEffectiveViewForNoSelection(t *testing.T) {
	root := t.TempDir()
	repository := Repository(t.TempDir())
	manager := testManager(t, root)
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"defaults":{"reviewer":"opencode"}}`)

	snapshot, err := manager.ResolveRuntime(RunRequest{Repository: repository})
	if !errors.Is(err, ErrNoRepositorySelection) {
		t.Fatalf("error = %v, want ErrNoRepositorySelection", err)
	}
	if snapshot == nil {
		t.Fatal("no-selection resolution did not retain effective configuration")
	}
	if got := snapshot.Effective().DefaultReviewer.Value; got != "opencode" {
		t.Fatalf("default reviewer = %q, want opencode", got)
	}
	if selection := snapshot.Selection(); len(selection.Authored) != 0 || len(selection.Expanded) != 0 {
		t.Fatalf("no-selection facts = %#v", selection)
	}
	if _, err := os.Stat(filepath.Join(string(repository), ".reviewparty", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("resolution created repository configuration: %v", err)
	}
}

func requireRuntimeSelectionFacts(t *testing.T, selection ResolvedReviews, want facts) {
	t.Helper()
	if selection.Kind != want.kind {
		t.Fatalf("selection kind = %q, want %q", selection.Kind, want.kind)
	}
	if selection.ConcurrencyLimit != want.limit {
		t.Fatalf("concurrency limit = %d, want %d", selection.ConcurrencyLimit, want.limit)
	}
	if selection.LimitSource != want.source {
		t.Fatalf("limit source = %q, want %q", selection.LimitSource, want.source)
	}
}

func requireRuntimeSlot(t *testing.T, selection ResolvedReviews, profileName string) ExpandedProfile {
	t.Helper()
	if len(selection.Expanded) == 0 {
		t.Fatalf("selection has no expanded Profiles, want %q", profileName)
	}
	for _, slot := range selection.Expanded {
		if slot.Profile == profileName {
			return slot
		}
	}
	t.Fatalf("selection = %#v, missing Profile %q", selection.Expanded, profileName)
	return ExpandedProfile{}
}

func requireRuntimeProfile(t *testing.T, snapshot RuntimeSnapshot, slot ExpandedProfile) Profile {
	t.Helper()
	profile, found := snapshot.ProfileFor(slot)
	if !found {
		t.Fatalf("snapshot missing Profile for %#v", slot)
	}
	return profile
}

func publishRuntimeProfile(t *testing.T, manager *Manager, draft ProfileDraft) {
	t.Helper()
	plan, err := manager.PlanProfileCreation("", draft)
	if err != nil || !plan.Valid() {
		t.Fatalf("Profile plan error = %v, reason = %q", err, plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func writeRuntimeSelection(t *testing.T, repository Repository, selection ReviewSelection) {
	t.Helper()
	if selection.Global == nil {
		selection.Global = []SelectionItem{}
	}
	if selection.Repository == nil {
		selection.Repository = []SelectionItem{}
	}
	payload, err := json.Marshal(struct {
		SchemaVersion int             `json:"schema_version"`
		Reviews       ReviewSelection `json:"reviews"`
	}{SchemaVersion: SchemaVersion, Reviews: selection})
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, filepath.Join(string(repository), ".reviewparty", "config.json"), string(payload))
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
