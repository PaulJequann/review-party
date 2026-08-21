package store

import (
	"reflect"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestLedgerRoundTripsReviewBundle(t *testing.T) {
	store, err := NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	completed := created.Add(2 * time.Minute)
	bundle := model.ReviewBundle{
		ID:              "rb_1724232000000_0123456789abcdef",
		Party:           "standard",
		Description:     "test bundle",
		PartyRevision:   "abc123",
		Repository:      "/repo",
		SubjectKind:     model.SubjectWorkingChanges,
		SubjectIdentity: "identity-1",
		Lifecycle:       model.LifecycleIncomplete,
		Members: []model.BundleMember{
			{Profile: "bugs", ReviewID: "rp_1724232000001_ffffffffffffffff", Lifecycle: model.LifecycleCompleted, Status: string(model.ResultFindings), FindingCount: 2},
			{Profile: "code-quality", ReviewID: "", Lifecycle: model.LifecyclePending},
		},
		ConcurrencyLimit: 2,
		CreatedAt:        created,
		UpdatedAt:        completed,
		CompletedAt:      completed,
		Termination: &model.BundleTermination{
			Category: model.TerminationCancelled,
			Message:  "caller cancelled the party",
		},
	}

	if err := store.CreateReviewBundle(bundle); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadReviewBundle(bundle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, bundle) {
		t.Fatalf("loaded = %#v, want %#v", loaded, bundle)
	}

	bundle.Members[1] = model.BundleMember{Profile: "code-quality", ReviewID: "rp_1724232000002_eeeeeeeeeeeeeeee", Lifecycle: model.LifecycleCompleted, Status: string(model.ResultClean)}
	bundle.Lifecycle = model.LifecycleCompleted
	bundle.Termination = nil
	bundle.UpdatedAt = completed
	if err := store.SaveReviewBundle(bundle); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.LoadReviewBundle(bundle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, bundle) {
		t.Fatalf("reloaded = %#v, want %#v", reloaded, bundle)
	}
}

func TestLedgerRejectsSavingUnknownReviewBundle(t *testing.T) {
	store, err := NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.SaveReviewBundle(model.ReviewBundle{ID: "rb_1724232000099_0123456789abcdef"})
	if err == nil {
		t.Fatal("expected error saving a bundle that was never created")
	}
}
