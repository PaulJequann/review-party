package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
)

var missTestTime = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func missTestRecord(id model.MissID, review model.ReviewID, offset time.Duration) MissRecord {
	return MissRecord{
		ID:       id,
		ReviewID: review,
		Report: model.MissReport{
			Location:    model.MissLocation{Path: "internal/a.go", Line: 12},
			Source:      model.MissSourceCodexPR,
			Description: "nil map write",
			RecordedBy:  "pj",
		},
		RecordedAt: missTestTime.Add(offset),
	}
}

func secondLedgerReview() model.ReviewRecord {
	review := ledgerFixture(model.LifecycleCompleted)
	review.ID = "rp_1723200000001_fedcba9876543210"
	review.Subject.Repository = "/other"
	review.Subject.Kind = model.SubjectCommittedRange
	review.Subject.Identity = "base..head"
	review.ProfileRevision.Name = "security"
	return review
}

func recordTestMisses(t *testing.T, ledger MissStore, records ...MissRecord) []model.Miss {
	t.Helper()
	misses, err := ledger.RecordMisses(records)
	if err != nil {
		t.Fatal(err)
	}
	return misses
}

func listTestMisses(t *testing.T, ledger MissStore, query MissQuery) []model.Miss {
	t.Helper()
	misses, err := ledger.ListMisses(query)
	if err != nil {
		t.Fatal(err)
	}
	return misses
}

func missIDs(misses []model.Miss) []model.MissID {
	ids := []model.MissID{}
	for _, miss := range misses {
		ids = append(ids, miss.ID)
	}
	return ids
}

func TestLedgerRecordsMissWithFactsReadFromTheReview(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := ledgerFixture(model.LifecycleCompleted)
	review.Subject.Patch = "PATCH-SENTINEL"
	saveTestReviews(t, ledger, review)

	recorded := recordTestMisses(t, ledger, missTestRecord("ms_1725192000000_0000000000000001", review.ID, 0))

	want := []model.Miss{{
		ID:              "ms_1725192000000_0000000000000001",
		ReviewID:        review.ID,
		Repository:      "/repo",
		SubjectKind:     model.SubjectWorkingChanges,
		SubjectIdentity: "subject",
		Profile:         "bugs",
		Location:        model.MissLocation{Path: "internal/a.go", Line: 12},
		Source:          model.MissSourceCodexPR,
		Description:     "nil map write",
		RecordedBy:      "pj",
		RecordedAt:      missTestTime,
	}}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("recorded = %#v, want %#v", recorded, want)
	}
	if listed := listTestMisses(t, ledger, MissQuery{}); !reflect.DeepEqual(listed, want) {
		t.Fatalf("listed = %#v, want %#v", listed, want)
	}
	encoded, err := json.Marshal(recorded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "PATCH-SENTINEL") {
		t.Fatalf("miss carries the subject patch: %s", encoded)
	}
	requireNoCopiedReviewFacts(t, missColumns(t, ledger))
}

func requireNoCopiedReviewFacts(t *testing.T, columns []string) {
	t.Helper()
	for _, column := range columns {
		for _, fact := range []string{"subject", "patch", "repository", "profile"} {
			if strings.Contains(column, fact) {
				t.Fatalf("misses table copies review fact in column %q", column)
			}
		}
	}
}

func missColumns(t *testing.T, ledger *LedgerRecordStore) []string {
	t.Helper()
	rows, err := ledger.db.Query("SELECT name FROM pragma_table_info('misses')")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close rows: %v", err)
		}
	}()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(columns) == 0 {
		t.Fatal("misses table has no columns")
	}
	return columns
}

func TestLedgerMissOnUnknownReviewWritesNothing(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := ledgerFixture(model.LifecycleCompleted)
	saveTestReviews(t, ledger, review)

	_, err := ledger.RecordMisses([]MissRecord{
		missTestRecord("ms_1725192000000_0000000000000001", review.ID, 0),
		missTestRecord("ms_1725192000000_0000000000000002", "rp_1723200000009_9999999999999999", 0),
	})
	if err == nil || !strings.Contains(err.Error(), `no review with id "rp_1723200000009_9999999999999999"`) {
		t.Fatalf("error = %v", err)
	}
	if listed := listTestMisses(t, ledger, MissQuery{IncludeRemoved: true}); len(listed) != 0 {
		t.Fatalf("listed = %v, want no misses after a failed record", missIDs(listed))
	}
}

func TestLedgerListsMissesByRepositoryProfileAndReview(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	first := ledgerFixture(model.LifecycleCompleted)
	second := secondLedgerReview()
	saveTestReviews(t, ledger, first, second)
	recordTestMisses(t, ledger,
		missTestRecord("ms_1725192000000_0000000000000003", second.ID, time.Minute),
		missTestRecord("ms_1725192000000_0000000000000002", first.ID, 0),
		missTestRecord("ms_1725192000000_0000000000000001", first.ID, time.Minute),
	)

	for name, test := range map[string]struct {
		query MissQuery
		want  []model.MissID
	}{
		"all in recorded order": {MissQuery{}, []model.MissID{"ms_1725192000000_0000000000000002", "ms_1725192000000_0000000000000001", "ms_1725192000000_0000000000000003"}},
		"repository":            {MissQuery{Repository: "/other"}, []model.MissID{"ms_1725192000000_0000000000000003"}},
		"profile":               {MissQuery{Profile: "bugs"}, []model.MissID{"ms_1725192000000_0000000000000002", "ms_1725192000000_0000000000000001"}},
		"review":                {MissQuery{ReviewID: second.ID}, []model.MissID{"ms_1725192000000_0000000000000003"}},
		"no match":              {MissQuery{Repository: "/repo", Profile: "security"}, []model.MissID{}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := missIDs(listTestMisses(t, ledger, test.query)); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ids = %v, want %v", got, test.want)
			}
		})
	}
	listed := listTestMisses(t, ledger, MissQuery{Repository: "/other"})
	want := [][3]string{{string(model.SubjectCommittedRange), "base..head", "security"}}
	if got := missReviewFacts(listed); !reflect.DeepEqual(got, want) {
		t.Fatalf("miss facts = %v, want the second review's subject and profile %v", got, want)
	}
}

func missReviewFacts(misses []model.Miss) [][3]string {
	facts := [][3]string{}
	for _, miss := range misses {
		facts = append(facts, [3]string{string(miss.SubjectKind), miss.SubjectIdentity, miss.Profile})
	}
	return facts
}

func removeTestMisses(t *testing.T, ledger MissStore, removal model.MissRemoval, ids ...model.MissID) []MissRemovalOutcome {
	t.Helper()
	outcomes, err := ledger.RemoveMisses(ids, removal)
	if err != nil {
		t.Fatal(err)
	}
	return outcomes
}

func missRemovals(misses []model.Miss) []*model.MissRemoval {
	removals := []*model.MissRemoval{}
	for _, miss := range misses {
		removals = append(removals, miss.Removal)
	}
	return removals
}

func TestLedgerMissRemovalKeepsTheFirstTombstone(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := ledgerFixture(model.LifecycleCompleted)
	saveTestReviews(t, ledger, review)
	recordTestMisses(t, ledger, missTestRecord("ms_1725192000000_0000000000000001", review.ID, 0))
	first := model.MissRemoval{Reason: "not a bug", RemovedBy: "pj", RemovedAt: missTestTime.Add(time.Hour)}
	second := model.MissRemoval{Reason: "again", RemovedBy: "other", RemovedAt: missTestTime.Add(2 * time.Hour)}

	outcomes := removeTestMisses(t, ledger, first, "ms_1725192000000_0000000000000001")
	if want := []MissRemovalOutcome{{ID: "ms_1725192000000_0000000000000001", Status: MissRemoved, Removal: first}}; !reflect.DeepEqual(outcomes, want) {
		t.Fatalf("outcomes = %#v, want %#v", outcomes, want)
	}
	if listed := listTestMisses(t, ledger, MissQuery{}); len(listed) != 0 {
		t.Fatalf("listed = %v, want removed miss hidden", missIDs(listed))
	}

	outcomes = removeTestMisses(t, ledger, second, "ms_1725192000000_0000000000000001")
	if want := []MissRemovalOutcome{{ID: "ms_1725192000000_0000000000000001", Status: MissAlreadyRemoved, Removal: first}}; !reflect.DeepEqual(outcomes, want) {
		t.Fatalf("second outcomes = %#v, want %#v", outcomes, want)
	}
	listed := listTestMisses(t, ledger, MissQuery{IncludeRemoved: true})
	if got := missRemovals(listed); !reflect.DeepEqual(got, []*model.MissRemoval{&first}) {
		t.Fatalf("removals = %v, want only the first tombstone %v", got, first)
	}
}

func TestLedgerMissRemovalWithAnUnknownIDChangesNothing(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := ledgerFixture(model.LifecycleCompleted)
	saveTestReviews(t, ledger, review)
	recordTestMisses(t, ledger,
		missTestRecord("ms_1725192000000_0000000000000001", review.ID, 0),
		missTestRecord("ms_1725192000000_0000000000000002", review.ID, time.Minute),
	)

	_, err := ledger.RemoveMisses([]model.MissID{"ms_1725192000000_0000000000000001", "ms_1725192000000_0000000000000009", "ms_1725192000000_0000000000000002"}, model.MissRemoval{Reason: "dup", RemovedBy: "pj", RemovedAt: missTestTime})
	if err == nil || !strings.Contains(err.Error(), `no miss with id "ms_1725192000000_0000000000000009"`) {
		t.Fatalf("error = %v", err)
	}
	want := []model.MissID{"ms_1725192000000_0000000000000001", "ms_1725192000000_0000000000000002"}
	if got := missIDs(listTestMisses(t, ledger, MissQuery{})); !reflect.DeepEqual(got, want) {
		t.Fatalf("active misses = %v, want %v", got, want)
	}
}

func writeSchemaTenLedger(t *testing.T, directory string) model.ReviewRecord {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(directory, ledgerFilename))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, db.Close)
	initial, err := migrationFiles.ReadFile("migrations/initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY); INSERT INTO schema_migrations(version) VALUES(10)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(initial)); err != nil {
		t.Fatal(err)
	}
	review := ledgerFixture(model.LifecycleCompleted)
	if err := (reviewRecordProjection{db: db}).save(review); err != nil {
		t.Fatal(err)
	}
	return review
}

func TestPrepareUpgradesSchemaTenLedgerPreservingReviews(t *testing.T) {
	directory := t.TempDir()
	review := writeSchemaTenLedger(t, directory)

	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	if version := readSchemaVersion(t, directory); version != currentLedgerSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentLedgerSchemaVersion)
	}
	ledger, err := openLedgerRecordStore(directory, false)
	if err != nil {
		t.Fatalf("open upgraded ledger without preparation = %v", err)
	}
	defer closeTestResource(t, ledger.Close)
	loaded, err := ledger.Load(review.ID)
	if err != nil {
		t.Fatalf("load preserved review = %v", err)
	}
	if !reflect.DeepEqual(loaded, review) {
		t.Fatalf("loaded = %#v, want %#v", loaded, review)
	}
	misses := recordTestMisses(t, ledger, missTestRecord("ms_1725192000000_0000000000000001", review.ID, 0))
	if len(misses) != 1 || misses[0].Profile != "bugs" {
		t.Fatalf("misses = %#v", misses)
	}
}

func TestSchemaTenLedgerRequiresPreparationWithoutUpgrading(t *testing.T) {
	directory := t.TempDir()
	writeSchemaTenLedger(t, directory)

	deferred, err := NewDeferredLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	_, err = deferred.ListMisses(MissQuery{})
	if !errors.Is(err, ErrReviewRecordStateRequiresPreparation) || !strings.Contains(err.Error(), "run review-party init") {
		t.Fatalf("error = %v, want a preparation error naming review-party init", err)
	}
	if version := readSchemaVersion(t, directory); version != 10 {
		t.Fatalf("schema version = %d, want unchanged 10", version)
	}
}
