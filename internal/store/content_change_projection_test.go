package store

import (
	"reflect"
	"testing"
	"time"

	"reviewparty/internal/model"
)

const coverageSource = "repository:.reviewparty/profiles/bugs"

var coveredChanges = []model.ContentChange{
	{Path: "a.go", Before: model.ZeroObjectID, After: "1111111111111111111111111111111111111111"},
	{Path: "b.go", Before: "2222222222222222222222222222222222222222", After: "3333333333333333333333333333333333333333"},
}

func coverageFixture(id model.ReviewID, lifecycle model.Lifecycle, changes []model.ContentChange, minute int) model.ReviewRecord {
	record := ledgerFixture(lifecycle)
	record.ID = id
	record.ProfileRevision.Source = coverageSource
	record.Subject.ContentChanges = changes
	record.CreatedAt = record.CreatedAt.Add(time.Duration(minute) * time.Minute)
	record.UpdatedAt = record.CreatedAt
	return record
}

func TestSaveAndLoadKeepContentChanges(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	record := coverageFixture("rp_1723200000000_00000000000000a1", model.LifecycleCompleted, coveredChanges, 0)
	saveTestReviews(t, ledger, record)

	loaded, err := ledger.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Subject.ContentChanges, coveredChanges) {
		t.Fatalf("loaded content changes = %#v, want %#v", loaded.Subject.ContentChanges, coveredChanges)
	}

	record.Subject.ContentChanges = coveredChanges[:1]
	saveTestReviews(t, ledger, record)
	stale, err := ledger.ContentTransitions(TransitionQuery{ProfileSource: coverageSource, Paths: []string{"b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("re-saved Review still carries its replaced edge: %#v", stale)
	}
}

func TestContentTransitionsListEveryEdgeOnTheQueriedPathsForTheProfile(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	extra := model.ContentChange{Path: "c.go", Before: model.ZeroObjectID, After: "4444444444444444444444444444444444444444"}
	fix := model.ContentChange{Path: "b.go", Before: coveredChanges[1].After, After: "5555555555555555555555555555555555555555"}
	otherProfile := coverageFixture("rp_1723200000000_00000000000000b3", model.LifecycleCompleted, coveredChanges, 3)
	otherProfile.ProfileRevision.Source = "global:profiles/bugs"
	saveTestReviews(t, ledger,
		coverageFixture("rp_1723200000000_00000000000000b1", model.LifecycleCompleted, coveredChanges, 1),
		coverageFixture("rp_1723200000000_00000000000000b2", model.LifecycleIncomplete, []model.ContentChange{coveredChanges[1], extra}, 2),
		otherProfile,
		coverageFixture("rp_1723200000000_00000000000000b4", model.LifecycleRunning, []model.ContentChange{fix}, 4),
		coverageFixture("rp_1723200000000_00000000000000b5", model.LifecycleCompleted, []model.ContentChange{extra}, 5),
	)

	transitions, err := ledger.ContentTransitions(TransitionQuery{ProfileSource: coverageSource, Paths: []string{"b.go", "a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []ContentTransition{
		{Review: "rp_1723200000000_00000000000000b1", Lifecycle: model.LifecycleCompleted, Path: "a.go", Before: coveredChanges[0].Before, After: coveredChanges[0].After},
		{Review: "rp_1723200000000_00000000000000b1", Lifecycle: model.LifecycleCompleted, Path: "b.go", Before: coveredChanges[1].Before, After: coveredChanges[1].After},
		{Review: "rp_1723200000000_00000000000000b2", Lifecycle: model.LifecycleIncomplete, Path: "b.go", Before: coveredChanges[1].Before, After: coveredChanges[1].After},
		{Review: "rp_1723200000000_00000000000000b4", Lifecycle: model.LifecycleRunning, Path: "b.go", Before: fix.Before, After: fix.After},
	}
	if got := withoutCreatedAt(transitions); !reflect.DeepEqual(got, want) {
		t.Fatalf("transitions = %#v, want %#v", got, want)
	}
	for index := 1; index < len(transitions); index++ {
		previous, current := transitions[index-1], transitions[index]
		if previous.Path == current.Path && current.CreatedAt.Before(previous.CreatedAt) {
			t.Fatalf("transitions on %s are not in creation order: %v after %v", current.Path, current.CreatedAt, previous.CreatedAt)
		}
	}
}

func TestContentTransitionsRequireAProfileAndPaths(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	for name, query := range map[string]TransitionQuery{
		"no profile": {Paths: []string{"a.go"}},
		"no paths":   {ProfileSource: coverageSource},
	} {
		if _, err := ledger.ContentTransitions(query); err == nil {
			t.Fatalf("%s: transition query succeeded", name)
		}
	}
}

func TestPrepareUpgradesSchemaTwelveLedgerToIndexContentChanges(t *testing.T) {
	directory := t.TempDir()
	review := writeLedgerAtSchema(t, directory, 12)

	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	completeTestMaintenance(t, directory)
	if version := readSchemaVersion(t, directory); version != currentLedgerSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentLedgerSchemaVersion)
	}
	ledger, err := openLedgerRecordStore(directory, false)
	if err != nil {
		t.Fatalf("open upgraded ledger = %v", err)
	}
	defer closeTestResource(t, ledger.Close)
	if _, err := ledger.Load(review.ID); err != nil {
		t.Fatalf("load preserved review = %v", err)
	}
	covered := coverageFixture("rp_1723200000000_00000000000000c1", model.LifecycleCompleted, coveredChanges, 1)
	saveTestReviews(t, ledger, covered)
	transitions, err := ledger.ContentTransitions(TransitionQuery{ProfileSource: coverageSource, Paths: []string{"a.go", "b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 2 || transitions[0].Review != covered.ID {
		t.Fatalf("transitions after upgrade = %#v", transitions)
	}
}

func withoutCreatedAt(transitions []ContentTransition) []ContentTransition {
	stripped := make([]ContentTransition, 0, len(transitions))
	for _, transition := range transitions {
		transition.CreatedAt = time.Time{}
		stripped = append(stripped, transition)
	}
	return stripped
}
