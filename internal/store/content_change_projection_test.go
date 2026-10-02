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
	stale, err := ledger.ContentChangeCoverage(CoverageQuery{ProfileSource: coverageSource, Changes: coveredChanges})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("re-saved Review still matches its replaced set: %#v", stale)
	}
}

func TestContentChangeCoverageMatchesExactSetForProfile(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	superset := append(append([]model.ContentChange{}, coveredChanges...), model.ContentChange{Path: "c.go", Before: model.ZeroObjectID, After: "4444444444444444444444444444444444444444"})
	differentAfter := []model.ContentChange{coveredChanges[0], {Path: "b.go", Before: coveredChanges[1].Before, After: "5555555555555555555555555555555555555555"}}
	otherProfile := coverageFixture("rp_1723200000000_00000000000000b3", model.LifecycleCompleted, coveredChanges, 3)
	otherProfile.ProfileRevision.Source = "global:profiles/bugs"
	saveTestReviews(t, ledger,
		coverageFixture("rp_1723200000000_00000000000000b1", model.LifecycleCompleted, coveredChanges, 1),
		coverageFixture("rp_1723200000000_00000000000000b2", model.LifecycleIncomplete, coveredChanges, 2),
		otherProfile,
		coverageFixture("rp_1723200000000_00000000000000b4", model.LifecycleCompleted, superset, 4),
		coverageFixture("rp_1723200000000_00000000000000b5", model.LifecycleCompleted, coveredChanges[:1], 5),
		coverageFixture("rp_1723200000000_00000000000000b6", model.LifecycleCompleted, differentAfter, 6),
	)

	candidates, err := ledger.ContentChangeCoverage(CoverageQuery{ProfileSource: coverageSource, Changes: coveredChanges})
	if err != nil {
		t.Fatal(err)
	}
	want := []CoverageCandidate{
		{ID: "rp_1723200000000_00000000000000b2", Lifecycle: model.LifecycleIncomplete},
		{ID: "rp_1723200000000_00000000000000b1", Lifecycle: model.LifecycleCompleted},
	}
	if got := withoutCreatedAt(candidates); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestContentChangeCoverageRejectsEmptyAndRepeatedSets(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	for name, changes := range map[string][]model.ContentChange{
		"empty":    nil,
		"repeated": {coveredChanges[0], coveredChanges[0]},
	} {
		if _, err := ledger.ContentChangeCoverage(CoverageQuery{ProfileSource: coverageSource, Changes: changes}); err == nil {
			t.Fatalf("%s set: coverage query succeeded", name)
		}
	}
}

func TestPrepareUpgradesSchemaTwelveLedgerToIndexContentChanges(t *testing.T) {
	directory := t.TempDir()
	review := writeLedgerAtSchema(t, directory, 12)

	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatalf("prepare = %v", err)
	}
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
	candidates, err := ledger.ContentChangeCoverage(CoverageQuery{ProfileSource: coverageSource, Changes: coveredChanges})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != covered.ID {
		t.Fatalf("candidates after upgrade = %#v", candidates)
	}
}

func withoutCreatedAt(candidates []CoverageCandidate) []CoverageCandidate {
	stripped := make([]CoverageCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.CreatedAt = time.Time{}
		stripped = append(stripped, candidate)
	}
	return stripped
}
