package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func seedCustomizedBugsProfile(t *testing.T, root, revision string) {
	t.Helper()
	manager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: []Template{{ID: "bugs", Revision: revision, Instructions: "customized\n"}}})
	plan, err := manager.PlanProfileCreation("", ProfileDraft{Target: ScopeGlobal, Name: "bugs", Reviewer: "codex", Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", TemplateID: "bugs", Instructions: "customized\n"})
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

func newBugsManager(root, revision string) *Manager {
	instructions := "packaged new\n"
	if revision == "old" {
		instructions = "customized\n"
	}
	return NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: []Template{{ID: "bugs", Revision: revision, Instructions: instructions}}})
}

func TestTemplateDriftFlagsCustomizedInstructions(t *testing.T) {
	root := t.TempDir()
	seedCustomizedBugsProfile(t, root, "old")
	manager := newBugsManager(root, "new")
	drift, err := manager.TemplateDrift("")
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 {
		t.Fatalf("drift = %#v", drift)
	}
	if !drift[0].Customized {
		t.Fatalf("drift customized = %#v", drift[0])
	}
	if drift[0].TemplateRevision != "old" {
		t.Fatalf("saved Template revision = %q", drift[0].TemplateRevision)
	}
}

func TestTemplateUpdateKeepsExecutionSettingsAndCreatesRevision(t *testing.T) {
	root := t.TempDir()
	seedCustomizedBugsProfile(t, root, "old")
	manager := newBugsManager(root, "new")
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
