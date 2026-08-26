package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanRejectsEnabledSameScopeModelMismatchWithoutWriting(t *testing.T) {
	globalRoot := t.TempDir()
	manager := testManager(t, globalRoot)
	globalPath := filepath.Join(globalRoot, "config.json")

	plan, err := manager.Plan("", []Intent{
		SetReviewerModel{Target: ScopeGlobal, Reviewer: "opencode", Model: "model-a"},
		SetReviewerAllowedModels{Target: ScopeGlobal, Reviewer: "opencode", Models: []string{"model-b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("plan is valid, want effective policy rejection")
	}
	if !strings.Contains(plan.Reason(), globalPath) {
		t.Fatalf("plan reason = %q, want path %q", plan.Reason(), globalPath)
	}
	assertPublishRefused(t, manager, plan)
	assertFileAbsent(t, globalPath)
}

func TestPlanAllowsDisabledSameScopeModelMismatch(t *testing.T) {
	globalRoot := t.TempDir()
	manager := testManager(t, globalRoot)

	plan, err := manager.Plan("", []Intent{
		SetReviewerEnabled{Target: ScopeGlobal, Reviewer: "opencode", Enabled: false},
		SetReviewerModel{Target: ScopeGlobal, Reviewer: "opencode", Model: "model-a"},
		SetReviewerAllowedModels{Target: ScopeGlobal, Reviewer: "opencode", Models: []string{"model-b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan reason = %q, want valid disabled policy", plan.Reason())
	}
}

func TestPlanRejectsAllowlistExcludingPackagedModelWithoutWriting(t *testing.T) {
	globalRoot := t.TempDir()
	manager := testManager(t, globalRoot)
	globalPath := filepath.Join(globalRoot, "config.json")

	plan, err := manager.Plan("", []Intent{
		SetReviewerAllowedModels{Target: ScopeGlobal, Reviewer: "grok", Models: []string{"other-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("plan is valid, want packaged model rejection")
	}
	for _, fragment := range []string{"grok-4.5", "packaged configuration", globalPath} {
		if !strings.Contains(plan.Reason(), fragment) {
			t.Fatalf("plan reason = %q, want %q", plan.Reason(), fragment)
		}
	}
	assertPublishRefused(t, manager, plan)
	assertFileAbsent(t, globalPath)
}

func TestPlanAllowsAllowlistContainingPackagedModel(t *testing.T) {
	manager := testManager(t, t.TempDir())
	plan, err := manager.Plan("", []Intent{
		SetReviewerAllowedModels{Target: ScopeGlobal, Reviewer: "grok", Models: []string{"grok-4.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan reason = %q, want packaged model allowed", plan.Reason())
	}
}

func TestPlanValidatesStagedScopeAgainstUntouchedScopeWithoutWriting(t *testing.T) {
	globalRoot := t.TempDir()
	repository := t.TempDir()
	globalPath := filepath.Join(globalRoot, "config.json")
	repositoryPath := filepath.Join(repository, ".reviewparty", "config.json")
	const globalDocument = `{"schema_version":1,"reviewers":{"opencode":{"allowed_models":["model-b"]}}}`
	writeDocument(t, globalPath, globalDocument)
	manager := testManager(t, globalRoot)

	plan, err := manager.Plan(Repository(repository), []Intent{
		SetReviewerModel{Target: ScopeRepository, Reviewer: "opencode", Model: "model-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("plan is valid, want effective policy rejection")
	}
	for _, path := range []string{repositoryPath, globalPath} {
		if !strings.Contains(plan.Reason(), path) {
			t.Fatalf("plan reason = %q, want path %q", plan.Reason(), path)
		}
	}
	assertPublishRefused(t, manager, plan)
	assertFileAbsent(t, repositoryPath)
	payload, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != globalDocument {
		t.Fatalf("invalid plan changed global configuration: %s", payload)
	}
}

func assertPublishRefused(t *testing.T, manager *Manager, plan Plan) {
	t.Helper()
	err := manager.Publish(plan)
	if err == nil || !strings.Contains(err.Error(), plan.Reason()) {
		t.Fatalf("Publish() error = %v, want refusal preserving plan reason", err)
	}
}

func assertFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid plan wrote configuration %q: %v", path, err)
	}
}
