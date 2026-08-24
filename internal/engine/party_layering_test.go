package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestPartyExtendsConsolidatesIntoOneBundle(t *testing.T) {
	repository := changedTestRepository(t)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(findingsReview)})
	personalRoot := personalPartyRoot(t, conductor)
	writeTestPartyAt(t, filepath.Join(personalRoot, "parties"), model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "baseline",
		Profiles:      []model.PartyMember{{Profile: "bugs"}, {Profile: "documentation"}},
	})
	writeTestPartyAt(t, filepath.Join(repository, ".reviewparty", "parties"), model.PartyDefinition{
		SchemaVersion:    1,
		Name:             "gate",
		Extends:          []string{"baseline"},
		ConcurrencyLimit: 1,
		Profiles:         []model.PartyMember{{Profile: "code-quality"}},
	})

	bundle, err := conductor.RunParty(context.Background(), partySelection(repository, "gate"))
	if err != nil {
		t.Fatal(err)
	}
	assertCompletedBundle(t, bundle)
	if len(bundle.Members) != 3 {
		t.Fatalf("members = %#v, want the consolidated inherited plus local members", bundle.Members)
	}
	want := []string{"bugs", "documentation", "code-quality"}
	for index, profile := range want {
		if bundle.Members[index].Profile != profile {
			t.Fatalf("members[%d] = %q, want %q (inherited order then local additions)", index, bundle.Members[index].Profile, profile)
		}
	}
}

func TestPartyLocalMemberReplacesInheritedMemberSettings(t *testing.T) {
	repository := changedTestRepository(t)
	inherited := successfulExecutor(cleanReview)
	local := successfulExecutor(findingsReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{"grok": local, "opencode": inherited})
	personalRoot := personalPartyRoot(t, conductor)
	writeTestPartyAt(t, filepath.Join(personalRoot, "parties"), model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "baseline",
		Profiles:      []model.PartyMember{{Profile: "bugs", Reviewer: "opencode", Model: "global-model"}},
	})
	writeTestPartyAt(t, filepath.Join(repository, ".reviewparty", "parties"), model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "gate",
		Extends:       []string{"baseline"},
		Profiles:      []model.PartyMember{{Profile: "bugs", Reviewer: "grok"}},
	})

	bundle, err := conductor.RunParty(context.Background(), partySelection(repository, "gate"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Members) != 1 || bundle.Members[0].Profile != "bugs" {
		t.Fatalf("members = %#v, want exactly one bugs member", bundle.Members)
	}
	record, err := conductor.Inspect(context.Background(), bundle.Members[0].ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ProfileRevision.ReviewerID != "grok" {
		t.Fatalf("reviewer = %q, want the locally overriding grok", record.ProfileRevision.ReviewerID)
	}
	if inherited.attemptCount() != 0 {
		t.Fatal("inherited reviewer executed despite being replaced")
	}
	if local.attemptCount() != 1 {
		t.Fatalf("local attempts = %d, want one", local.attemptCount())
	}
}

func TestPartyDefinitionRejectsNullConcurrencyLimit(t *testing.T) {
	_, err := decodePartyDefinition([]byte(`{
  "schema_version": 1,
  "name": "gate",
  "profiles": [{"profile": "bugs"}],
  "concurrency_limit": null
}`), "gate")
	assertErrorMentions(t, err, "concurrency_limit must not be null")
}

func TestPartyExtendsCycleFailsClosedBeforeLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	parties := filepath.Join(repository, ".reviewparty", "parties")
	writeTestPartyAt(t, parties, model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "first",
		Extends:       []string{"second"},
		Profiles:      []model.PartyMember{{Profile: "bugs"}},
	})
	writeTestPartyAt(t, parties, model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "second",
		Extends:       []string{"first"},
		Profiles:      []model.PartyMember{{Profile: "code-quality"}},
	})

	_, err := conductor.RunParty(context.Background(), partySelection(repository, "first"))
	assertErrorMentions(t, err, "cycle")
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want none on cycle detection", executor.attemptCount())
	}
}

func TestPartyListingReportsExtendsCycle(t *testing.T) {
	repository := changedTestRepository(t)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(cleanReview)})
	parties := filepath.Join(repository, ".reviewparty", "parties")
	writeTestPartyAt(t, parties, model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "first",
		Extends:       []string{"second"},
		Profiles:      []model.PartyMember{{Profile: "bugs"}},
	})
	writeTestPartyAt(t, parties, model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "second",
		Extends:       []string{"first"},
		Profiles:      []model.PartyMember{{Profile: "code-quality"}},
	})

	summaries, err := conductor.PartiesForRepository(repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range summaries {
		if summary.Name == "first" {
			if !strings.Contains(summary.Error, "cycle") {
				t.Fatalf("first summary error = %q, want extends cycle", summary.Error)
			}
			return
		}
	}
	t.Fatal("first Party missing from listing")
}

func TestPartyUnknownExtendsTargetFailsBeforeLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeTestPartyAt(t, filepath.Join(repository, ".reviewparty", "parties"), model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "broken-parent",
		Extends:       []string{"missing-party"},
		Profiles:      []model.PartyMember{{Profile: "bugs"}},
	})

	_, err := conductor.RunParty(context.Background(), partySelection(repository, "broken-parent"))
	assertErrorMentions(t, err, "missing-party")
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want zero when a parent cannot resolve", executor.attemptCount())
	}
}

func TestBarePartyRunUsesDefaultPartyConfigurationChain(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writePersonalPartyConfig(t, conductor, `{"schema_version":1,"defaults":{"party":"standard"}}`)
	writeRepositoryPartyConfig(t, repository, `{"schema_version":1,"defaults":{"party":"gate"}}`)
	writeTestPartyAt(t, filepath.Join(repository, ".reviewparty", "parties"), model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "gate",
		Profiles:      []model.PartyMember{{Profile: "code-quality"}},
	})

	bundle, err := conductor.RunParty(context.Background(), model.PartySelection{Repository: repository, Subject: model.WorkingChanges()})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Party != "gate" {
		t.Fatalf("bundle party = %q, want the configured repository default gate", bundle.Party)
	}
}

func TestBarePartyRunFallsBackToPackagedStandard(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})

	bundle, err := conductor.RunParty(context.Background(), model.PartySelection{Repository: repository, Subject: model.WorkingChanges()})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Party != defaultPartyName || len(bundle.Members) != 3 {
		t.Fatalf("bundle = %s/%#v, want packaged %s with three members", bundle.Party, bundle.Members, defaultPartyName)
	}
}

func TestNegativePartyConcurrencyOverrideFailsBeforeLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	selection := partySelection(repository, "standard")
	selection.ConcurrencyLimit = -1

	_, err := conductor.RunParty(context.Background(), selection)
	assertErrorMentions(t, err, "must not be negative")
	if executor.attemptCount() != 0 {
		t.Fatalf("attempts = %d, want none for invalid concurrency", executor.attemptCount())
	}
}

func TestInheritedConcurrencyLimitDrivesExecution(t *testing.T) {
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
	personalRoot := personalPartyRoot(t, conductor)
	writeTestPartyAt(t, filepath.Join(personalRoot, "parties"), model.PartyDefinition{
		SchemaVersion:    1,
		Name:             "first-baseline",
		ConcurrencyLimit: limit,
		Profiles:         []model.PartyMember{{Profile: "bugs"}, {Profile: "code-quality"}},
	})
	writeTestPartyAt(t, filepath.Join(personalRoot, "parties"), model.PartyDefinition{
		SchemaVersion:    1,
		Name:             "second-baseline",
		ConcurrencyLimit: 4,
		Profiles:         []model.PartyMember{{Profile: "documentation"}},
	})
	writeTestPartyAt(t, filepath.Join(repository, ".reviewparty", "parties"), model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "gate",
		Extends:       []string{"first-baseline", "second-baseline"},
		Profiles:      []model.PartyMember{{Profile: "operability"}},
	})
	writeTestProfile(t, repository, "operability")

	bundle, err := conductor.RunParty(context.Background(), partySelection(repository, "gate"))
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ConcurrencyLimit != limit {
		t.Fatalf("bundle concurrency limit = %d, want inherited %d", bundle.ConcurrencyLimit, limit)
	}
	assertCompletedBundle(t, bundle)
	if seen := probe.max(); seen > limit {
		t.Fatalf("peak concurrent executions = %d, want at most %d", seen, limit)
	}
}

func TestPartyCanExtendPackagedStandard(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeTestPartyAt(t, filepath.Join(repository, ".reviewparty", "parties"), model.PartyDefinition{
		SchemaVersion: 1,
		Name:          "extended",
		Extends:       []string{"standard"},
		Profiles:      []model.PartyMember{{Profile: "security"}},
	})
	writeTestProfile(t, repository, "security")

	bundle, err := conductor.RunParty(context.Background(), partySelection(repository, "extended"))
	if err != nil {
		t.Fatal(err)
	}
	assertCompletedBundle(t, bundle)
	if len(bundle.Members) != 4 {
		t.Fatalf("members = %#v, want packaged three plus the local addition", bundle.Members)
	}
}

// writeTestPartyAt serializes the definition with its production JSON tags so
// the fixture exercises the exact documented file contract.
func writeTestPartyAt(t *testing.T, directory string, definition model.PartyDefinition) {
	t.Helper()
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, definition.Name+".json"), string(payload))
}

func writeTestProfile(t *testing.T, repository, name string) {
	t.Helper()
	directory := filepath.Join(repository, ".reviewparty", "profiles")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, name+".md"), "Act as a reviewer for "+name+" concerns.")
}

func personalPartyRoot(t *testing.T, conductor *Conductor) string {
	t.Helper()
	root, err := conductor.profiles.manager().PersonalRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writePersonalPartyConfig(t *testing.T, conductor *Conductor, payload string) {
	t.Helper()
	writeTestFile(t, filepath.Join(personalPartyRoot(t, conductor), "config.json"), payload)
}

func writeRepositoryPartyConfig(t *testing.T, repository, payload string) {
	t.Helper()
	directory := filepath.Join(repository, ".reviewparty")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "config.json"), payload)
}
