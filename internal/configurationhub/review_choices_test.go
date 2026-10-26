package configurationhub

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func TestReviewChoicesUseValidScopedInventory(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}})
	repository := configuration.Repository(t.TempDir())
	publishPreviewProfile(t, manager, configuration.ScopeGlobal, "")
	publishPreviewProfile(t, manager, configuration.ScopeRepository, repository)
	invalid := filepath.Join(string(repository), ".reviewparty", "profiles", "invalid")
	if err := os.MkdirAll(invalid, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalid, "profile.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := buildSnapshot(manager, repository)
	if err != nil {
		t.Fatal(err)
	}
	want := []configuration.SelectedDefinition{
		{Kind: configuration.ItemProfile, Name: "shared", Scope: configuration.ScopeRepository},
		{Kind: configuration.ItemProfile, Name: "shared", Scope: configuration.ScopeGlobal},
	}
	if !reflect.DeepEqual(snapshot.ReviewChoices, want) {
		t.Fatalf("choices = %#v, want %#v", snapshot.ReviewChoices, want)
	}
	for index, choice := range snapshot.ReviewChoices {
		draft := reviewFormDraft{operation: "add", choice: choice}
		var output bytes.Buffer
		e := editor{snapshot: snapshot, drafts: &draftSet{reviews: draft}, RunOptions: RunOptions{
			Accessible: true, Input: newLineInput(fmt.Sprintf("%d\n", index+1)), Output: &output,
		}}
		intent, err := e.addReview(configuration.DefaultReviewSelection())
		if err != nil {
			t.Fatal(err)
		}
		normal, err := addReviewIntent(e.draftSet().reviews, configuration.DefaultReviewSelection())
		if err != nil || !reflect.DeepEqual(intent, normal) || !reflect.DeepEqual(e.draftSet().reviews.choice, choice) {
			t.Fatalf("adapters disagree: %#v, %#v, %v", intent, normal, err)
		}
		for _, label := range []string{"[global] profile shared", "[repository] profile shared", "retains existing entries"} {
			if !strings.Contains(output.String(), label) {
				t.Fatalf("missing %q in %s", label, output.String())
			}
		}
	}
}

func TestReviewAddRetainsEntriesAndShowsFinalWorkloadInBothAdapters(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}})
	repository := configuration.Repository(t.TempDir())
	publishPreviewProfile(t, manager, configuration.ScopeGlobal, "")
	publishPreviewProfile(t, manager, configuration.ScopeRepository, repository)
	plan, err := manager.PlanPartyCreation(repository, configuration.PartyDraft{
		Target: configuration.ScopeRepository, Name: "daily", ConcurrencyLimit: 7,
		Profiles: []configuration.ProfileReference{
			{Scope: configuration.ScopeGlobal, Profile: "shared"},
			{Scope: configuration.ScopeRepository, Profile: "shared"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	selection := configuration.ReviewSelection{ConcurrencyLimit: 2, Global: []configuration.SelectionItem{{Profile: "shared"}}}
	draft := reviewFormDraft{operation: "add", choice: configuration.SelectedDefinition{Kind: configuration.ItemParty, Scope: configuration.ScopeRepository, Name: "daily"}}
	intent, err := addReviewIntent(draft, selection)
	if err != nil {
		t.Fatal(err)
	}
	again, err := addReviewIntent(draft, intent.Selection)
	if err != nil || !reflect.DeepEqual(intent, again) {
		t.Fatalf("Add changed existing deduplication: %#v, %v", again, err)
	}
	message := planRequestCommand(context.Background(), manager, repository, nil, planRequest{
		kind: planReviews, review: reviewPlanRequest{selection: selection, draft: draft},
	})()
	ready, ok := message.(planReadyMsg)
	if !ok {
		t.Fatalf("plan result = %#v", message)
	}
	var accessible bytes.Buffer
	if err := renderReviewSelectionPreview(&accessible, manager, repository, intent.Selection); err != nil {
		t.Fatal(err)
	}
	for _, preview := range []string{ready.summary, accessible.String()} {
		for _, want := range []string{"selected [global] profile shared", "selected [repository] party daily", "effective concurrency: 2", "[global] shared", "[repository] shared", "deduplicated [global] shared", "Profiles named"} {
			if !strings.Contains(preview, want) {
				t.Fatalf("preview lacks %q: %s", want, preview)
			}
		}
	}
	if _, source, err := manager.EffectiveReviewSelection(repository); err != nil || source.Authored {
		t.Fatalf("preview wrote a selection: %#v, %v", source, err)
	}
}

func TestEmptyReviewChoicesOfferCreationAndReturn(t *testing.T) {
	model := New(Snapshot{Items: []Item{{Kind: itemProfile, Detail: "looks valid", Name: "invalid"}}})
	model.session.reviews.operation = "add"
	if command := model.openReviewFieldsForm(); command != nil || model.view != viewMenu || !strings.Contains(model.status, "Create a Profile") {
		t.Fatalf("empty inventory did not return with guidance: %s", model.status)
	}
	e := editor{RunOptions: RunOptions{Input: newLineInput(""), Output: &bytes.Buffer{}, Accessible: true}}
	if _, err := e.addReview(configuration.DefaultReviewSelection()); err == nil || !strings.Contains(err.Error(), "Create a Profile") {
		t.Fatalf("accessible empty inventory = %v", err)
	}
}

func TestRejectedStaleReviewChoiceRetainsDraft(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}})
	repository := configuration.Repository(t.TempDir())
	draft := reviewFormDraft{operation: "add", choice: configuration.SelectedDefinition{Kind: configuration.ItemProfile, Scope: configuration.ScopeRepository, Name: "vanished"}}
	message := planRequestCommand(context.Background(), manager, repository, nil, planRequest{
		kind: planReviews, review: reviewPlanRequest{selection: configuration.DefaultReviewSelection(), draft: draft},
	})()
	if _, ok := message.(planFailedMsg); !ok {
		t.Fatalf("missing definition was accepted: %#v", message)
	}
	model := New(Snapshot{ReviewChoices: []configuration.SelectedDefinition{draft.choice}})
	model.drafts.reviews = draft
	model.session = newFormSession(model.drafts)
	updated, _ := model.Update(message)
	model = requireHubModel(t, updated)
	if model.drafts.reviews != draft || model.session.reviews != draft || !strings.Contains(model.status, "Planning failed") {
		t.Fatalf("rejection lost draft: %#v", model.drafts.reviews)
	}
}
