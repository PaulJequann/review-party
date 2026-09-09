package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func seedBugsProfile(t *testing.T, root, templateInstructions, profileInstructions string) {
	t.Helper()
	manager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: []Template{{ID: "bugs", Revision: "old", Instructions: templateInstructions}}})
	plan, err := manager.PlanProfileCreation("", ProfileDraft{Target: ScopeGlobal, Name: "bugs", Reviewer: "codex", Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", TemplateID: "bugs", Instructions: profileInstructions})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("create plan reason %q", plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func newBugsManager(root, revision, instructions string) *Manager {
	return NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: []Template{{ID: "bugs", Revision: revision, Instructions: instructions}}})
}

func driftForBugs(t *testing.T, manager *Manager) TemplateDrift {
	t.Helper()
	drift, err := manager.TemplateDrift("")
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 {
		t.Fatalf("drift = %#v", drift)
	}
	return drift[0]
}

func TestTemplateDriftFlagsCustomizedInstructions(t *testing.T) {
	root := t.TempDir()
	seedBugsProfile(t, root, "packaged old\n", "my tweaks\n")
	entry := driftForBugs(t, newBugsManager(root, "new", "packaged new\n"))
	if !entry.Customized {
		t.Fatalf("drift customized = %#v", entry)
	}
	if entry.TemplateRevision != "old" {
		t.Fatalf("saved Template revision = %q", entry.TemplateRevision)
	}
}

func TestTemplateDriftLeavesUntouchedProfileUnflagged(t *testing.T) {
	root := t.TempDir()
	seedBugsProfile(t, root, "packaged old\n", "")
	entry := driftForBugs(t, newBugsManager(root, "new", "packaged new\n"))
	if entry.Customized {
		t.Fatalf("untouched drift customized = %#v", entry)
	}
}

func TestTemplateUpdateKeepsExecutionSettingsAndCreatesRevision(t *testing.T) {
	root := t.TempDir()
	seedBugsProfile(t, root, "packaged old\n", "my tweaks\n")
	manager := newBugsManager(root, "new", "packaged new\n")
	plan, err := manager.PlanProfileTemplateUpdate("", ScopeGlobal, "bugs")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("update plan reason %q", plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	assertTemplateUpdateExecution(t, manager)
	assertTemplateUpdateInstructions(t, manager, root)
	if drift, err := manager.TemplateDrift(""); err != nil || len(drift) != 0 {
		t.Fatalf("drift after update = %#v, err %v", drift, err)
	}
}

func assertTemplateUpdateExecution(t *testing.T, manager *Manager) {
	t.Helper()
	updated, found, err := manager.LoadProfile(ScopeGlobal, "", "bugs")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("bugs profile missing after update")
	}
	if updated.Reviewer != "codex" {
		t.Errorf("reviewer = %q", updated.Reviewer)
	}
	if updated.Model != "luna" {
		t.Errorf("model = %q", updated.Model)
	}
	if updated.ReasoningEffort != "high" {
		t.Errorf("effort = %q", updated.ReasoningEffort)
	}
	if updated.AttemptDeadline != "8m" {
		t.Errorf("deadline = %q", updated.AttemptDeadline)
	}
}

func assertTemplateUpdateInstructions(t *testing.T, manager *Manager, root string) {
	t.Helper()
	updated, found, err := manager.LoadProfile(ScopeGlobal, "", "bugs")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("bugs profile missing after update")
	}
	if updated.TemplateRevision != "new" {
		t.Errorf("Template revision = %q", updated.TemplateRevision)
	}
	if updated.Instructions != "packaged new\n" {
		t.Errorf("instructions = %q", updated.Instructions)
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", "bugs", "instructions.md")); err != nil {
		t.Error(err)
	}
}
