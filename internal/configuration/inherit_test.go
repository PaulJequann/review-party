package configuration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const globalPrePush = `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}`

func repositoryConfig(repository string) string {
	return filepath.Join(repository, ".reviewparty", "config.json")
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestLoadedCheckpointsFollowEnrollmentAndWholeReplacement(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "config.json")
	writeDocument(t, global, `{"schema_version":1,"checkpoints":`+globalPrePush+`}`)
	manager := testManager(t, root)
	prePush := map[CheckpointName]Checkpoint{CheckpointPrePush: {Requirement: RequirementReviewed, ReviewBudget: 3, Waivers: WaiversHuman}}
	preCommit := map[CheckpointName]Checkpoint{CheckpointPreCommit: {Requirement: RequirementJudged, UnreviewedLines: 5, ReviewBudget: 2, Waivers: WaiversHuman}}

	unenrolled := t.TempDir()
	enrolled := t.TempDir()
	writeDocument(t, repositoryConfig(enrolled), `{"schema_version":1}`)
	optedOut := t.TempDir()
	writeRepositoryCheckpoints(t, optedOut, `{}`)
	declaring := t.TempDir()
	writeRepositoryCheckpoints(t, declaring, `{"pre-commit":{"requirement":"judged","unreviewed_lines":5,"review_budget":2}}`)
	for _, test := range []struct {
		name       string
		repository string
		want       Value[map[CheckpointName]Checkpoint]
	}{
		{"no Repository Configuration has none", unenrolled, Value[map[CheckpointName]Checkpoint]{Source: SourcePackaged}},
		{"Repository Configuration without the field takes the Global ones", enrolled, Value[map[CheckpointName]Checkpoint]{Value: prePush, Authored: true, Source: SourceGlobal, Path: global}},
		{"an empty object replaces them with none", optedOut, Value[map[CheckpointName]Checkpoint]{Value: map[CheckpointName]Checkpoint{}, Authored: true, Source: SourceRepository, Path: repositoryConfig(optedOut)}},
		{"declared Checkpoints replace them as a whole", declaring, Value[map[CheckpointName]Checkpoint]{Value: preCommit, Authored: true, Source: SourceRepository, Path: repositoryConfig(declaring)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			loaded, err := manager.Load(Repository(test.repository))
			if err != nil {
				t.Fatal(err)
			}
			if got := loaded.Checkpoints(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Checkpoints() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestReviewSelectionToEditStartsFromTheTargetFileOrWhatIsInForce(t *testing.T) {
	globalSelection := ReviewSelection{ConcurrencyLimit: 2, Global: []SelectionItem{{Profile: "global-pick"}}, Repository: []SelectionItem{}}
	repositorySelection := ReviewSelection{ConcurrencyLimit: 3, Global: []SelectionItem{{Party: "repository-pick"}}, Repository: []SelectionItem{}}
	withGlobal := t.TempDir()
	writeDocument(t, filepath.Join(withGlobal, "config.json"), `{"schema_version":1,"reviews":{"concurrency_limit":2,"global":[{"profile":"global-pick"}],"repository":[]}}`)
	withoutGlobal := t.TempDir()

	authoring := t.TempDir()
	writeDocument(t, repositoryConfig(authoring), `{"schema_version":1,"reviews":{"concurrency_limit":3,"global":[{"party":"repository-pick"}],"repository":[]}}`)
	inheriting := t.TempDir()
	writeDocument(t, repositoryConfig(inheriting), `{"schema_version":1}`)

	for _, test := range []struct {
		name       string
		root       string
		target     Scope
		repository string
		want       ReviewSelection
	}{
		{"Global edits Global's own selection", withGlobal, ScopeGlobal, "", globalSelection},
		{"Global edits Global's own selection even where a repository authors one", withGlobal, ScopeGlobal, authoring, globalSelection},
		{"Global without a selection starts from the default", withoutGlobal, ScopeGlobal, authoring, DefaultReviewSelection()},
		{"Repository edits its authored selection", withGlobal, ScopeRepository, authoring, repositorySelection},
		{"Repository without one starts from the Global selection in force", withGlobal, ScopeRepository, inheriting, globalSelection},
		{"Repository with nothing in force starts from the default", withoutGlobal, ScopeRepository, inheriting, DefaultReviewSelection()},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := testManager(t, test.root)
			got, err := manager.ReviewSelectionToEdit(test.target, Repository(test.repository))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ReviewSelectionToEdit = %#v, want %#v", got, test.want)
			}
			got.ConcurrencyLimit = 9
			got.Global = append(got.Global[:0], SelectionItem{Profile: "mutated"})
			again, err := manager.ReviewSelectionToEdit(test.target, Repository(test.repository))
			if err != nil || !reflect.DeepEqual(again, test.want) {
				t.Fatalf("mutating one result changed the next = %#v, %v", again, err)
			}
		})
	}
}

func TestReviewSelectionToEditNeedsARepositoryForRepositoryEdits(t *testing.T) {
	if _, err := testManager(t, t.TempDir()).ReviewSelectionToEdit(ScopeRepository, ""); err == nil {
		t.Fatal("ReviewSelectionToEdit accepted a Repository edit without a repository")
	}
}

func TestInheritGlobalReviewSelectionRemovesOnlyTheRepositorySelection(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"reviews":{"concurrency_limit":2,"global":[{"profile":"global-pick"}],"repository":[]}}`)
	manager := testManager(t, root)
	repository := t.TempDir()
	writeDocument(t, repositoryConfig(repository), `{"schema_version":1,"reviewers":{"opencode":{"enabled":false}},"reviews":{"concurrency_limit":3,"global":[],"repository":[]}}`)

	publishPlan(t, manager, planCheckpoints(t, manager, repository, InheritGlobalReviewSelection{}))
	content := readConfig(t, repositoryConfig(repository))
	if strings.Contains(content, `"reviews"`) || !strings.Contains(content, `"enabled": false`) {
		t.Fatalf("Repository Configuration after inheriting = %s", content)
	}
	loaded, err := manager.Load(Repository(repository))
	if err != nil || loaded.ReviewSelection().Source != SourceGlobal {
		t.Fatalf("selection in force = %#v, %v", loaded.ReviewSelection(), err)
	}

	if _, err := manager.Plan(Repository(repository), []Intent{InheritGlobalReviewSelection{}}); err == nil {
		t.Fatal("inheriting a selection the repository already inherits planned silently")
	}
	unenrolled := t.TempDir()
	if _, err := manager.Plan(Repository(unenrolled), []Intent{InheritGlobalReviewSelection{}}); err == nil {
		t.Fatal("inheriting without Repository Configuration planned silently")
	}
	if _, err := os.Stat(repositoryConfig(unenrolled)); !os.IsNotExist(err) {
		t.Fatalf("a refused plan created Repository Configuration: %v", err)
	}
}

func TestInheritGlobalCheckpointsRemovesTheRepositoryField(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"checkpoints":`+globalPrePush+`}`)
	manager := testManager(t, root)
	for _, authored := range []string{`{}`, `{"pre-commit":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}`} {
		repository := t.TempDir()
		writeRepositoryCheckpoints(t, repository, authored)

		publishPlan(t, manager, planCheckpoints(t, manager, repository, InheritGlobalCheckpoints{}))
		if content := readConfig(t, repositoryConfig(repository)); strings.Contains(content, `"checkpoints"`) {
			t.Fatalf("Repository Configuration after inheriting %s = %s", authored, content)
		}
		loaded, err := manager.Load(Repository(repository))
		if err != nil || loaded.Checkpoints().Source != SourceGlobal {
			t.Fatalf("Checkpoints in force after inheriting %s = %#v, %v", authored, loaded.Checkpoints(), err)
		}
		if _, err := manager.Plan(Repository(repository), []Intent{InheritGlobalCheckpoints{}}); err == nil {
			t.Fatal("inheriting Checkpoints the repository already inherits planned silently")
		}
	}
}

func TestDeclareNoCheckpointsWritesAnEmptyObjectThatSurvivesReload(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"checkpoints":`+globalPrePush+`}`)
	manager := testManager(t, root)
	enrolled := t.TempDir()
	writeDocument(t, repositoryConfig(enrolled), `{"schema_version":1}`)
	declaring := t.TempDir()
	writeRepositoryCheckpoints(t, declaring, `{"pre-commit":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}`)
	unenrolled := t.TempDir()

	for _, repository := range []string{enrolled, declaring, unenrolled} {
		publishPlan(t, manager, planCheckpoints(t, manager, repository, DeclareNoCheckpoints{}))
		if content := readConfig(t, repositoryConfig(repository)); !strings.Contains(content, `"checkpoints": {}`) {
			t.Fatalf("Repository Configuration after declaring none = %s", content)
		}
		loaded, err := manager.Load(Repository(repository))
		want := Value[map[CheckpointName]Checkpoint]{Value: map[CheckpointName]Checkpoint{}, Authored: true, Source: SourceRepository, Path: repositoryConfig(repository)}
		if err != nil || !reflect.DeepEqual(loaded.Checkpoints(), want) {
			t.Fatalf("Checkpoints in force after reload = %#v, %v", loaded.Checkpoints(), err)
		}
		if _, err := manager.Plan(Repository(repository), []Intent{DeclareNoCheckpoints{}}); err == nil {
			t.Fatal("declaring no Checkpoints twice planned silently")
		}
	}
}
