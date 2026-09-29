package store

import (
	"errors"
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
	defer closeTestResource(t, store.Close)
	created := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	completed := created.Add(2 * time.Minute)
	bundle := model.ReviewBundle{
		ID:              "rb_1724232000000_0123456789abcdef",
		Description:     "test bundle",
		Revision:        "abc123",
		Repository:      "/repo",
		SubjectKind:     model.SubjectWorkingChanges,
		SubjectIdentity: "identity-1",
		Lifecycle:       model.LifecycleIncomplete,
		Selection: &model.BundleSelection{
			Kind:             "repository_default",
			Source:           "/repo/.reviewparty/config.json",
			ConcurrencyLimit: 2,
			LimitSource:      "repository_selection",
			Authored: []model.BundleAuthoredItem{
				{Kind: "party", Name: "baseline", Scope: "global"},
				{Kind: "profile", Name: "security", Scope: "repository"},
			},
		},
		Warnings: []model.BundleWarning{{Category: "same_name_cross_scope", Name: "code-quality", Message: "warning text"}},
		Deduplicated: []model.SkippedDuplicate{{
			Scope: "global", Profile: "code-quality", Origin: "reviews.global[1]#0", KeptOrigin: "reviews.global[0]",
		}},
		Members: []model.BundleMember{
			{Scope: "global", Profile: "bugs", ProfileRevision: "rev-1", Origin: "reviews.global[0]#0", ReviewID: "rp_1724232000001_ffffffffffffffff", Lifecycle: model.LifecycleCompleted, Status: string(model.ResultFindings), FindingCount: 2},
			{Scope: "repository", Profile: "security", ProfileRevision: "rev-2", Origin: "reviews.repository[0]", ReviewID: "", Lifecycle: model.LifecyclePending},
		},
		ConcurrencyLimit: 2,
		CreatedAt:        created,
		UpdatedAt:        completed,
		CompletedAt:      completed,
		Termination: &model.BundleTermination{
			Category: model.TerminationCancelled,
			Message:  "caller cancelled the run",
		},
	}

	if err := store.CreateReviewBundle(bundle, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadReviewBundle(bundle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, bundle) {
		t.Fatalf("loaded = %#v, want %#v", loaded, bundle)
	}

	bundle.Members[1] = model.BundleMember{Scope: "repository", Profile: "security", ProfileRevision: "rev-2", Origin: "reviews.repository[0]", ReviewID: "rp_1724232000002_eeeeeeeeeeeeeeee", Lifecycle: model.LifecycleCompleted, Status: string(model.ResultClean)}
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

func TestLedgerPersistsBundleWithoutSelectionFacts(t *testing.T) {
	store, err := NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store.Close)
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	bundle := model.ReviewBundle{
		ID:               "rb_1724662800000_0123456789abcdef",
		Revision:         "def456",
		Repository:       "/repo",
		SubjectKind:      model.SubjectCommittedRange,
		SubjectIdentity:  "identity-2",
		Lifecycle:        model.LifecyclePending,
		Members:          []model.BundleMember{},
		ConcurrencyLimit: 1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := store.CreateReviewBundle(bundle, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadReviewBundle(bundle.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireAbsentSelectionFacts(t, loaded)
}

// requireAbsentSelectionFacts proves absent facts round-trip as empty
// collections rather than null.
func requireAbsentSelectionFacts(t *testing.T, loaded model.ReviewBundle) {
	t.Helper()
	if loaded.Selection != nil {
		t.Fatalf("selection = %#v, want nil", loaded.Selection)
	}
	for name, facts := range map[string][]string{"warnings": warningTexts(loaded.Warnings), "deduplicated": deduplicationTexts(loaded.Deduplicated)} {
		if len(facts) != 0 {
			t.Fatalf("%s did not round-trip empty: %#v", name, facts)
		}
	}
}

func TestLedgerRejectsSavingUnknownReviewBundle(t *testing.T) {
	store, err := NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store.Close)
	err = store.SaveReviewBundle(model.ReviewBundle{ID: "rb_1724232000099_0123456789abcdef"})
	if err == nil {
		t.Fatal("expected error saving a bundle that was never created")
	}
}

func warningTexts(warnings []model.BundleWarning) []string {
	texts := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		texts = append(texts, warning.Message)
	}
	return texts
}

func deduplicationTexts(duplicates []model.SkippedDuplicate) []string {
	texts := make([]string, 0, len(duplicates))
	for _, duplicate := range duplicates {
		texts = append(texts, duplicate.Origin+"→"+duplicate.KeptOrigin)
	}
	return texts
}

func TestLedgerCreatesReviewBundleWithPendingMemberRecords(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	bundle, members := pendingBundleFixture()
	if err := ledger.CreateReviewBundle(bundle, members); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.LoadReviewBundle(bundle.ID); err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		loaded, err := ledger.Load(member.ID)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Lifecycle != model.LifecyclePending {
			t.Fatalf("member %s lifecycle = %q, want pending", member.ID, loaded.Lifecycle)
		}
	}
}

func TestLedgerBundleCreationWritesNothingWhenAMemberFails(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	bundle, members := pendingBundleFixture()
	members[1].SchemaVersion = model.CurrentReviewRecordSchemaVersion + 1
	if err := ledger.CreateReviewBundle(bundle, members); err == nil {
		t.Fatal("expected the unsupported member schema to fail bundle creation")
	}
	if _, err := ledger.LoadReviewBundle(bundle.ID); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("bundle load error = %v, want ErrReviewNotFound", err)
	}
	if _, err := ledger.Load(members[0].ID); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("first member load error = %v, want ErrReviewNotFound", err)
	}
}

func pendingBundleFixture() (model.ReviewBundle, []model.ReviewRecord) {
	first := ledgerFixture(model.LifecyclePending)
	first.Termination = nil
	first.Passes = []model.PassRecord{{Name: "review", Required: true, Attempts: []model.AttemptRecord{}}}
	second := first
	second.ID = "rp_1723200000000_fedcba9876543210"
	bundle := model.ReviewBundle{
		ID: "rb_1723200000000_0123456789abcdef", Repository: "/repo", Lifecycle: model.LifecyclePending,
		Members: []model.BundleMember{
			{Scope: "global", Profile: "bugs", ReviewID: first.ID, Lifecycle: model.LifecyclePending},
			{Scope: "global", Profile: "bugs", ReviewID: second.ID, Lifecycle: model.LifecyclePending},
		},
		ConcurrencyLimit: 1, CreatedAt: first.CreatedAt, UpdatedAt: first.CreatedAt,
	}
	return bundle, []model.ReviewRecord{first, second}
}
