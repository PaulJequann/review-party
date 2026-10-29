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
	writeRepositoryCheckpoints(t, repository, `{"pre-push":{"requirement":"judged","exempt_paths":["*.md"],"unreviewed_lines":40,"review_budget":2,"integrations":["git","claude-code","codex","agents-md"]}}`)
	manager := testManager(t, t.TempDir())

	checkpoints, err := manager.Checkpoints(Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	want := map[CheckpointName]Checkpoint{CheckpointPrePush: {
		Requirement: RequirementJudged, ExemptPaths: []string{"*.md"}, UnreviewedLines: 40, ReviewBudget: 2, Waivers: WaiversHuman, Integrations: []IntegrationName{IntegrationGit, IntegrationClaudeCode, IntegrationCodex, IntegrationAgentsMD},
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
		{"unknown name", `{"pre-merge":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}`, `unknown checkpoint "pre-merge"`},
		{"missing requirement", `{"pre-push":{"unreviewed_lines":0,"review_budget":3}}`, `checkpoints.pre-push.requirement: unknown requirement ""`},
		{"unknown requirement", `{"pre-push":{"requirement":"approved","unreviewed_lines":0,"review_budget":3}}`, `unknown requirement "approved"; expected reviewed or judged`},
		{"unknown policy", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3,"waivers":"robots"}}`, `checkpoints.pre-push.waivers: unknown waiver policy "robots"`},
		{"unknown integration", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3,"integrations":["cursor"]}}`, `unknown integration "cursor"; expected git, claude-code, codex, or agents-md`},
		{"duplicate integration", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3,"integrations":["git","git"]}}`, `integration "git" is listed twice`},
		{"missing allowance", `{"pre-push":{"requirement":"reviewed","review_budget":3}}`, `unreviewed_lines is required`},
		{"missing budget", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0}}`, `review_budget is required`},
		{"negative allowance", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":-1,"review_budget":3}}`, `unreviewed_lines: must not be negative`},
		{"zero budget", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":0}}`, `review_budget: must be between 1 and 9`},
		{"budget over nine", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":10}}`, `review_budget: must be between 1 and 9`},
		{"malformed pattern", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3,"exempt_paths":["/docs/**"]}}`, `exempt_paths: pattern "/docs/**" must be relative`},
		{"unknown field", `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3,"bypass":true}}`, `unknown field "bypass"`},
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

func TestGlobalCheckpointsAreInForceForEnrolledRepositories(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "config.json")
	writeDocument(t, global, `{"schema_version":1,"checkpoints":{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}}`)
	manager := testManager(t, root)
	declared := map[CheckpointName]Checkpoint{CheckpointPrePush: {Requirement: RequirementReviewed, ReviewBudget: 3, Waivers: WaiversHuman}}

	unenrolled := t.TempDir()
	enrolled := t.TempDir()
	writeDocument(t, filepath.Join(enrolled, ".reviewparty", "config.json"), `{"schema_version":1}`)
	replacing := t.TempDir()
	writeRepositoryCheckpoints(t, replacing, `{}`)
	for _, test := range []struct {
		name       string
		repository string
		want       Value[map[CheckpointName]Checkpoint]
	}{
		{"unenrolled repository has none", unenrolled, Value[map[CheckpointName]Checkpoint]{Source: SourcePackaged}},
		{"enrolled repository takes the Global ones", enrolled, Value[map[CheckpointName]Checkpoint]{Value: declared, Authored: true, Source: SourceGlobal, Path: global}},
		{"repository checkpoints replace them as a whole", replacing, Value[map[CheckpointName]Checkpoint]{Value: map[CheckpointName]Checkpoint{}, Authored: true, Source: SourceRepository, Path: filepath.Join(replacing, ".reviewparty", "config.json")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			effective, err := manager.EffectiveCheckpoints(Repository(test.repository))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(effective, test.want) {
				t.Fatalf("effective = %#v, want %#v", effective, test.want)
			}
		})
	}
}

func TestRepositoryOptOutSurvivesUnrelatedMutations(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"checkpoints":{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}}`)
	manager := testManager(t, root)
	repository := t.TempDir()
	writeRepositoryCheckpoints(t, repository, `{}`)

	publishPlan(t, manager, planCheckpoints(t, manager, repository, SetReviewerEnabled{Target: ScopeRepository, Reviewer: "opencode", Enabled: false}))
	payload, err := os.ReadFile(filepath.Join(repository, ".reviewparty", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"checkpoints": {}`) {
		t.Fatalf("rewritten document dropped the opt-out: %s", payload)
	}
	effective, err := manager.EffectiveCheckpoints(Repository(repository))
	if err != nil || effective.Source != SourceRepository || len(effective.Value) != 0 {
		t.Fatalf("effective after an unrelated mutation = %#v, %v", effective, err)
	}
}

func TestGlobalCheckpointPlansEditGlobalConfiguration(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	checkpoint := NewCheckpoint()
	checkpoint.Integrations = []IntegrationName{IntegrationGit}
	enrolled := t.TempDir()
	writeDocument(t, filepath.Join(enrolled, ".reviewparty", "config.json"), `{"schema_version":1}`)

	plan := planCheckpoints(t, manager, "", SetCheckpoint{Target: ScopeGlobal, Name: CheckpointPrePush, Checkpoint: checkpoint})
	if changes := plan.Changes(); len(changes) != 1 || changes[0].Scope != ScopeGlobal || changes[0].Path != filepath.Join(root, "config.json") {
		t.Fatalf("changes = %#v", changes)
	}
	publishPlan(t, manager, plan)
	declared, err := manager.Checkpoints(Repository(enrolled))
	if err != nil || !reflect.DeepEqual(declared, map[CheckpointName]Checkpoint{CheckpointPrePush: checkpoint}) {
		t.Fatalf("declared = %#v, %v", declared, err)
	}

	publishPlan(t, manager, planCheckpoints(t, manager, "", RemoveCheckpoint{Target: ScopeGlobal, Name: CheckpointPrePush}))
	if declared, err = manager.Checkpoints(Repository(enrolled)); err != nil || len(declared) != 0 {
		t.Fatalf("declared after remove = %#v, %v", declared, err)
	}
}

func TestEnrollmentPlanWritesOnlyTheSchemaVersion(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	repository := t.TempDir()
	path := filepath.Join(repository, ".reviewparty", "config.json")

	if enrollment, err := manager.Enrollment(Repository(repository)); err != nil || enrollment.Enrolled || enrollment.Offered() {
		t.Fatalf("enrollment without Global Checkpoints = %#v, %v", enrollment, err)
	}
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"checkpoints":{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}}`)
	if enrollment, err := manager.Enrollment(Repository(repository)); err != nil || enrollment.Enrolled || !enrollment.Offered() {
		t.Fatalf("enrollment with Global Checkpoints = %#v, %v", enrollment, err)
	}

	plan, err := manager.PlanEnrollment(Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{{Field: "schema_version", Scope: ScopeRepository, Path: path, After: "1", HadAfter: true}}
	if !plan.Valid() || !reflect.DeepEqual(plan.Changes(), want) {
		t.Fatalf("plan = %s %#v", plan.Reason(), plan.Changes())
	}
	publishPlan(t, manager, plan)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "{\n  \"schema_version\": 1\n}\n" {
		t.Fatalf("content = %q, %v", content, err)
	}
	if enrollment, err := manager.Enrollment(Repository(repository)); err != nil || !enrollment.Enrolled || enrollment.Offered() {
		t.Fatalf("enrollment after enrolling = %#v, %v", enrollment, err)
	}
	if again, err := manager.PlanEnrollment(Repository(repository)); err != nil || !again.Unchanged() {
		t.Fatalf("second enrollment plan = %#v, %v", again.Changes(), err)
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

	plan := planCheckpoints(t, manager, repository, SetCheckpoint{Target: ScopeRepository, Name: CheckpointPrePush, Checkpoint: checkpoint})
	change := plan.Changes()[0]
	got := [3]string{change.Field, strconv.FormatBool(change.HadBefore), change.After}
	if want := [3]string{"checkpoints.pre-push", "false", `{"requirement":"reviewed","exempt_paths":["docs/**"],"unreviewed_lines":0,"review_budget":3,"waivers":"human","integrations":["git"]}`}; got != want {
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

	if again := planCheckpoints(t, manager, repository, SetCheckpoint{Target: ScopeRepository, Name: CheckpointPrePush, Checkpoint: checkpoint}); len(again.Changes()) != 0 {
		t.Fatalf("replanning the same checkpoint = %#v", again.Changes())
	}

	publishPlan(t, manager, planCheckpoints(t, manager, repository, RemoveCheckpoint{Target: ScopeRepository, Name: CheckpointPrePush}))
	checkpoints, err := manager.Checkpoints(Repository(repository))
	if err != nil || checkpoints != nil {
		t.Fatalf("checkpoints after removal = %#v, %v", checkpoints, err)
	}
}

func TestCheckpointPlanRejectsUnknownName(t *testing.T) {
	plan, err := testManager(t, t.TempDir()).Plan(Repository(t.TempDir()), []Intent{SetCheckpoint{Target: ScopeRepository, Name: "pre-merge", Checkpoint: NewCheckpoint()}})
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
