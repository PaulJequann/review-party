package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func testPartyConductor(t *testing.T, executors map[string]attemptExecutor) *Conductor {
	return testConductorWithExecutors(t, executors, time.Second)
}

func partySelection(repository, name string) model.PartySelection {
	return model.PartySelection{Name: name, Repository: repository, Subject: model.WorkingChanges()}
}

func TestPartyPreflightFailsClosedBeforeAnyLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})

	_, err := conductor.RunParty(context.Background(), model.PartySelection{
		Name:       "broken",
		Repository: repository,
		Subject:    model.WorkingChanges(),
	})
	if err == nil || !strings.Contains(err.Error(), "unknown review party") {
		t.Fatalf("error = %v, want unknown review party", err)
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want none", executor.attemptCount())
	}
}

func TestPartyMemberCompilationFailureLaunchesNothing(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeTestParty(t, repository, model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "bad-member",
		Profiles:      []model.PartyMember{{Profile: "bugs"}, {Profile: "code-quality", Reviewer: "does-not-exist"}},
	})

	_, err := conductor.RunParty(context.Background(), partySelection(repository, "bad-member"))
	assertErrorMentions(t, err, "member \"code-quality\"", "unknown reviewer")
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want fail-closed preflight with zero attempts", executor.attemptCount())
	}
}

func assertErrorMentions(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error = %v, want it to mention %q", err, fragment)
		}
	}
}

func TestPartyMembersShareOneFrozenSubject(t *testing.T) {
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"frozen\"\n")
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(_ context.Context, _ attemptSpec) attemptExecution {
			writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \"mutated mid-party\"\n")
			return attemptExecution{AssistantText: findingsReview, Outcome: model.AttemptCompleted}
		},
	}
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeTestParty(t, repository, model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "duo",
		Profiles:      []model.PartyMember{{Profile: "bugs"}, {Profile: "code-quality"}},
	})

	bundle, err := conductor.RunParty(context.Background(), partySelection(repository, "duo"))
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("bundle lifecycle = %s, want completed", bundle.Lifecycle)
	}
	if len(bundle.Members) != 2 {
		t.Fatalf("members = %#v, want two", bundle.Members)
	}
	for _, member := range bundle.Members {
		assertChildUsesFrozenSubject(t, conductor, bundle, member.ReviewID)
	}
	assertCompletedFindingMembers(t, bundle, 1)
}

func assertChildUsesFrozenSubject(t *testing.T, conductor *Conductor, bundle model.ReviewBundle, id model.ReviewID) {
	t.Helper()
	record, err := conductor.Inspect(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if record.Subject.Identity != bundle.SubjectIdentity {
		t.Fatalf("child %s subject identity %q does not match bundle identity %q", record.ID, record.Subject.Identity, bundle.SubjectIdentity)
	}
	if !strings.Contains(record.Subject.Patch, "\"frozen\"") {
		t.Fatalf("child %s did not use the frozen subject:\n%s", record.ID, record.Subject.Patch)
	}
}

func assertCompletedFindingMembers(t *testing.T, bundle model.ReviewBundle, expectedFindingCount int) {
	t.Helper()
	for _, member := range bundle.Members {
		assertCompletedFindingMember(t, member, expectedFindingCount)
	}
}

func assertCompletedFindingMember(t *testing.T, member model.BundleMember, expectedFindingCount int) {
	t.Helper()
	if member.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("member = %#v, want completed", member)
	}
	if member.Status != string(model.ResultFindings) {
		t.Fatalf("member status = %q, want findings", member.Status)
	}
	if member.FindingCount != expectedFindingCount {
		t.Fatalf("member finding count = %d, want %d", member.FindingCount, expectedFindingCount)
	}
}

func TestPartyKeepsCompletedMemberVisibleWhenSiblingIsIncomplete(t *testing.T) {
	repository := changedTestRepository(t)
	conductor := testPartyConductor(t, map[string]attemptExecutor{
		defaultReviewer: successfulExecutor(findingsReview),
		"copilot":       &scriptedExecutor{availability: availability{Diagnostic: "unavailable harness"}},
	})
	writeTestParty(t, repository, model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "mixed",
		Profiles:      []model.PartyMember{{Profile: "bugs"}, {Profile: "code-quality", Reviewer: "copilot"}},
	})

	bundle, err := conductor.RunParty(context.Background(), partySelection(repository, "mixed"))
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("bundle lifecycle = %s, want incomplete", bundle.Lifecycle)
	}
	assertVisibleCompletedMember(t, bundle.Members[0])
	assertInspectableIncompleteMember(t, conductor, bundle.Members[1])
}

func assertVisibleCompletedMember(t *testing.T, member model.BundleMember) {
	t.Helper()
	if member.ReviewID == "" || member.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("member = %#v, want a persisted completed review", member)
	}
	if member.Status != string(model.ResultFindings) || member.FindingCount != 1 {
		t.Fatalf("member = %#v, want its finding preserved", member)
	}
}

func assertInspectableIncompleteMember(t *testing.T, conductor *Conductor, member model.BundleMember) {
	t.Helper()
	if member.ReviewID == "" || member.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("member = %#v, want an inspectable incomplete child review", member)
	}
	record, err := conductor.Inspect(context.Background(), member.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Lifecycle != model.LifecycleIncomplete || record.Termination == nil {
		t.Fatalf("child record lifecycle = %s, want honest incomplete termination", record.Lifecycle)
	}
}

func TestPartyPreservesManifestOrderUnderConcurrency(t *testing.T) {
	repository := changedTestRepository(t)
	slow := delayedExecutor(cleanReview, 200*time.Millisecond)
	fast := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{"grok": slow, "opencode": fast})
	writeTestParty(t, repository, model.PartyDefinition{
		SchemaVersion:    1,
		Name:             "ordered",
		ConcurrencyLimit: 2,
		Profiles:         []model.PartyMember{{Profile: "bugs", Reviewer: "opencode", Model: "test-model"}, {Profile: "code-quality", Reviewer: "grok"}},
	})

	bundle, err := conductor.RunParty(context.Background(), partySelection(repository, "ordered"))
	if err != nil {
		t.Fatal(err)
	}
	assertManifestOrderPreserved(t, conductor, bundle)
}

// delayedExecutor finishes slowly so a concurrent party's first-listed member
// completes last.
func delayedExecutor(output string, delay time.Duration) *scriptedExecutor {
	return &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(ctx context.Context, _ attemptSpec) attemptExecution {
			select {
			case <-ctx.Done():
				return attemptExecution{Outcome: model.AttemptCancelled}
			case <-time.After(delay):
				return attemptExecution{AssistantText: output, Outcome: model.AttemptCompleted}
			}
		},
	}
}

func assertManifestOrderPreserved(t *testing.T, conductor *Conductor, bundle model.ReviewBundle) {
	t.Helper()
	firstRecord, err := conductor.Inspect(context.Background(), bundle.Members[0].ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if firstRecord.ProfileRevision.Name != bundle.Members[0].Profile {
		t.Fatalf("members[0] maps to %q, want %q despite finishing last", firstRecord.ProfileRevision.Name, bundle.Members[0].Profile)
	}
	assertCompletedBundle(t, bundle)
}

func assertCompletedBundle(t *testing.T, bundle model.ReviewBundle) {
	t.Helper()
	if bundle.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("bundle lifecycle = %s, want completed", bundle.Lifecycle)
	}
	for _, member := range bundle.Members {
		if member.Lifecycle != model.LifecycleCompleted {
			t.Fatalf("member = %#v, want completed", member)
		}
	}
}

type gateProbe struct {
	mu      sync.Mutex
	active  int
	maxSeen int
}

func (probe *gateProbe) enter() {
	probe.mu.Lock()
	probe.active++
	if probe.active > probe.maxSeen {
		probe.maxSeen = probe.active
	}
	probe.mu.Unlock()
}

func (probe *gateProbe) exit() {
	probe.mu.Lock()
	probe.active--
	probe.mu.Unlock()
}

func (probe *gateProbe) max() int {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return probe.maxSeen
}

func TestPartyConcurrencyLimitBoundsActiveReviewers(t *testing.T) {
	repository := changedTestRepository(t)
	const limit = 2
	probe := &gateProbe{}
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(context.Context, attemptSpec) attemptExecution {
			probe.enter()
			defer probe.exit()
			time.Sleep(20 * time.Millisecond)
			return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
		},
	}
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})

	bundle, err := conductor.RunParty(context.Background(), model.PartySelection{
		Name:             "standard",
		Repository:       repository,
		Subject:          model.WorkingChanges(),
		ConcurrencyLimit: limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Lifecycle != model.LifecycleCompleted || len(bundle.Members) != 3 {
		t.Fatalf("bundle = %#v lifecycle %s, want three completed members", bundle.Members, bundle.Lifecycle)
	}
	if seen := probe.max(); seen > limit {
		t.Fatalf("peak concurrent executions = %d, want at most %d", seen, limit)
	}
	if seen := probe.max(); seen < 2 {
		t.Fatalf("peak concurrent executions = %d, want the configured parallelism to be exercised", seen)
	}
}

func TestPartyDefinitionConcurrencyLimitDrivesExecution(t *testing.T) {
	repository := changedTestRepository(t)
	probe := &gateProbe{}
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(context.Context, attemptSpec) attemptExecution {
			probe.enter()
			defer probe.exit()
			time.Sleep(20 * time.Millisecond)
			return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
		},
	}
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeTestParty(t, repository, model.PartyDefinition{
		SchemaVersion:    1,
		Name:             "parallel-file",
		ConcurrencyLimit: 2,
		Profiles:         []model.PartyMember{{Profile: "bugs"}, {Profile: "code-quality"}, {Profile: "documentation"}},
	})

	bundle, err := conductor.RunParty(context.Background(), partySelection(repository, "parallel-file"))
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ConcurrencyLimit != 2 || bundle.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("bundle = %#v lifecycle %s", bundle.Members, bundle.Lifecycle)
	}
	if seen := probe.max(); seen > 2 {
		t.Fatalf("peak concurrent executions = %d, want at most the definition limit 2", seen)
	}
	if seen := probe.max(); seen < 2 {
		t.Fatalf("peak concurrent executions = %d, want the file-configured parallelism honored", seen)
	}
}

func TestPartyListingShadowsPackagedWithRepositoryDefinition(t *testing.T) {
	repository := testRepository(t)
	writeTestParty(t, repository, model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "standard",
		Description:   "repository override",
		Profiles:      []model.PartyMember{{Profile: "bugs"}},
	})
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(cleanReview)})

	summaries, err := conductor.PartiesForRepository(repository)
	if err != nil {
		t.Fatal(err)
	}
	var standard *PartySummary
	for index := range summaries {
		if summaries[index].Name == "standard" {
			standard = &summaries[index]
		}
	}
	if standard == nil {
		t.Fatalf("summaries = %#v, want one standard entry", summaries)
	}
	if standard.Source != "repository" || len(standard.Members) != 1 {
		t.Fatalf("standard summary = %#v, want repository shadow replacing packaged definition", standard)
	}
}

func writeTestParty(t *testing.T, repository string, definition model.PartyDefinition) {
	t.Helper()
	payload := fmt.Sprintf(`{"schema_version":%d,"name":%q,"description":"test party","concurrency_limit":%d,"profiles":[`, definition.SchemaVersion, definition.Name, definition.ConcurrencyLimit)
	for index, member := range definition.Profiles {
		if index > 0 {
			payload += ","
		}
		payload += fmt.Sprintf(`{"profile":%q`, member.Profile)
		if member.Reviewer != "" {
			payload += fmt.Sprintf(`,"reviewer":%q`, member.Reviewer)
		}
		if member.Model != "" {
			payload += fmt.Sprintf(`,"model":%q`, member.Model)
		}
		if member.Effort != "" {
			payload += fmt.Sprintf(`,"effort":%q`, member.Effort)
		}
		payload += "}"
	}
	payload += "]}"
	directory := filepath.Join(repository, ".reviewparty", "parties")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, definition.Name+".json"), payload)
}
