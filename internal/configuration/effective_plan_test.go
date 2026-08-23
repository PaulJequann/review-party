package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanRejectsEnabledSameScopeModelMismatchWithoutWriting(t *testing.T) {
	personalRoot := t.TempDir()
	manager := testManager(t, personalRoot)
	personalPath := filepath.Join(personalRoot, "config.json")

	plan, err := manager.Plan("", []Intent{
		SetReviewerModel{Target: ScopePersonal, Reviewer: "opencode", Model: "model-a"},
		SetReviewerAllowedModels{Target: ScopePersonal, Reviewer: "opencode", Models: []string{"model-b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("plan is valid, want effective policy rejection")
	}
	if !strings.Contains(plan.Reason(), personalPath) {
		t.Fatalf("plan reason = %q, want path %q", plan.Reason(), personalPath)
	}
	assertPublishRefused(t, manager, plan)
	assertFileAbsent(t, personalPath)
}

func TestPlanAllowsDisabledSameScopeModelMismatch(t *testing.T) {
	personalRoot := t.TempDir()
	manager := testManager(t, personalRoot)

	plan, err := manager.Plan("", []Intent{
		SetReviewerEnabled{Target: ScopePersonal, Reviewer: "opencode", Enabled: false},
		SetReviewerModel{Target: ScopePersonal, Reviewer: "opencode", Model: "model-a"},
		SetReviewerAllowedModels{Target: ScopePersonal, Reviewer: "opencode", Models: []string{"model-b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan reason = %q, want valid disabled policy", plan.Reason())
	}
}

func TestPlanRejectsAllowlistExcludingPackagedModelWithoutWriting(t *testing.T) {
	personalRoot := t.TempDir()
	manager := testManager(t, personalRoot)
	personalPath := filepath.Join(personalRoot, "config.json")

	plan, err := manager.Plan("", []Intent{
		SetReviewerAllowedModels{Target: ScopePersonal, Reviewer: "grok", Models: []string{"other-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("plan is valid, want packaged model rejection")
	}
	for _, fragment := range []string{"grok-4.5", "packaged configuration", personalPath} {
		if !strings.Contains(plan.Reason(), fragment) {
			t.Fatalf("plan reason = %q, want %q", plan.Reason(), fragment)
		}
	}
	assertPublishRefused(t, manager, plan)
	assertFileAbsent(t, personalPath)
}

func TestPlanAllowsAllowlistContainingPackagedModel(t *testing.T) {
	manager := testManager(t, t.TempDir())
	plan, err := manager.Plan("", []Intent{
		SetReviewerAllowedModels{Target: ScopePersonal, Reviewer: "grok", Models: []string{"grok-4.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan reason = %q, want packaged model allowed", plan.Reason())
	}
}

func TestPlanValidatesStagedScopeAgainstUntouchedScopeWithoutWriting(t *testing.T) {
	personalRoot := t.TempDir()
	repository := t.TempDir()
	personalPath := filepath.Join(personalRoot, "config.json")
	repositoryPath := filepath.Join(repository, ".reviewparty", "config.json")
	const personalDocument = `{"schema_version":1,"reviewers":{"opencode":{"allowed_models":["model-b"]}}}`
	writeDocument(t, personalPath, personalDocument)
	manager := testManager(t, personalRoot)

	plan, err := manager.Plan(Repository(repository), []Intent{
		SetReviewerModel{Target: ScopeRepository, Reviewer: "opencode", Model: "model-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("plan is valid, want effective policy rejection")
	}
	for _, path := range []string{repositoryPath, personalPath} {
		if !strings.Contains(plan.Reason(), path) {
			t.Fatalf("plan reason = %q, want path %q", plan.Reason(), path)
		}
	}
	assertPublishRefused(t, manager, plan)
	assertFileAbsent(t, repositoryPath)
	payload, err := os.ReadFile(personalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != personalDocument {
		t.Fatalf("invalid plan changed personal configuration: %s", payload)
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
