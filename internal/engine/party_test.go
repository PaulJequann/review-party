package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func testPartyConductor(t *testing.T, executors map[string]attemptExecutor) *Conductor {
	return testConductorWithExecutors(t, executors, time.Second)
}

func TestPartyPreflightFailsClosedBeforeAnyLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	_, err := conductor.RunParty(context.Background(), model.PartySelection{Name: "broken", Repository: repository, Subject: model.WorkingChanges()})
	if err == nil || !strings.Contains(err.Error(), "unknown review party") {
		t.Fatalf("error = %v", err)
	}
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want none", executor.attemptCount())
	}
}

func TestPartyExecutesSavedScopedProfile(t *testing.T) {
	repository := changedTestRepository(t)
	writeExecutableProfile(t, repository, "bugs")
	writeConfigurationParty(t, repository, configuration.Party{
		SchemaVersion: 1, Name: "gate", ConcurrencyLimit: 1,
		Profiles: []configuration.ProfileReference{{Scope: configuration.ScopeRepository, Profile: "bugs"}},
	})
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	bundle, err := conductor.RunParty(context.Background(), model.PartySelection{Name: "gate", Repository: repository, Subject: model.WorkingChanges()})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Lifecycle != model.LifecycleCompleted || executor.attemptCount() != 1 {
		t.Fatalf("bundle = %#v, attempts = %d", bundle, executor.attemptCount())
	}
}

func TestPartyRevisionAndBundleMembersRetainProfileScope(t *testing.T) {
	created := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	global := model.PartyDefinition{SchemaVersion: 1, Name: "dupes", ConcurrencyLimit: 1, Profiles: []model.PartyMember{{Scope: "global", Profile: "bugs"}}}
	repository := global
	repository.Profiles = []model.PartyMember{{Scope: "repository", Profile: "bugs"}}
	compiled := compiledProfile{revision: model.ProfileRevision{Name: "bugs", Revision: "same"}}
	globalMembers := []compiledPartyMember{{reference: global.Profiles[0], profile: compiled}}
	globalBundle, err := newPendingBundle(created, partyPlan{effective: global, members: globalMembers})
	if err != nil {
		t.Fatal(err)
	}
	repositoryMembers := []compiledPartyMember{{reference: repository.Profiles[0], profile: compiled}}
	repositoryBundle, err := newPendingBundle(created, partyPlan{effective: repository, members: repositoryMembers})
	if err != nil {
		t.Fatal(err)
	}
	if globalBundle.PartyRevision == repositoryBundle.PartyRevision {
		t.Fatal("Profile scope did not affect Party Revision")
	}
	if globalBundle.Members[0].Scope != "global" {
		t.Fatalf("Global member = %#v", globalBundle.Members[0])
	}
	if repositoryBundle.Members[0].Scope != "repository" {
		t.Fatalf("Repository member = %#v", repositoryBundle.Members[0])
	}
}

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
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "instructions.md"), []byte("Review material bugs.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
