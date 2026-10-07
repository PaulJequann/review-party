package configurationhub

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func baselineFirstUseManager(t *testing.T) *configuration.Manager {
	t.Helper()
	return configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
		Templates: []configuration.Template{
			{ID: "bugs", Revision: "v1", Instructions: "Review bugs.", Baseline: true},
			{ID: "docs", Revision: "v1", Instructions: "Review docs.", Baseline: true},
			{ID: "security", Revision: "v1", Instructions: "Review security."},
		},
	})
}

func requireCompleteBaseline(t *testing.T, manager *configuration.Manager, repository configuration.Repository) {
	t.Helper()
	baseline, err := manager.Baseline(repository)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Party.State != configuration.BaselineReady {
		t.Fatalf("Party state = %s", baseline.Party.State)
	}
	for _, member := range baseline.Members {
		if member.State != configuration.BaselineReady {
			t.Fatalf("member %s = %s", member.Template.ID, member.State)
		}
	}
}

func TestFirstUseBaselineIsTheInitialChoiceAndSharesOneExecution(t *testing.T) {
	manager := baselineFirstUseManager(t)
	repository := configuration.Repository(t.TempDir())

	runFirstUse(t, manager, repository,
		"",      // first review: the preselected Review Party baseline
		"",      // one execution for every missing member: default yes
		"codex", // reviewer
		"luna",  // model
		"high",  // effort
		"8m",    // deadline
		"y",     // publish the Profiles
		"y",     // publish the Party
		"y",     // publish the selection
		"3",     // no Checkpoint
	)

	requireCompleteBaseline(t, manager, repository)
	selection := selectionOf(t, manager, repository)
	if !slices.Equal(selection.Global, []configuration.SelectionItem{{Party: configuration.BaselinePartyName}}) || selection.ConcurrencyLimit != 2 {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestFirstUseBaselineCreatesEachMemberWhenExecutionIsNotShared(t *testing.T) {
	manager := baselineFirstUseManager(t)
	repository := configuration.Repository(t.TempDir())

	runFirstUse(t, manager, repository,
		"",                                 // first review: the Review Party baseline
		"n",                                // one Profile Creation per member
		"codex", "luna", "high", "8m", "y", // bugs
		"codex", "sol", "low", "4m", "y", // docs
		"y", // publish the Party
		"y", // publish the selection
		"3", // no Checkpoint
	)

	requireCompleteBaseline(t, manager, repository)
	for name, want := range map[string][]string{
		"bugs": {"codex", "luna", "high", "8m", "bugs"},
		"docs": {"codex", "sol", "low", "4m", "docs"},
	} {
		profile, _, err := manager.LoadProfile(configuration.ScopeGlobal, repository, name)
		if err != nil {
			t.Fatal(err)
		}
		if got := []string{profile.Reviewer, profile.Model, profile.ReasoningEffort, profile.AttemptDeadline, profile.TemplateID}; !slices.Equal(got, want) {
			t.Fatalf("Profile %s = %v, want %v", name, got, want)
		}
	}
}

func TestFirstUseBindsASelectedBaselineWithoutEditingTheSelection(t *testing.T) {
	repository := configuration.Repository(t.TempDir())
	author := baselineFirstUseManager(t)
	runFirstUse(t, author, repository, "", "", "codex", "luna", "high", "8m", "y", "y", "y", "3")
	path := filepath.Join(string(repository), ".reviewparty", "config.json")
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	teammate := baselineFirstUseManager(t)

	runFirstUse(t, teammate, repository,
		"",      // create the selected baseline: default yes
		"",      // one execution for every missing member: default yes
		"codex", // reviewer
		"luna",  // model
		"high",  // effort
		"8m",    // deadline
		"y",     // publish the Profiles
		"y",     // publish the Party
		"3",     // no Checkpoint
	)

	requireCompleteBaseline(t, teammate, repository)
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, committed) {
		t.Fatalf("Repository Configuration changed: %v\n%s", err, after)
	}
	binding, err := teammate.SelectionBinding(repository)
	if err != nil || !binding.Ready() {
		t.Fatalf("binding = %#v, error %v", binding, err)
	}
}

func TestFirstUseReportsASelectedDifferingBaselineAsBlocked(t *testing.T) {
	repository := configuration.Repository(t.TempDir())
	author := baselineFirstUseManager(t)
	runFirstUse(t, author, repository, "", "", "codex", "luna", "high", "8m", "y", "y", "y", "3")
	teammate := baselineFirstUseManager(t)
	publishGlobalProfile(t, teammate, "bugs")
	plan, err := teammate.PlanPartyCreation(repository, configuration.PartyDraft{
		Target: configuration.ScopeGlobal, Name: configuration.BaselinePartyName, ConcurrencyLimit: 1,
		Profiles: []configuration.ProfileReference{{Scope: configuration.ScopeGlobal, Profile: "bugs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := teammate.Publish(plan); err != nil {
		t.Fatal(err)
	}

	output := runFirstUse(t, teammate, repository, "3") // no Checkpoint

	if !strings.Contains(output, "Review Party baseline blocked: Global Party \"baseline\" at ") {
		t.Fatalf("output:\n%s", output)
	}
}

func TestFirstUseReportsABlockedBaselineInsteadOfOfferingIt(t *testing.T) {
	manager := baselineFirstUseManager(t)
	repository := configuration.Repository(t.TempDir())
	publishGlobalProfile(t, manager, "bugs")
	plan, err := manager.PlanPartyCreation(repository, configuration.PartyDraft{
		Target: configuration.ScopeGlobal, Name: configuration.BaselinePartyName, ConcurrencyLimit: 1,
		Profiles: []configuration.ProfileReference{{Scope: configuration.ScopeGlobal, Profile: "bugs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}

	output := runFirstUse(t, manager, repository,
		"2", // first review: Global Profile bugs
		"y", // publish the selection
		"3", // no Checkpoint
	)

	if !strings.Contains(output, "Review Party baseline blocked: Global Party \"baseline\" at ") || strings.Contains(output, "Review Party baseline (") {
		t.Fatalf("output:\n%s", output)
	}
}
