package store

import (
	"reflect"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestLedgerListsInFlightBundlesAndStandaloneReviewsForOneRepositoryNewestFirst(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	bundles := []model.ReviewBundle{
		inFlightBundleFixture("rb_1725192000000_000000000000000a", "/repo", model.LifecycleRunning, base),
		inFlightBundleFixture("rb_1725192000000_000000000000000b", "/repo", model.LifecyclePending, base.Add(2*time.Minute)),
		inFlightBundleFixture("rb_1725192000000_000000000000000c", "/repo", model.LifecycleCompleted, base.Add(3*time.Minute)),
		inFlightBundleFixture("rb_1725192000000_000000000000000d", "/repo", model.LifecycleIncomplete, base.Add(4*time.Minute)),
		inFlightBundleFixture("rb_1725192000000_000000000000000e", "/other", model.LifecycleRunning, base.Add(5*time.Minute)),
		inFlightBundleFixture("rb_1725192000000_000000000000000f", "/repo", model.LifecycleCompleted, base),
		inFlightBundleFixture("rb_1725192000000_0000000000000010", "/repo", model.LifecycleIncomplete, base),
	}
	bundles[5].Members = []model.BundleMember{{Scope: "global", Profile: "bugs"}}
	bundles[6].Members = []model.BundleMember{{Scope: "global", Profile: "bugs", ReviewID: "rp_1725192000000_000000000000000f"}}
	for _, bundle := range bundles {
		if err := ledger.CreateReviewBundle(bundle, nil); err != nil {
			t.Fatal(err)
		}
	}
	completed := ledgerFixture(model.LifecycleCompleted)
	completed.ID = "rp_1725192000000_000000000000000c"
	incomplete := ledgerFixture(model.LifecycleIncomplete)
	incomplete.ID = "rp_1725192000000_000000000000000d"
	saveTestReviews(t, ledger,
		inFlightReviewFixture("rp_1725192000000_000000000000000a", "/repo", model.LifecycleRunning, base.Add(time.Minute)),
		inFlightReviewFixture("rp_1725192000000_000000000000000b", "/repo", model.LifecyclePending, base.Add(3*time.Minute)),
		completed, incomplete,
		inFlightReviewFixture("rp_1725192000000_000000000000000e", "/other", model.LifecycleRunning, base.Add(5*time.Minute)),
		inFlightReviewFixture("rp_1725192000000_000000000000000f", "/repo", model.LifecycleRunning, base.Add(6*time.Minute)),
	)

	got, err := ledger.InFlight("/repo")
	if err != nil {
		t.Fatal(err)
	}
	want := InFlight{
		Bundles: []model.ReviewBundleID{"rb_1725192000000_000000000000000b", "rb_1725192000000_000000000000000a"},
		Reviews: []model.ReviewID{"rp_1725192000000_000000000000000b", "rp_1725192000000_000000000000000a"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("in flight = %#v, want %#v", got, want)
	}
}

func TestLedgerReportsNothingInFlightAsEmptyLists(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	got, err := ledger.InFlight("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Bundles == nil || got.Reviews == nil || len(got.Bundles) != 0 || len(got.Reviews) != 0 {
		t.Fatalf("in flight = %#v, want two empty, non-nil lists", got)
	}
}

func inFlightBundleFixture(id model.ReviewBundleID, repository string, lifecycle model.Lifecycle, created time.Time) model.ReviewBundle {
	return model.ReviewBundle{
		ID: id, Repository: repository, Lifecycle: lifecycle, Members: []model.BundleMember{},
		ConcurrencyLimit: 1, CreatedAt: created, UpdatedAt: created,
	}
}

func inFlightReviewFixture(id model.ReviewID, repository string, lifecycle model.Lifecycle, created time.Time) model.ReviewRecord {
	record := ledgerFixture(model.LifecyclePending)
	record.ID, record.Lifecycle, record.Termination = id, lifecycle, nil
	record.Subject.Repository = repository
	record.Passes = []model.PassRecord{{Name: "review", Required: true, Attempts: []model.AttemptRecord{}}}
	record.CreatedAt, record.UpdatedAt = created, created
	return record
}
