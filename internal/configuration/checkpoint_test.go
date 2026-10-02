package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestMatchPathPattern(t *testing.T) {
	for _, test := range []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.md", "README.md", true},
		{"*.md", "docs/a/b.md", true},
		{"*.md", "docs/a/b.go", false},
		{"*.md", "notes.md/main.go", false},
		{"docs/**", "docs/guide.md", true},
		{"docs/**", "docs/a/b/c.go", true},
		{"docs/**", "src/docs/a.go", false},
		{"docs/**", "documentation/a.go", false},
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/a/b.md", false},
		{"**/testdata/**", "testdata/x.json", true},
		{"**/testdata/**", "internal/engine/testdata/x.json", true},
		{"**/testdata/**", "internal/engine/x.json", false},
		{"a/**/b.go", "a/b.go", true},
		{"a/**/b.go", "a/x/y/b.go", true},
		{"a/**/b.go", "a/x/y/c.go", false},
		{"**/a/b", "a/a/b", true},
		{"a/**/b/c", "a/b/x/b/c", true},
		{"**/a/**/a", "x/a/y/a", true},
		{"**/a/**/a", "x/a/y/b", false},
		{strings.Repeat("**/", 40) + "missing", strings.Repeat("d/", 60) + "file", false},
		{"?.txt", "a.txt", true},
		{"?.txt", "ab.txt", false},
		{"*", "docs/a.go", true},
		{"src/main.go", "src/main.go", true},
		{"src/main.go", "other/src/main.go", false},
	} {
		if got := matchPathPattern(test.pattern, test.path); got != test.want {
			t.Errorf("matchPathPattern(%q, %q) = %t, want %t", test.pattern, test.path, got, test.want)
		}
	}
}

func TestValidatePathPattern(t *testing.T) {
	for _, pattern := range []string{"*.md", "docs/**", "**/testdata/**", "a/[bc]/d"} {
		if err := ValidatePathPattern(pattern); err != nil {
			t.Errorf("ValidatePathPattern(%q) = %v", pattern, err)
		}
	}
	for _, pattern := range []string{"", "/docs/**", "docs/", "docs//a", "./docs", "docs/../x", "a/[b"} {
		if err := ValidatePathPattern(pattern); err == nil {
			t.Errorf("ValidatePathPattern(%q) accepted a malformed pattern", pattern)
		}
	}
}

func writeRepositoryCheckpoints(t *testing.T, repository, checkpoints string) {
	t.Helper()
	writeDocument(t, filepath.Join(repository, ".reviewparty", "config.json"), `{"schema_version":1,"checkpoints":`+checkpoints+`}`)
}

func TestRepositoryCheckpointsDecodeWithDefaults(t *testing.T) {
	repository := t.TempDir()
	writeRepositoryCheckpoints(t, repository, `{"pre-push":{"requirement":"reviewed","exempt_paths":["*.md"],"integrations":["git","claude-code","codex"]}}`)
	manager := testManager(t, t.TempDir())

	checkpoints, err := manager.Checkpoints(Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	want := map[CheckpointName]Checkpoint{CheckpointPrePush: {
		Requirement: RequirementReviewed, ExemptPaths: []string{"*.md"}, Waivers: WaiversHuman, Integrations: []IntegrationName{IntegrationGit, IntegrationClaudeCode, IntegrationCodex},
	}}
	if !reflect.DeepEqual(checkpoints, want) {
		t.Fatalf("checkpoints = %#v, want %#v", checkpoints, want)
	}
}

func TestRepositoryCheckpointsRejectInvalidDeclarations(t *testing.T) {
	for _, test := range []struct {
		name        string
		checkpoints string
		reason      string
	}{
		{"unknown name", `{"pre-merge":{"requirement":"reviewed"}}`, `unknown checkpoint "pre-merge"`},
		{"missing requirement", `{"pre-push":{}}`, `checkpoints.pre-push.requirement: unknown requirement ""`},
		{"unknown requirement", `{"pre-push":{"requirement":"judged"}}`, `unknown requirement "judged"`},
		{"unknown policy", `{"pre-push":{"requirement":"reviewed","waivers":"robots"}}`, `checkpoints.pre-push.waivers: unknown waiver policy "robots"`},
		{"unknown integration", `{"pre-push":{"requirement":"reviewed","integrations":["cursor"]}}`, `unknown integration "cursor"; expected git, claude-code, or codex`},
		{"duplicate integration", `{"pre-push":{"requirement":"reviewed","integrations":["git","git"]}}`, `integration "git" is listed twice`},
		{"negative size", `{"pre-push":{"requirement":"reviewed","small_change_lines":-1}}`, `small_change_lines: must not be negative`},
		{"malformed pattern", `{"pre-push":{"requirement":"reviewed","exempt_paths":["/docs/**"]}}`, `exempt_paths: pattern "/docs/**" must be relative`},
		{"unknown field", `{"pre-push":{"requirement":"reviewed","bypass":true}}`, `unknown field "bypass"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := t.TempDir()
			writeRepositoryCheckpoints(t, repository, test.checkpoints)
			_, err := testManager(t, t.TempDir()).Load(Repository(repository))
			var invalid InvalidDocumentError
			if !errors.As(err, &invalid) || !strings.Contains(invalid.Reason, test.reason) {
				t.Fatalf("error = %v, want reason containing %q", err, test.reason)
			}
		})
	}
}

func TestGlobalConfigurationRejectsCheckpoints(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"checkpoints":{}}`)
	_, err := testManager(t, root).Load("")
	var invalid InvalidDocumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v", err)
	}
	if invalid.Scope != ScopeGlobal {
		t.Fatalf("scope = %s", invalid.Scope)
	}
	if !strings.Contains(invalid.Reason, "checkpoints is a Repository Configuration field") {
		t.Fatalf("reason = %s", invalid.Reason)
	}
}

// planCheckpoints plans the intents and fails unless the plan is valid.
func planCheckpoints(t *testing.T, manager *Manager, repository string, intents ...Intent) Plan {
	t.Helper()
	plan, err := manager.Plan(Repository(repository), intents)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("invalid plan: %s", plan.Reason())
	}
	return plan
}

func publishPlan(t *testing.T, manager *Manager, plan Plan) {
	t.Helper()
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointPlansSetAndRemove(t *testing.T) {
	repository := t.TempDir()
	manager := testManager(t, t.TempDir())
	checkpoint := NewCheckpoint()
	checkpoint.ExemptPaths = []string{"docs/**"}
	checkpoint.Integrations = []IntegrationName{IntegrationGit}

	plan := planCheckpoints(t, manager, repository, SetCheckpoint{Name: CheckpointPrePush, Checkpoint: checkpoint})
	change := plan.Changes()[0]
	got := [3]string{change.Field, strconv.FormatBool(change.HadBefore), change.After}
	if want := [3]string{"checkpoints.pre-push", "false", `{"requirement":"reviewed","exempt_paths":["docs/**"],"waivers":"human","integrations":["git"]}`}; got != want {
		t.Fatalf("field, had before, after = %v, want %v", got, want)
	}
	publishPlan(t, manager, plan)
	payload, err := os.ReadFile(filepath.Join(repository, ".reviewparty", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"checkpoints": {`) {
		t.Fatalf("published document = %s", payload)
	}

	if again := planCheckpoints(t, manager, repository, SetCheckpoint{Name: CheckpointPrePush, Checkpoint: checkpoint}); len(again.Changes()) != 0 {
		t.Fatalf("replanning the same checkpoint = %#v", again.Changes())
	}

	publishPlan(t, manager, planCheckpoints(t, manager, repository, RemoveCheckpoint{Name: CheckpointPrePush}))
	checkpoints, err := manager.Checkpoints(Repository(repository))
	if err != nil || checkpoints != nil {
		t.Fatalf("checkpoints after removal = %#v, %v", checkpoints, err)
	}
}

func TestCheckpointPlanRejectsUnknownName(t *testing.T) {
	plan, err := testManager(t, t.TempDir()).Plan(Repository(t.TempDir()), []Intent{SetCheckpoint{Name: "pre-merge", Checkpoint: NewCheckpoint()}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Reason(), `unknown checkpoint "pre-merge"`) {
		t.Fatalf("plan reason = %q", plan.Reason())
	}
}

func TestCheckpointExempts(t *testing.T) {
	checkpoint := Checkpoint{ExemptPaths: []string{"*.md", "docs/**"}}
	for path, want := range map[string]bool{"README.md": true, "docs/a.go": true, "src/a.go": false} {
		if got := checkpoint.Exempts(path); got != want {
			t.Errorf("Exempts(%q) = %t, want %t", path, got, want)
		}
	}
}
