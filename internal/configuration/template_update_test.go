package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplateUpdateKeepsExecutionSettingsAndCreatesRevision(t *testing.T) {
	root := t.TempDir()
	oldManager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: []Template{{ID: "bugs", Revision: "old", Instructions: "customized\n"}}})
	plan, err := oldManager.PlanProfileCreation("", ProfileDraft{Target: ScopeGlobal, Name: "bugs", Reviewer: "codex", Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", TemplateID: "bugs", Instructions: "customized\n"})
	if err != nil || !plan.Valid() {
		t.Fatalf("create plan = valid %t, err %v, reason %q", plan.Valid(), err, plan.Reason())
	}
	if err := oldManager.Publish(plan); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: []Template{{ID: "bugs", Revision: "new", Instructions: "packaged new\n"}}})
	drift, err := manager.TemplateDrift("")
	if err != nil || len(drift) != 1 || !drift[0].Customized {
		t.Fatalf("drift = %#v, err %v", drift, err)
	}
	if drift[0].TemplateRevision != "old" {
		t.Fatalf("saved Template revision = %q", drift[0].TemplateRevision)
	}
	plan, err = manager.PlanProfileTemplateUpdate("", ScopeGlobal, "bugs")
	if err != nil || !plan.Valid() {
		t.Fatalf("update plan = valid %t, err %v, reason %q", plan.Valid(), err, plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	updated, found, err := manager.LoadProfile(ScopeGlobal, "", "bugs")
	if err != nil || !found {
		t.Fatal(err)
	}
	if updated.Reviewer != "codex" || updated.Model != "luna" || updated.ReasoningEffort != "high" || updated.AttemptDeadline != "8m" {
		t.Fatalf("execution settings changed: %#v", updated)
	}
	if updated.TemplateRevision != "new" || updated.Instructions != "packaged new\n" {
		t.Fatalf("Template update = %#v", updated)
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", "bugs", "instructions.md")); err != nil {
		t.Fatal(err)
	}
}
