package configuration

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var baselineExecution = ProfileExecution{Reviewer: "codex", Model: "gpt-5.6-luna", ReasoningEffort: "high", AttemptDeadline: "8m"}

func baselineManager(t *testing.T, root string) *Manager {
	t.Helper()
	return NewManager(Options{
		GlobalRoot: root,
		Reviewers:  []string{"codex"},
		Templates: []Template{
			{ID: "style", Revision: "style-v1", Instructions: "STYLE\n", Baseline: true},
			{ID: "bugs", Revision: "bugs-v2", Instructions: "BUGS\n", Baseline: true},
			{ID: "security", Revision: "security-v1", Instructions: "SECURITY\n"},
			{ID: "docs", Revision: "docs-v1", Instructions: "DOCS\n", Baseline: true},
		},
	})
}

func requireBaseline(t *testing.T, manager *Manager) Baseline {
	t.Helper()
	baseline, err := manager.Baseline("")
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}

func memberStates(baseline Baseline) map[string]BaselineState {
	states := map[string]BaselineState{}
	for _, member := range baseline.Members {
		states[member.Template.ID] = member.State
	}
	return states
}

func publishBaselineProfiles(t *testing.T, manager *Manager) Plan {
	t.Helper()
	plan, err := manager.PlanBaselineProfiles("", baselineExecution)
	requirePublishedPlan(t, manager, plan, err)
	return plan
}

func requireUnchangedPlan(t *testing.T, label string, plan Plan, err error) {
	t.Helper()
	if changes := requireValidPlan(t, label, plan, err).Changes(); len(changes) != 0 {
		t.Fatalf("%s changes = %v", label, changes)
	}
}

func requireBlockedBy(t *testing.T, baseline Baseline, path string) {
	t.Helper()
	if blocked := baseline.Blocked(); blocked == nil || !strings.Contains(blocked.Error(), path) {
		t.Fatalf("blocked = %v, want %s", blocked, path)
	}
}

func TestBaselineReadsEachMemberStateInTemplateOrder(t *testing.T) {
	root := t.TempDir()
	manager := baselineManager(t, root)
	requirePublishedPlan(t, manager, requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "bugs", Reviewer: "codex", Model: "luna", ReasoningEffort: "high", AttemptDeadline: "1m", TemplateID: "bugs",
	}), nil)
	requirePublishedPlan(t, manager, requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "docs", Reviewer: "codex", Model: "luna", ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "MINE\n",
	}), nil)
	writeDocument(t, filepath.Join(root, "profiles", "style", "instructions.md"), "PARTIAL\n")

	baseline := requireBaseline(t, manager)

	if names := BaselineNames(baseline.Members); !slices.Equal(names, []string{"bugs", "docs", "style"}) {
		t.Fatalf("members = %v", names)
	}
	want := map[string]BaselineState{"bugs": BaselineReady, "docs": BaselineForeign, "style": BaselineBroken}
	if got := memberStates(baseline); !maps.Equal(got, want) {
		t.Fatalf("states = %v", got)
	}
	if baseline.Party.State != BaselineMissing {
		t.Fatalf("Party state = %s", baseline.Party.State)
	}
	requireBlockedBy(t, baseline, filepath.Join(root, "profiles", "style"))
}

func TestPlanBaselineProfilesCreatesOnlyMissingMembers(t *testing.T) {
	root := t.TempDir()
	manager := baselineManager(t, root)
	requirePublishedPlan(t, manager, requireProfilePlan(t, manager, ProfileDraft{
		Target: ScopeGlobal, Name: "docs", Reviewer: "codex", Model: "mine", ReasoningEffort: "low", AttemptDeadline: "1m", Instructions: "MINE\n",
	}), nil)
	kept := requireProfile(t, manager, ScopeGlobal, "docs")

	publishBaselineProfiles(t, manager)

	for _, name := range []string{"bugs", "style"} {
		profile := requireProfile(t, manager, ScopeGlobal, name)
		execution := ProfileExecution{Reviewer: profile.Reviewer, Model: profile.Model, ReasoningEffort: profile.ReasoningEffort, AttemptDeadline: profile.AttemptDeadline}
		if profile.TemplateID != name || execution != baselineExecution {
			t.Fatalf("Profile %s = %#v", name, profile)
		}
	}
	if docs := requireProfile(t, manager, ScopeGlobal, "docs"); !reflect.DeepEqual(docs, kept) {
		t.Fatalf("existing Profile changed: %#v", docs)
	}
	rerun, err := manager.PlanBaselineProfiles("", ProfileExecution{})
	requireUnchangedPlan(t, "rerun", rerun, err)
}

func TestBaselineProfilesPublishNoMemberWhenOneFails(t *testing.T) {
	root := t.TempDir()
	manager := baselineManager(t, root)
	plan, err := manager.PlanBaselineProfiles("", baselineExecution)
	requireValidPlan(t, "baseline Profiles", plan, err)
	writes := 0
	manager.publication.writeProfile = func(publication *pendingProfilePublication) error {
		writes++
		if writes == 2 {
			return errors.New("forced second Profile failure")
		}
		return writeProfileAtomically(publication)
	}

	if err := manager.Publish(plan); err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}

	for _, name := range []string{"bugs", "docs", "style"} {
		if _, err := os.Lstat(filepath.Join(root, "profiles", name)); !os.IsNotExist(err) {
			t.Fatalf("Profile %s survived rollback: %v", name, err)
		}
	}
}

func TestPlanBaselineProfilesRefusesIncompleteExecutionAndBlocks(t *testing.T) {
	root := t.TempDir()
	manager := baselineManager(t, root)
	incomplete, err := manager.PlanBaselineProfiles("", ProfileExecution{Reviewer: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	requireInvalidPlan(t, incomplete, "need a Reviewer, Model, Reasoning Effort, and Attempt Deadline")
	path := filepath.Join(root, "parties", BaselinePartyName+".json")
	writeDocument(t, path, "{")
	blocked, err := manager.PlanBaselineProfiles("", baselineExecution)
	if err != nil {
		t.Fatal(err)
	}
	requireInvalidPlan(t, blocked, path)
}

func TestBaselinePartyIsReadyOnlyWithTheBaselineMembers(t *testing.T) {
	root := t.TempDir()
	manager := baselineManager(t, root)
	publishBaselineProfiles(t, manager)
	plan, err := manager.PlanBaselineParty("")
	requirePublishedPlan(t, manager, plan, err)
	if state := requireBaseline(t, manager).Party.State; state != BaselineReady {
		t.Fatalf("Party state = %s", state)
	}
	party, _, err := manager.LoadParty(ScopeGlobal, "", BaselinePartyName)
	if err != nil || party.ConcurrencyLimit != 3 {
		t.Fatalf("Party = %#v, error %v", party, err)
	}
	rerun, err := manager.PlanBaselineParty("")
	requireUnchangedPlan(t, "rerun", rerun, err)

	path := filepath.Join(root, "parties", "baseline.json")
	writeDocument(t, path, `{"schema_version":1,"name":"baseline","concurrency_limit":1,"profiles":[{"scope":"global","profile":"bugs"}]}`)
	baseline := requireBaseline(t, manager)
	if baseline.Party.State != BaselineDiffers {
		t.Fatalf("Party state = %s", baseline.Party.State)
	}
	requireBlockedBy(t, baseline, path)
	if blocked := baseline.SelectionBlocked(ReviewSelection{Global: []SelectionItem{{Profile: "bugs"}}}); blocked != nil {
		t.Fatalf("selection without the baseline Party blocked: %v", blocked)
	}
}

func TestBaselineSelectAddsThePartyOnce(t *testing.T) {
	baseline := requireBaseline(t, baselineManager(t, t.TempDir()))
	item := SelectionItem{Party: BaselinePartyName}

	created := baseline.Select(ReviewSelection{}).Selection
	if want := (ReviewSelection{ConcurrencyLimit: 3, Global: []SelectionItem{item}, Repository: []SelectionItem{}}); !reflect.DeepEqual(created, want) {
		t.Fatalf("new selection = %#v", created)
	}
	existing := ReviewSelection{ConcurrencyLimit: 1, Repository: []SelectionItem{{Profile: "local"}}}
	again := baseline.Select(baseline.Select(existing).Selection).Selection
	if want := (ReviewSelection{ConcurrencyLimit: 1, Global: []SelectionItem{item}, Repository: existing.Repository}); !reflect.DeepEqual(again, want) {
		t.Fatalf("existing selection = %#v", again)
	}
}
