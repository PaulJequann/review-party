package configuration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSkillTemplatesDiscoverCallerSkillsInRootOrder(t *testing.T) {
	primary, secondary, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	writeDocument(t, filepath.Join(primary, "audit", "SKILL.md"), "Primary audit.")
	writeDocument(t, filepath.Join(primary, ".trash", "SKILL.md"), "Trashed.")
	writeDocument(t, filepath.Join(primary, "notes", "README.md"), "No skill here.")
	writeDocument(t, filepath.Join(secondary, "audit", "SKILL.md"), "Shadowed audit.")
	writeDocument(t, filepath.Join(elsewhere, "linked", "SKILL.md"), "Linked skill.")
	writeDocument(t, filepath.Join(elsewhere, "linked", "scripts", "run.sh"), "echo")
	if err := os.Symlink(filepath.Join(elsewhere, "linked"), filepath.Join(secondary, "linked")); err != nil {
		t.Fatal(err)
	}

	templates := SkillTemplates([]string{primary, secondary, filepath.Join(primary, "missing")})

	var ids []string
	for _, template := range templates {
		ids = append(ids, template.ID)
	}
	if !reflect.DeepEqual(ids, []string{"skill:audit", "skill:linked"}) {
		t.Fatalf("template IDs = %#v", ids)
	}
	if !strings.HasSuffix(templates[0].Instructions, "\nPrimary audit.\n") {
		t.Fatalf("audit came from the shadowed root: %q", templates[0].Instructions)
	}
	if !reflect.DeepEqual(templates[1].BundledFiles, []string{"scripts/run.sh"}) {
		t.Fatalf("linked bundled files = %#v", templates[1].BundledFiles)
	}
}

func TestSkillTemplateFramesBodyAndListsBundledFiles(t *testing.T) {
	skills := t.TempDir()
	writeDocument(t, filepath.Join(skills, "audit", "SKILL.md"), "---\nname: audit\ndescription: frontmatter text\n---\n\nAudit the tests.\n")
	writeDocument(t, filepath.Join(skills, "audit", "references", "rubric.md"), "rubric")
	writeDocument(t, filepath.Join(skills, "audit", "agents", "openai.yaml"), "ui")
	writeDocument(t, filepath.Join(skills, "audit", "references", ".gitignore"), "*.tmp")
	writeDocument(t, filepath.Join(skills, "audit", ".cache", "state.json"), "{}")

	templates := SkillTemplates([]string{skills})

	if len(templates) != 1 {
		t.Fatalf("templates = %#v", templates)
	}
	audit := templates[0]
	if !strings.HasPrefix(audit.Instructions, "This Profile was imported from the `audit` skill.") || !strings.HasSuffix(audit.Instructions, "---\n\nAudit the tests.\n") {
		t.Fatalf("audit instructions = %q", audit.Instructions)
	}
	if strings.Contains(audit.Instructions, "frontmatter text") || strings.Contains(audit.Instructions, skills) {
		t.Fatalf("audit instructions leak frontmatter or path: %q", audit.Instructions)
	}
	if !reflect.DeepEqual(audit.BundledFiles, []string{"references/rubric.md"}) {
		t.Fatalf("audit bundled files = %#v", audit.BundledFiles)
	}
}

func TestSkillTemplateCreationWarnsAboutUnreadableBundledFiles(t *testing.T) {
	skills := t.TempDir()
	writeDocument(t, filepath.Join(skills, "review", "SKILL.md"), "Review carefully.")
	for _, name := range []string{"a.md", "b.md", "c.md", "d.md", "e.md", "f.md"} {
		writeDocument(t, filepath.Join(skills, "review", "references", name), name)
	}
	manager := NewManager(Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}, Templates: SkillTemplates([]string{skills})})

	plan := requireProfilePlan(t, manager, skillProfileDraft("review"))

	want := "Template skill:review bundles 6 files the Reviewer cannot read: references/a.md, references/b.md, references/c.md, references/d.md, references/e.md, and 1 more"
	if !reflect.DeepEqual(plan.Warnings(), []string{want}) {
		t.Fatalf("warnings = %#v", plan.Warnings())
	}
}

func TestEditedSkillSurfacesAsTemplateDriftAndUpdates(t *testing.T) {
	skills, root := t.TempDir(), t.TempDir()
	skillPath := filepath.Join(skills, "audit", "SKILL.md")
	writeDocument(t, skillPath, "---\nname: audit\n---\nFirst rubric.\n")
	original := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: SkillTemplates([]string{skills})})
	requirePublishedPlan(t, original, requireProfilePlan(t, original, skillProfileDraft("audit")), nil)

	writeDocument(t, skillPath, "---\nname: audit\n---\nSecond rubric.\n")
	manager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: SkillTemplates([]string{skills})})

	before, _ := original.Template("skill:audit")
	after, _ := manager.Template("skill:audit")
	want := []TemplateDrift{{Scope: ScopeGlobal, Profile: "audit", TemplateID: "skill:audit", TemplateRevision: before.Revision, AvailableRevision: after.Revision}}
	drift, err := manager.TemplateDrift("")
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision == after.Revision || !reflect.DeepEqual(drift, want) {
		t.Fatalf("drift = %#v, want %#v", drift, want)
	}
	plan, err := manager.PlanProfileTemplateUpdate("", ScopeGlobal, "audit")
	requirePublishedPlan(t, manager, plan, err)
	if profile := requireProfile(t, manager, ScopeGlobal, "audit"); !strings.HasSuffix(profile.Instructions, "\nSecond rubric.\n") {
		t.Fatalf("updated instructions = %q", profile.Instructions)
	}
}

func TestTemplateUpdateRejectsSkillThatOutgrowsProfileLimit(t *testing.T) {
	skills, root := t.TempDir(), t.TempDir()
	skillPath := filepath.Join(skills, "audit", "SKILL.md")
	writeDocument(t, skillPath, "First rubric.\n")
	original := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: SkillTemplates([]string{skills})})
	requirePublishedPlan(t, original, requireProfilePlan(t, original, skillProfileDraft("audit")), nil)

	writeDocument(t, skillPath, strings.Repeat("x", MaximumDocumentBytes-16)+"\n")
	manager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: SkillTemplates([]string{skills})})
	plan, err := manager.PlanProfileTemplateUpdate("", ScopeGlobal, "audit")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() || !strings.Contains(plan.Reason(), "Profile instructions exceeds") {
		t.Fatalf("valid = %v, reason = %q", plan.Valid(), plan.Reason())
	}
	if profile := requireProfile(t, manager, ScopeGlobal, "audit"); !strings.HasSuffix(profile.Instructions, "\nFirst rubric.\n") {
		t.Fatalf("instructions = %q", profile.Instructions)
	}
}

func TestDeletedSkillSurfacesAsUnavailableTemplateSource(t *testing.T) {
	skills, root := t.TempDir(), t.TempDir()
	writeDocument(t, filepath.Join(skills, "audit", "SKILL.md"), "Audit the tests.")
	original := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: SkillTemplates([]string{skills})})
	requirePublishedPlan(t, original, requireProfilePlan(t, original, skillProfileDraft("audit")), nil)
	recorded, _ := original.Template("skill:audit")

	if err := os.RemoveAll(filepath.Join(skills, "audit")); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Options{GlobalRoot: root, Reviewers: []string{"codex"}, Templates: SkillTemplates([]string{skills})})

	drift, err := manager.TemplateDrift("")
	if err != nil {
		t.Fatal(err)
	}
	want := []TemplateDrift{{Scope: ScopeGlobal, Profile: "audit", TemplateID: "skill:audit", TemplateRevision: recorded.Revision, SourceUnavailable: true}}
	if !reflect.DeepEqual(drift, want) {
		t.Fatalf("drift = %#v, want %#v", drift, want)
	}
}

func skillProfileDraft(skill string) ProfileDraft {
	return ProfileDraft{Target: ScopeGlobal, Name: skill, Reviewer: "codex", Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", TemplateID: "skill:" + skill}
}
