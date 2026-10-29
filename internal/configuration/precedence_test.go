package configuration

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlansWarnWhenPrecedenceChangesWhatIsInForce(t *testing.T) {
	const (
		globalReviews     = `"reviews":{"concurrency_limit":2,"global":[{"profile":"global-pick"}],"repository":[]}`
		repositoryReviews = `"reviews":{"concurrency_limit":3,"global":[{"profile":"one"},{"profile":"two"}],"repository":[]}`
		bothCheckpoints   = `{"pre-push":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3},"pre-commit":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}`
		preCommit         = `{"pre-commit":{"requirement":"reviewed","unreviewed_lines":0,"review_budget":3}}`
	)
	emptySelection := ReviewSelection{ConcurrencyLimit: 4}
	for _, test := range []struct {
		name       string
		global     string
		repository string
		intents    []Intent
		want       []string
	}{
		{
			name: "a Repository selection takes over from the Global one", global: globalReviews, repository: ``,
			intents: []Intent{SetReviewSelection{Target: ScopeRepository, Selection: emptySelection}},
			want:    []string{"The Review selection in force here moves from Global Configuration (1 item) to Repository Configuration (0 items), which replaces the Global one as a whole; later edits to the Global selection no longer reach this repository."},
		},
		{
			name: "inheriting hands the selection back to Global", global: globalReviews, repository: repositoryReviews,
			intents: []Intent{InheritGlobalReviewSelection{}},
			want:    []string{"The Review selection in force here moves from Repository Configuration (2 items) to Global Configuration (1 item); edits to the Global selection now reach this repository."},
		},
		{
			name: "inheriting when Global declares none leaves no selection in force", global: ``, repository: repositoryReviews,
			intents: []Intent{InheritGlobalReviewSelection{}},
			want:    []string{"Neither Repository nor Global Configuration now declares a Review selection, so none is in force here."},
		},
		{
			name: "creating Repository Configuration enrolls the repository", global: `"checkpoints":` + globalPrePush, repository: "absent",
			intents: []Intent{SetReviewerEnabled{Target: ScopeRepository, Reviewer: "opencode", Enabled: false}},
			want:    []string{"Creating Repository Configuration enrolls this repository, so the Global pre-push Checkpoint now applies here."},
		},
		{
			name: "declaring no Checkpoints takes the Global ones out of force", global: `"checkpoints":` + bothCheckpoints, repository: ``,
			intents: []Intent{DeclareNoCheckpoints{}},
			want:    []string{"Repository Configuration declares no Checkpoints, so the Global pre-push and pre-commit Checkpoints no longer apply here."},
		},
		{
			name: "inheriting Checkpoints puts the Global ones in force", global: `"checkpoints":` + globalPrePush, repository: `"checkpoints":{}`,
			intents: []Intent{InheritGlobalCheckpoints{}},
			want:    []string{"Repository Configuration no longer declares Checkpoints of its own, so the Global pre-push Checkpoint now applies here."},
		},
		{
			name: "removing the last Repository Checkpoint puts the Global ones in force", global: `"checkpoints":` + globalPrePush, repository: `"checkpoints":` + preCommit,
			intents: []Intent{RemoveCheckpoint{Target: ScopeRepository, Name: CheckpointPreCommit}},
			want:    []string{"Repository Configuration no longer declares Checkpoints of its own, so the Global pre-push Checkpoint now applies here."},
		},
		{
			name: "a Repository Checkpoint replaces the Global ones", global: `"checkpoints":` + globalPrePush, repository: ``,
			intents: []Intent{SetCheckpoint{Target: ScopeRepository, Name: CheckpointPreCommit, Checkpoint: NewCheckpoint()}},
			want:    []string{"Repository Configuration now declares its own Checkpoints, which replace the Global pre-push Checkpoint here."},
		},
		{
			name: "a Global selection edit this repository replaces", global: globalReviews, repository: repositoryReviews,
			intents: []Intent{SetReviewSelection{Target: ScopeGlobal, Selection: emptySelection}},
			want:    []string{"Repository Configuration replaces the Global Review selection here, so this change to it applies only to repositories without their own."},
		},
		{
			name: "a Global Checkpoint edit this repository replaces", global: `"checkpoints":` + globalPrePush, repository: `"checkpoints":{}`,
			intents: []Intent{SetCheckpoint{Target: ScopeGlobal, Name: CheckpointPreCommit, Checkpoint: NewCheckpoint()}},
			want:    []string{"Repository Configuration replaces the Global Checkpoints here, so this change to them applies only to enrolled repositories without their own."},
		},
		{
			name: "a first Repository selection with nothing to replace", global: ``, repository: "absent",
			intents: []Intent{SetReviewSelection{Target: ScopeRepository, Selection: emptySelection}},
		},
		{
			name: "a Repository edit that moves nothing between files", global: globalReviews + `,"checkpoints":` + globalPrePush, repository: repositoryReviews,
			intents: []Intent{SetReviewSelection{Target: ScopeRepository, Selection: emptySelection}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1`+prefixComma(test.global)+`}`)
			repository := t.TempDir()
			if test.repository != "absent" {
				writeDocument(t, repositoryConfig(repository), `{"schema_version":1`+prefixComma(test.repository)+`}`)
			}
			plan := planCheckpoints(t, testManager(t, root), repository, test.intents...)
			if got := plan.Warnings(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("warnings = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPlansWithoutARepositoryCarryNoPrecedenceWarnings(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, filepath.Join(root, "config.json"), `{"schema_version":1,"checkpoints":`+globalPrePush+`}`)
	plan := planCheckpoints(t, testManager(t, root), "", SetCheckpoint{Target: ScopeGlobal, Name: CheckpointPreCommit, Checkpoint: NewCheckpoint()})
	if warnings := plan.Warnings(); len(warnings) != 0 {
		t.Fatalf("warnings = %q", warnings)
	}
}

func prefixComma(fields string) string {
	if fields == "" {
		return ""
	}
	return "," + fields
}
