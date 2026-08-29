package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func testPartyConductor(t *testing.T, executors map[string]attemptExecutor) *Conductor {
	return testConductorWithExecutors(t, executors, time.Second)
}

// writeRepositorySelection authors a complete repository default roll-up.
func writeRepositorySelection(t *testing.T, repository string, selection configuration.ReviewSelection) {
	t.Helper()
	payload := struct {
		SchemaVersion int                           `json:"schema_version"`
		Reviews       configuration.ReviewSelection `json:"reviews"`
	}{SchemaVersion: 1, Reviews: selection}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository, ".reviewparty", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeGlobalParty(t *testing.T, globalRoot string, party configuration.Party) {
	t.Helper()
	payload, err := json.Marshal(party)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(globalRoot, "parties", party.Name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeExecutableProfile authors a complete repository-scoped Profile.
func writeExecutableProfile(t *testing.T, repository, name string) {
	t.Helper()
	profile := configuration.Profile{SchemaVersion: 1, Name: name, Reviewer: defaultReviewer, Model: "grok-4.5", ReasoningEffort: "high", AttemptDeadline: "1m"}
	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(repository, ".reviewparty", "profiles", name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	instructions := "Review material " + name + "."
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "instructions.md"), []byte(instructions+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runRun(t *testing.T, conductor *Conductor, selection model.RunSelection) model.ReviewBundle {
	t.Helper()
	bundle, err := conductor.Run(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func runRunExpectingFailure(t *testing.T, conductor *Conductor, selection model.RunSelection) error {
	t.Helper()
	_, err := conductor.Run(context.Background(), selection)
	if err == nil {
		t.Fatal("run unexpectedly succeeded")
	}
	return err
}

func requireAttemptCount(t *testing.T, executor *scriptedExecutor, want int) {
	t.Helper()
	if got := executor.attemptCount(); got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func requireBundleOutcome(t *testing.T, bundle model.ReviewBundle, wantMembers int, wantLifecycle model.Lifecycle) {
	t.Helper()
	if bundle.Lifecycle != wantLifecycle || len(bundle.Members) != wantMembers {
		t.Fatalf("bundle = %#v, want %d member(s) with lifecycle %q", bundle, wantMembers, wantLifecycle)
	}
}

// requireScopedMember asserts the ordered slot holds "scope:profile".
func requireScopedMember(t *testing.T, bundle model.ReviewBundle, index int, scopedProfile string) {
	t.Helper()
	member := bundle.Members[index]
	want := parseScopedName(t, scopedProfile)
	if member.Scope != want.scope || member.Profile != want.profile {
		t.Fatalf("member %d = %s:%s, want %q", index, member.Scope, member.Profile, scopedProfile)
	}
}

type scopedName struct{ scope, profile string }

func parseScopedName(t *testing.T, value string) scopedName {
	t.Helper()
	scope, profile, found := strings.Cut(value, ":")
	if !found {
		t.Fatalf("scoped name %q must look like scope:profile", value)
	}
	return scopedName{scope: scope, profile: profile}
}

// requireExecutedMember asserts one executed slot carries its scoped identity,
// origin, and exact Profile Revision.
func requireExecutedMember(t *testing.T, bundle model.ReviewBundle, index int, scopedProfile string) {
	t.Helper()
	requireScopedMember(t, bundle, index, scopedProfile)
	member := bundle.Members[index]
	if member.ProfileRevision == "" {
		t.Fatalf("member %d has no Profile Revision: %#v", index, member)
	}
	if member.Origin == "" {
		t.Fatalf("member %d has no origin: %#v", index, member)
	}
}

type bundleSelectionFacts struct {
	kind        string
	limitSource string
	limit       int
}

func requireSelectionFacts(t *testing.T, selection *model.BundleSelection, want bundleSelectionFacts) {
	t.Helper()
	if selection == nil {
		t.Fatal("bundle carries no selection record")
	}
	if selection.Kind != want.kind {
		t.Fatalf("selection kind = %q, want %q", selection.Kind, want.kind)
	}
	requireLimitFacts(t, selection, want)
}

func requireLimitFacts(t *testing.T, selection *model.BundleSelection, want bundleSelectionFacts) {
	t.Helper()
	if selection.ConcurrencyLimit != want.limit || selection.LimitSource != want.limitSource {
		t.Fatalf("limit facts = (%d, %q), want (%d, %q)", selection.ConcurrencyLimit, selection.LimitSource, want.limit, want.limitSource)
	}
}

func requireCrossScopeWarning(t *testing.T, warnings []model.BundleWarning, name string) {
	t.Helper()
	for _, warning := range warnings {
		isMatch := warning.Category == "same_name_cross_scope"
		nameMatches := warning.Name == name
		if isMatch && nameMatches {
			return
		}
	}
	t.Fatalf("warnings = %#v, want same-name cross-scope warning for %q", warnings, name)
}

func equalStringSlices(got, want []string) bool {
	return reflect.DeepEqual(got, want)
}

// promptProfileLabel returns the instructions sentence that opened the prompt.
func promptProfileLabel(prompt string) string {
	firstLine := prompt
	if index := strings.IndexByte(prompt, '\n'); index >= 0 {
		firstLine = prompt[:index]
	}
	return strings.TrimSuffix(firstLine, ".")
}

func capturePromptLabels(_ *testing.T) (*[]string, *scriptedExecutor) {
	var mu sync.Mutex
	var order []string
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(_ context.Context, spec attemptSpec) attemptExecution {
			mu.Lock()
			order = append(order, promptProfileLabel(spec.Prompt))
			mu.Unlock()
			return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
		},
	}
	return &order, executor
}

func assertPersistedSelectionMatches(t *testing.T, executed, persisted model.ReviewBundle) {
	t.Helper()
	if executed.ID != persisted.ID || executed.Revision != persisted.Revision {
		t.Fatalf("identity mismatch\nexecuted = %#v\npersisted = %#v", executed, persisted)
	}
	if !reflect.DeepEqual(executed.Selection, persisted.Selection) {
		t.Fatalf("selection mismatch\nexecuted = %#v\npersisted = %#v", executed.Selection, persisted.Selection)
	}
	if len(persisted.Members) != len(executed.Members) {
		t.Fatalf("members mismatch\nexecuted = %#v\npersisted = %#v", executed.Members, persisted.Members)
	}
	for index := range executed.Members {
		requireSameExecutedIdentity(t, index, executed.Members[index], persisted.Members[index])
	}
}

func requireSameExecutedIdentity(t *testing.T, index int, executed, persisted model.BundleMember) {
	t.Helper()
	executed.Lifecycle, persisted.Lifecycle = "", ""
	executed.ReviewID, persisted.ReviewID = "", ""
	executed.Status, persisted.Status = "", ""
	executed.FindingCount, persisted.FindingCount = 0, 0
	if executed != persisted {
		t.Fatalf("member %d identity mismatch\nexecuted = %#v\npersisted = %#v", index, executed, persisted)
	}
}

func TestRunPreflightFailsClosedBeforeAnyLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "ghosts"}},
		Repository:       []configuration.SelectionItem{},
	})
	err := runRunExpectingFailure(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})
	if !strings.Contains(err.Error(), "ghosts") {
		t.Fatalf("error = %v, want missing reference failure", err)
	}
	requireAttemptCount(t, executor, 0)
}

func TestRunExecutesSavedRollupInAuthoredOrder(t *testing.T) {
	repository := changedTestRepository(t)
	order, executor := capturePromptLabels(t)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	localDocs := writeLocalDocsProfile(t, repository)
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "code-quality"}, {Profile: "bugs"}},
		Repository:       []configuration.SelectionItem{{Profile: localDocs}},
	})
	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})
	requireBundleOutcome(t, bundle, 3, model.LifecycleCompleted)
	requireAttemptCount(t, executor, 3)
	requireExecutedMember(t, bundle, 0, "global:code-quality")
	requireExecutedMember(t, bundle, 1, "global:bugs")
	requireExecutedMember(t, bundle, 2, "repository:"+localDocs)
	wantOrder := []string{"Review code-quality concerns", "Review bugs concerns", "Review material local-docs"}
	if !equalStringSlices(*order, wantOrder) {
		t.Fatalf("execution order = %#v", *order)
	}
	requireSelectionFacts(t, bundle.Selection, bundleSelectionFacts{"repository_default", "repository_selection", 1})
	if !strings.HasSuffix(bundle.Selection.Source, ".reviewparty/config.json") {
		t.Fatalf("selection source = %q", bundle.Selection.Source)
	}
	persisted := inspectBundleRecord(t, conductor, bundle.ID)
	assertPersistedSelectionMatches(t, bundle, persisted)
}

// writeLocalDocsProfile creates a unique repository-scoped Profile for
// roll-up ordering fixtures and returns its name.
func writeLocalDocsProfile(t *testing.T, repository string) string {
	t.Helper()
	const name = "local-docs"
	writeExecutableProfile(t, repository, name)
	return name
}

func inspectBundleRecord(t *testing.T, conductor *Conductor, id model.ReviewBundleID) model.ReviewBundle {
	t.Helper()
	bundle, err := conductor.InspectBundle(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestRunExplicitProfileReplacesDefaultForOneRun(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	localDocs := writeLocalDocsProfile(t, repository)
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "code-quality"}},
		Repository:       []configuration.SelectionItem{{Profile: localDocs}},
	})
	selection := model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Profile: "repository:" + localDocs}
	bundle := runRun(t, conductor, selection)
	requireBundleOutcome(t, bundle, 1, model.LifecycleCompleted)
	requireScopedMember(t, bundle, 0, "repository:"+localDocs)
	if bundle.Members[0].ReviewID == "" {
		t.Fatal("sequential bundle member has no Review ID")
	}
	if bundle.Members[0].Origin != "explicit" {
		t.Fatalf("origin = %q, want explicit", bundle.Members[0].Origin)
	}
	requireSelectionFacts(t, bundle.Selection, bundleSelectionFacts{"explicit_profile", "explicit_profile", 1})
	if bundle.Selection.Source != "explicit" {
		t.Fatalf("source = %q", bundle.Selection.Source)
	}
	requireAttemptCount(t, executor, 1)
}

func TestRunExplicitPartyUsesItsOwnLimitAndMemberIdentity(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeExecutableProfile(t, repository, "release-check")
	writeConfigurationParty(t, repository, explicitGateParty())
	writeRepositorySelection(t, repository, savedBugsOnlySelection())
	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Party: "gate"})
	requireBundleOutcome(t, bundle, 1, model.LifecycleCompleted)
	requireScopedMember(t, bundle, 0, "repository:release-check")
	requireSelectionFacts(t, bundle.Selection, bundleSelectionFacts{"explicit_party", "party", 3})
	requireAuthoredName(t, bundle.Selection.Authored, "gate")
	requireAttemptCount(t, executor, 1)
}

func explicitGateParty() configuration.Party {
	return configuration.Party{
		SchemaVersion:    1,
		Name:             "gate",
		ConcurrencyLimit: 3,
		Profiles:         []configuration.ProfileReference{{Scope: configuration.ScopeRepository, Profile: "release-check"}},
	}
}

func savedBugsOnlySelection() configuration.ReviewSelection {
	return configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}},
		Repository:       []configuration.SelectionItem{},
	}
}

func requireAuthoredName(t *testing.T, authored []model.BundleAuthoredItem, want string) {
	t.Helper()
	if len(authored) != 1 || authored[0].Name != want {
		t.Fatalf("authored = %#v, want exactly %q", authored, want)
	}
}

func TestRunWarnsWhenSameNamedProfilesFromBothScopesExecute(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeExecutableProfile(t, repository, "code-quality")
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "code-quality"}},
		Repository:       []configuration.SelectionItem{{Profile: "code-quality"}},
	})
	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})
	requireBundleOutcome(t, bundle, 2, model.LifecycleCompleted)
	requireAttemptCount(t, executor, 2)
	requireCrossScopeWarning(t, bundle.Warnings, "code-quality")
	persisted := inspectBundleRecord(t, conductor, bundle.ID)
	requireCrossScopeWarning(t, persisted.Warnings, "code-quality")
}

func TestRunDeduplicatesExactScopedOccurrencesAcrossSources(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	globalRoot, err := conductor.configuration.GlobalRoot()
	if err != nil {
		t.Fatal(err)
	}
	writeGlobalParty(t, globalRoot, overlappingDocumentsParty())
	writeRepositorySelection(t, repository, overlappingSelection())
	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})
	requireBundleOutcome(t, bundle, 2, model.LifecycleCompleted)
	requireAttemptCount(t, executor, 2)
	requireFirstOccurrenceKept(t, bundle.Deduplicated)
	persisted := inspectBundleRecord(t, conductor, bundle.ID)
	requireFirstOccurrenceKept(t, persisted.Deduplicated)
}

func overlappingDocumentsParty() configuration.Party {
	return configuration.Party{
		SchemaVersion:    1,
		Name:             "overlaps",
		ConcurrencyLimit: 1,
		Profiles: []configuration.ProfileReference{
			{Scope: configuration.ScopeGlobal, Profile: "bugs"},
			{Scope: configuration.ScopeGlobal, Profile: "documentation"},
		},
	}
}

func overlappingSelection() configuration.ReviewSelection {
	return configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}, {Party: "overlaps"}},
		Repository:       []configuration.SelectionItem{},
	}
}

func requireFirstOccurrenceKept(t *testing.T, duplicates []model.SkippedDuplicate) {
	t.Helper()
	want := model.SkippedDuplicate{
		Scope: "global", Profile: "bugs",
		Origin: "reviews.global[1]#0", KeptOrigin: "reviews.global[0]",
	}
	if len(duplicates) != 1 || duplicates[0] != want {
		t.Fatalf("deduplicated = %#v, want exactly %#v", duplicates, want)
	}
}

func TestRunHonorsTheAuthoredConcurrencyLimit(t *testing.T) {
	repository := changedTestRepository(t)
	arrivals, release, executor := barrierExecutor(t)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	go func() {
		arrivals.Wait()
		close(*release)
	}()
	writeRepositorySelection(t, repository, concurrentTwoProfilesSelection())
	runContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bundle, err := conductor.Run(runContext, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})
	if err != nil {
		t.Fatal(err)
	}
	requireBundleOutcome(t, bundle, 2, model.LifecycleCompleted)
	requireSelectionFacts(t, bundle.Selection, bundleSelectionFacts{"repository_default", "repository_selection", 2})
}

// barrierExecutor releases its callers only after two arrivals prove the
// authored Concurrency Limit allowed parallel execution.
func barrierExecutor(t *testing.T) (*sync.WaitGroup, *chan struct{}, *scriptedExecutor) {
	var arrivals sync.WaitGroup
	arrivals.Add(2)
	release := make(chan struct{})
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(ctx context.Context, _ attemptSpec) attemptExecution {
			arrivals.Done()
			select {
			case <-release:
				return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
			case <-ctx.Done():
				return failedExecution(model.AttemptCancelled, model.TerminationCancelled, model.PhaseReviewerExecution, ctx.Err().Error())
			case <-time.After(5 * time.Second):
				return failedExecution(model.AttemptUnknownFailure, model.TerminationUnknownFailure, model.PhaseReviewerExecution, "concurrency barrier timed out")
			}
		},
	}
	return &arrivals, &release, executor
}

func concurrentTwoProfilesSelection() configuration.ReviewSelection {
	return configuration.ReviewSelection{
		ConcurrencyLimit: 2,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}, {Profile: "documentation"}},
		Repository:       []configuration.SelectionItem{},
	}
}
