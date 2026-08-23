package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testManager(t *testing.T, personalRoot string) *Manager {
	t.Helper()
	return NewManager(Options{
		PersonalRoot:            personalRoot,
		Reviewers:               []string{"grok", "opencode", "copilot", "codex"},
		PackagedDefaultReviewer: "grok",
		PackagedDefaultProfile:  "bugs",
		ValidateProfileName: func(name string) error {
			if strings.ContainsAny(name, "./") {
				return errors.New("must match [a-z0-9][a-z0-9-]*")
			}
			return nil
		},
	})
}

func writeDocument(t *testing.T, path, payload string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePreservesScopeProvenance(t *testing.T) {
	root := t.TempDir()
	repository := t.TempDir()
	writeDocument(t, filepath.Join(root, "config.json"), `{
  "schema_version": 1,
  "defaults": {"reviewer": "opencode"},
  "reviewers": {"opencode": {"model": "meta/muse-spark-1.2-contributor"}}
}`)
	writeDocument(t, filepath.Join(repository, ".reviewparty", "config.json"), `{
  "schema_version": 1,
  "defaults": {"profile": "security"},
  "reviewers": {"opencode": {"enabled": false}}
}`)
	manager := testManager(t, root)

	effective, err := manager.Resolve(Request{Repository: Repository(repository)})
	if err != nil {
		t.Fatal(err)
	}
	personalPath := filepath.Join(root, "config.json")
	repositoryPath := filepath.Join(repository, ".reviewparty", "config.json")

	assertStringValue(t, "default reviewer", effective.DefaultReviewer, "opencode", true, SourcePersonal, personalPath)
	assertStringValue(t, "default profile", effective.DefaultProfile, "security", true, SourceRepository, repositoryPath)
	opencode := effective.Reviewers["opencode"]
	assertBoolValue(t, "opencode enabled", opencode.Enabled, false, true, SourceRepository, repositoryPath)
	assertStringValue(t, "opencode model", opencode.Model, "meta/muse-spark-1.2-contributor", true, SourcePersonal, personalPath)
	grok := effective.Reviewers["grok"]
	assertBoolValue(t, "grok enabled", grok.Enabled, true, false, SourcePackaged, "")
}

func TestResolveReportsExplicitOverrideProvenance(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)

	effective, err := manager.Resolve(Request{Overrides: Overrides{Reviewer: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	assertStringValue(t, "explicit reviewer", effective.DefaultReviewer, "codex", true, SourceExplicit, "")
	assertStringValue(t, "packaged profile", effective.DefaultProfile, "bugs", false, SourcePackaged, "")
}

func TestResolvingDefaultsCreatesNoFile(t *testing.T) {
	root := t.TempDir()
	repository := t.TempDir()
	manager := testManager(t, root)

	loaded, err := manager.Load(Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	assertLoadedDocumentAbsent(t, "personal", loaded.Personal)
	assertLoadedDocumentAbsent(t, "repository", loaded.Repository)
	effective, err := manager.Resolve(Request{})
	if err != nil {
		t.Fatal(err)
	}
	assertStringValue(t, "packaged reviewer", effective.DefaultReviewer, "grok", false, SourcePackaged, "")
	if _, err := os.Stat(filepath.Join(root, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("personal configuration was created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repository, ".reviewparty", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("repository configuration was created: %v", err)
	}
}

func TestRepositoryScopeRejectsPersonalOnlyFields(t *testing.T) {
	repository := t.TempDir()
	writeDocument(t, filepath.Join(repository, ".reviewparty", "config.json"),
		`{"schema_version":1,"state_directory":"/somewhere/private"}`)
	manager := testManager(t, t.TempDir())

	_, err := manager.Load(Repository(repository))
	var invalid InvalidDocumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidDocumentError", err)
	}
	if invalid.Scope != ScopeRepository || !strings.Contains(invalid.Reason, "state_directory is a Personal Configuration field") {
		t.Fatalf("error = %#v", invalid)
	}
}

func TestPublishIsAtomicWhenASecondFileFails(t *testing.T) {
	root := t.TempDir()
	repository := t.TempDir()
	personalPath := filepath.Join(root, "config.json")
	writeDocument(t, personalPath, `{"schema_version":1,"defaults":{"reviewer":"opencode"}}`)
	manager := testManager(t, root)
	writes := 0
	manager.publishWrite = func(write *pendingWrite) error {
		writes++
		if writes == 2 {
			return errors.New("forced second publication failure")
		}
		return writeAtomically(write)
	}

	plan, err := manager.Plan(Repository(repository), []Intent{
		SetStateDirectory{Directory: root},
		SetDefaultReviewer{Target: ScopeRepository, Reviewer: "codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan = %#v", plan)
	}

	err = manager.Publish(plan)
	if err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	payload, readErr := os.ReadFile(personalPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	const original = `{"schema_version":1,"defaults":{"reviewer":"opencode"}}`
	if string(payload) != original {
		t.Fatalf("personal configuration changed during failed publication:\n%s", payload)
	}
}

func TestPlanPreviewsWithoutPublishingUntilPublished(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	plan, err := manager.Plan(Repository(""), []Intent{
		SetStateDirectory{Directory: root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan = %#v", plan)
	}
	path := filepath.Join(root, "config.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("planning created configuration: %v", err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("confirmed configuration was not published: %v", err)
	}
}

func TestPublishedTypedReviewerIntentsUpdateEffectiveValues(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	requireConfirmedPlan(t, manager, []Intent{
		SetDefaultProfile{Target: ScopePersonal, Profile: "security"},
		SetReviewerEnabled{Target: ScopePersonal, Reviewer: "opencode", Enabled: false},
		SetReviewerAllowedModels{Target: ScopePersonal, Reviewer: "opencode", Models: []string{"model-a", "model-b"}},
	})
	effective := requireEffective(t, manager)
	assertStringValue(t, "default profile", effective.DefaultProfile, "security", true, SourcePersonal, filepath.Join(root, "config.json"))
	opencode := effective.Reviewers["opencode"]
	assertBoolValue(t, "opencode enabled", opencode.Enabled, false, true, SourcePersonal, filepath.Join(root, "config.json"))
	if !reflect.DeepEqual(opencode.AllowedModels.Value, []string{"model-a", "model-b"}) || !opencode.AllowedModels.Authored {
		t.Fatalf("opencode allowed models = %#v", opencode.AllowedModels)
	}
}

func TestRenderedDocumentIsStableAndOmitsDefaults(t *testing.T) {
	document := Document{SchemaVersion: SchemaVersion}
	payload, err := renderDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"schema_version\": 1\n}\n"
	if string(payload) != want {
		t.Fatalf("payload = %q, want %q", payload, want)
	}
	full := Document{
		SchemaVersion:  SchemaVersion,
		StateDirectory: "/state",
		Defaults:       Defaults{Reviewer: "grok", Profile: "bugs"},
		Reviewers: map[string]ReviewerPolicy{
			"opencode": {Enabled: boolPointer(false), Model: "m"},
		},
		Eval: &EvalPolicy{ConcurrencyLimit: 2},
	}
	payload, err = renderDocument(full)
	if err != nil {
		t.Fatal(err)
	}
	want = `{
  "schema_version": 1,
  "state_directory": "/state",
  "defaults": {
    "reviewer": "grok",
    "profile": "bugs"
  },
  "reviewers": {
    "opencode": {
      "enabled": false,
      "model": "m"
    }
  },
  "eval": {
    "concurrency_limit": 2
  }
}
`
	if string(payload) != want {
		t.Fatalf("payload =\n%s\nwant\n%s", payload, want)
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func TestPlanRejectsUnknownReviewerAndPublishRefusesInvalidPlan(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)

	plan, err := manager.Plan(Repository(""), []Intent{
		SetDefaultReviewer{Target: ScopePersonal, Reviewer: "unknown"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() || !strings.Contains(plan.Reason(), `unknown reviewer "unknown"`) {
		t.Fatalf("plan = %#v", plan)
	}
	if err := manager.Publish(plan); err == nil || !strings.Contains(err.Error(), "invalid change plan") {
		t.Fatalf("publish error = %v", err)
	}
}

func TestPlanRejectsNilIntentAndPublishRefusesInvalidPlan(t *testing.T) {
	manager := testManager(t, t.TempDir())
	plan, err := manager.Plan(Repository(""), []Intent{nil})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() || !strings.Contains(plan.Reason(), "configuration intent must not be nil") {
		t.Fatalf("plan = %#v", plan)
	}
	if err := manager.Publish(plan); err == nil || !strings.Contains(err.Error(), "invalid change plan") {
		t.Fatalf("publish error = %v", err)
	}
}

func TestPublishedPersonalConfigurationIsPrivateAndReadable(t *testing.T) {
	root := t.TempDir()
	manager := testManager(t, root)
	plan := requirePersonalPlan(t, manager, root)
	assertPersonalPlanShape(t, plan)
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.json")
	assertPrivateFile(t, path)
	effective := requireEffective(t, manager)
	assertStringValue(t, "state directory", effective.StateDirectory, root, true, SourcePersonal, path)
	assertStringValue(t, "model", effective.Reviewers["opencode"].Model, "meta/muse-spark-1.2-contributor", true, SourcePersonal, path)
}

func requirePersonalPlan(t *testing.T, manager *Manager, root string) Plan {
	t.Helper()
	plan, err := manager.Plan(Repository(""), []Intent{
		SetStateDirectory{Directory: root},
		SetReviewerModel{Target: ScopePersonal, Reviewer: "opencode", Model: "meta/muse-spark-1.2-contributor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan = %#v", plan)
	}
	return plan
}

func requireConfirmedPlan(t *testing.T, manager *Manager, intents []Intent) {
	t.Helper()
	plan, err := manager.Plan(Repository(""), intents)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("plan = %#v", plan)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func assertPersonalPlanShape(t *testing.T, plan Plan) {
	t.Helper()
	if len(plan.Changes()) != 2 {
		t.Fatalf("plan changes = %#v", plan.Changes())
	}
	if len(plan.Paths()) != 1 {
		t.Fatalf("plan paths = %#v", plan.Paths())
	}
	if plan.Scopes()[0] != ScopePersonal {
		t.Fatalf("plan scopes = %#v", plan.Scopes())
	}
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %v, want private", info.Mode().Perm())
	}
}

func requireEffective(t *testing.T, manager *Manager) Effective {
	t.Helper()
	effective, err := manager.Resolve(Request{})
	if err != nil {
		t.Fatal(err)
	}
	return effective
}

func assertStringValue(t *testing.T, label string, got Value[string], value string, authored bool, source Source, path string) {
	assertValue(t, label, got, value, authored, source, path)
}

func assertBoolValue(t *testing.T, label string, got Value[bool], value, authored bool, source Source, path string) {
	assertValue(t, label, got, value, authored, source, path)
}

func assertValue[T comparable](t *testing.T, label string, got Value[T], value T, authored bool, source Source, path string) {
	t.Helper()
	if got.Value != value {
		t.Fatalf("%s value = %v, want %v", label, got.Value, value)
	}
	if got.Authored != authored {
		t.Fatalf("%s authored = %t, want %t", label, got.Authored, authored)
	}
	if got.Source != source {
		t.Fatalf("%s source = %q, want %q", label, got.Source, source)
	}
	if got.Path != path {
		t.Fatalf("%s path = %q, want %q", label, got.Path, path)
	}
}

func assertLoadedDocumentAbsent(t *testing.T, label string, document LoadedDocument) {
	t.Helper()
	if document.Present {
		t.Fatalf("%s document was present: %#v", label, document)
	}
	if document.Path != "" {
		t.Fatalf("%s document path = %q", label, document.Path)
	}
}
