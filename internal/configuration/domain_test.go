package configuration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIncompleteRepositoryProfilePreventsGlobalFallback(t *testing.T) {
	globalRoot := t.TempDir()
	repository := t.TempDir()
	manager := testManager(t, globalRoot)
	plan := requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "GLOBAL\n",
	})
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(repository, ".reviewparty", "profiles", "security")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "instructions.md"), []byte("INCOMPLETE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := manager.ResolveProfile(Repository(repository), "security")
	if err == nil || !strings.Contains(err.Error(), "profile.json is missing") {
		t.Fatalf("error = %v", err)
	}
}

func TestProfilePublicationRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "profiles")); err != nil {
		t.Fatal(err)
	}
	manager := testManager(t, root)
	plan := requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "Review security.\n",
	})
	if err := manager.Publish(plan); err == nil {
		t.Fatal("publication followed an escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "security")); !os.IsNotExist(err) {
		t.Fatalf("outside Profile was created: %v", err)
	}
}

func TestProfilePublicationRejectsStalePlan(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	plan, err := manager.PlanProfileCreation("", ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "Review security boundaries.\n",
	})
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = valid %t, reason %q, error %v", plan.Valid(), plan.Reason(), err)
	}
	metadata := filepath.Join(root, "profiles", "security", "profile.json")
	writeDocument(t, metadata, `{"concurrent":"edit"}`)
	if err := manager.Publish(plan); err == nil || !strings.Contains(err.Error(), "stale change plan") {
		t.Fatalf("publish error = %v", err)
	}
	payload, err := os.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"concurrent":"edit"}` {
		t.Fatalf("concurrent edit changed: %s", payload)
	}
}

func TestTemplateSeedsProfileWithoutBecomingExecutable(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	manager.templates = []Template{{ID: "bugs", Revision: "bugs-v4", Instructions: "Find material bugs.\n"}}
	_, found, err := manager.LoadProfile(ScopeGlobal, "", "bugs")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("packaged Template resolved as an executable Profile")
	}
	plan := requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "bugs", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", TemplateID: "bugs",
	})
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	profile := requireProfile(t, manager, ScopeGlobal, "bugs")
	if profile.TemplateID != "bugs" {
		t.Fatalf("Template ID = %q", profile.TemplateID)
	}
	if profile.TemplateRevision != "bugs-v4" {
		t.Fatalf("Template revision = %q", profile.TemplateRevision)
	}
	if profile.Instructions != "Find material bugs.\n" {
		t.Fatalf("instructions = %q", profile.Instructions)
	}
}

func requireProfilePlan(t *testing.T, manager *Manager, draft ProfileDraft) Plan {
	t.Helper()
	plan, err := manager.PlanProfileCreation("", draft)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("invalid plan: %s", plan.Reason())
	}
	return plan
}

func requireProfile(t *testing.T, manager *Manager, scope Scope, name string) Profile {
	t.Helper()
	profile, found, err := manager.LoadProfile(scope, "", name)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("Profile %q not found", name)
	}
	return profile
}

func TestGlobalPartyCannotReferenceRepositoryProfile(t *testing.T) {
	manager := testManager(t, t.TempDir())
	err := manager.validateParty(Party{
		SchemaVersion: SchemaVersion, Name: "baseline", ConcurrencyLimit: 1,
		Profiles: []ProfileReference{{Scope: ScopeRepository, Profile: "security"}},
	}, ScopeGlobal)
	if err == nil || !strings.Contains(err.Error(), "only Global Profiles") {
		t.Fatalf("error = %v", err)
	}
}

func TestPartyReaderRejectsSupersededCompositionFields(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, filepath.Join(root, "parties", "baseline.json"), `{
  "schema_version": 1,
  "name": "baseline",
  "extends": ["other"],
  "concurrency_limit": 1,
  "profiles": [{"scope": "global", "profile": "bugs"}]
}`)
	manager := testManager(t, root)
	_, _, err := manager.LoadParty(ScopeGlobal, "", "baseline")
	if err == nil || !strings.Contains(err.Error(), `unknown field "extends"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestRepositoryReviewSelectionPreservesScopeGroupOrder(t *testing.T) {
	root := t.TempDir()
	repository := t.TempDir()
	manager := testManager(t, root)
	selection := ReviewSelection{
		ConcurrencyLimit: 2,
		Global:           []SelectionItem{{Party: "baseline"}, {Profile: "documentation"}},
		Repository:       []SelectionItem{{Profile: "security"}},
	}
	plan, err := manager.Plan(Repository(repository), []Intent{SetReviewSelection{Selection: selection}})
	if err != nil || !plan.Valid() {
		t.Fatalf("plan error = %v, reason = %q", err, plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := manager.EffectiveReviewSelection(Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved, selection) {
		t.Fatalf("selection = %#v, want %#v", resolved, selection)
	}
}

func TestGlobalProfilesDoNotCreateDefaultSelection(t *testing.T) {
	root := t.TempDir()
	repository := t.TempDir()
	manager := testManager(t, root)
	plan, err := manager.PlanProfileCreation("", ProfileDraft{
		Target: ScopeGlobal, Name: "security", Reviewer: "opencode", Model: "muse",
		ReasoningEffort: "high", AttemptDeadline: "2m", Instructions: "Review security boundaries.\n",
	})
	if err != nil || !plan.Valid() {
		t.Fatalf("plan error = %v, reason = %q", err, plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	selection, value, err := manager.EffectiveReviewSelection(Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	if value.Authored {
		t.Fatalf("selection was unexpectedly authored: %#v", value)
	}
	if len(selection.Global)+len(selection.Repository) != 0 {
		t.Fatalf("Global Profile enabled repository Reviews: %#v", selection)
	}
}
