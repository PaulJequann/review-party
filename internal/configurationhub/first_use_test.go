package configurationhub

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
)

func firstUseManager(t *testing.T) *configuration.Manager {
	t.Helper()
	return configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
		Templates: []configuration.Template{
			{ID: "bugs", Revision: "v1", Instructions: "Review bugs."},
			{ID: "documentation", Revision: "v2", Instructions: "Review docs."},
		},
	})
}

func publishGlobalProfile(t *testing.T, manager *configuration.Manager, name string) {
	t.Helper()
	plan, err := manager.PlanProfileCreation("", configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: name, Reviewer: "codex",
		Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review.",
	})
	if err != nil {
		t.Fatalf("plan profile %q: %v", name, err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatalf("publish profile %q: %v", name, err)
	}
}

func publishGlobalParty(t *testing.T, manager *configuration.Manager, draft configuration.PartyDraft) {
	t.Helper()
	draft.Target = configuration.ScopeGlobal
	plan, err := manager.PlanPartyCreation("", draft)
	if err != nil {
		t.Fatalf("plan party %q: %v", draft.Name, err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatalf("publish party %q: %v", draft.Name, err)
	}
}

func publishSelection(t *testing.T, manager *configuration.Manager, repository configuration.Repository, selection configuration.ReviewSelection) {
	t.Helper()
	plan, err := manager.Plan(repository, []configuration.Intent{configuration.SetReviewSelection{Selection: selection}})
	if err != nil {
		t.Fatalf("plan selection: %v", err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatalf("publish selection: %v", err)
	}
}

func runFirstUse(t *testing.T, manager *configuration.Manager, repository configuration.Repository, script ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var output bytes.Buffer
	err := RunFirstUse(manager, RunOptions{
		Context: ctx, Repository: repository, Accessible: true,
		Input: newHoldingInput(strings.Join(script, "\n") + "\n"), Output: &output,
	})
	if err != nil {
		t.Fatalf("RunFirstUse: %v\noutput:\n%s", err, output.String())
	}
	return output.String()
}

func selectionOf(t *testing.T, manager *configuration.Manager, repository configuration.Repository) configuration.ReviewSelection {
	t.Helper()
	selection, _, err := manager.EffectiveReviewSelection(repository)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func TestFirstUseCreatesADeclaredGlobalProfileFromItsSameNamedTemplate(t *testing.T) {
	author := firstUseManager(t)
	repository := configuration.Repository(t.TempDir())
	publishGlobalProfile(t, author, "documentation")
	publishSelection(t, author, repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: "documentation"}},
	})
	teammate := firstUseManager(t)

	runFirstUse(t, teammate, repository,
		"",      // create Global Profile "documentation": default yes
		"codex", // reviewer
		"luna",  // model
		"high",  // effort
		"8m",    // deadline
		"",      // instruction source: default Template
		"",      // Template: the preselected same-named one, listed second
		"n",     // keep the Template instructions
		"y",     // publish
	)

	profile, found, err := teammate.LoadProfile(configuration.ScopeGlobal, "", "documentation")
	if err != nil || !found {
		t.Fatalf("Global Profile documentation found=%v err=%v", found, err)
	}
	if profile.TemplateID != "documentation" || profile.TemplateRevision != "v2" {
		t.Fatalf("profile template = %q@%q, want documentation@v2", profile.TemplateID, profile.TemplateRevision)
	}
	binding, err := teammate.SelectionBinding(repository)
	if err != nil || !binding.Ready() {
		t.Fatalf("binding after first use = %#v %v, want ready", binding, err)
	}
}

func TestFirstUsePublishesAChosenExistingProfileAsTheSelection(t *testing.T) {
	manager := firstUseManager(t)
	repository := configuration.Repository(t.TempDir())
	publishGlobalProfile(t, manager, "bugs")

	runFirstUse(t, manager, repository,
		"1", // Global Profile bugs
		"y", // publish the selection
	)

	selection := selectionOf(t, manager, repository)
	if !slices.Equal(selection.Global, []configuration.SelectionItem{{Profile: "bugs"}}) || len(selection.Repository) != 0 {
		t.Fatalf("selection = %#v, want only Global Profile bugs", selection)
	}
}

func TestFirstUseSavesAChosenGlobalPartyAsARepositoryParty(t *testing.T) {
	manager := firstUseManager(t)
	repository := configuration.Repository(t.TempDir())
	publishGlobalProfile(t, manager, "bugs")
	members := []configuration.ProfileReference{{Scope: configuration.ScopeGlobal, Profile: "bugs"}}
	publishGlobalParty(t, manager, configuration.PartyDraft{Name: "crew", Description: "Shared crew", ConcurrencyLimit: 1, Profiles: members})

	output := runFirstUse(t, manager, repository,
		"1", // Global Party crew
		"",  // save as a Repository Party: default yes
		"y", // publish the Repository Party
		"y", // publish the selection
	)

	party, found, err := manager.LoadParty(configuration.ScopeRepository, repository, "crew")
	if err != nil || !found {
		t.Fatalf("Repository Party crew found=%v err=%v", found, err)
	}
	if !slices.Equal(party.Profiles, members) || party.Description != "Shared crew" {
		t.Fatalf("Repository Party = %#v, want the Global composition", party)
	}
	selection := selectionOf(t, manager, repository)
	if !slices.Equal(selection.Repository, []configuration.SelectionItem{{Party: "crew"}}) || len(selection.Global) != 0 {
		t.Fatalf("selection = %#v, want only Repository Party crew", selection)
	}
	if !strings.Contains(output, "Applied the shared-repository default") {
		t.Fatalf("output did not report the applied default:\n%s", output)
	}
}
