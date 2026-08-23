package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanOmitsScopeWhenClearIntentChangesNothing(t *testing.T) {
	personalRoot := t.TempDir()
	manager := testManager(t, personalRoot)

	plan, err := manager.Plan("", []Intent{SetStateDirectory{Directory: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan reason = %q, want valid no-op", plan.Reason())
	}
	assertEmptyPlanPreview(t, plan)
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(personalRoot, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("no-op publication created configuration: %v", err)
	}
}

func assertEmptyPlanPreview(t *testing.T, plan Plan) {
	t.Helper()
	if len(plan.Changes()) != 0 {
		t.Fatalf("no-op changes = %v", plan.Changes())
	}
	if len(plan.Scopes()) != 0 {
		t.Fatalf("no-op scopes = %v", plan.Scopes())
	}
	if len(plan.Paths()) != 0 {
		t.Fatalf("no-op paths = %v", plan.Paths())
	}
}

func TestLoadRejectsNullEvalRetryPolicy(t *testing.T) {
	personalRoot := t.TempDir()
	path := filepath.Join(personalRoot, "config.json")
	writeDocument(t, path, `{"schema_version":1,"eval":{"retry_policy":null}}`)

	_, err := testManager(t, personalRoot).Load("")
	var invalid InvalidDocumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidDocumentError", err)
	}
	if !strings.Contains(invalid.Reason, "retry_policy must not be null") {
		t.Fatalf("reason = %q, want null retry-policy rejection", invalid.Reason)
	}
}
