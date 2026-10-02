package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"reviewparty/internal/model"
)

var verdictTestTime = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

func judgment(t *testing.T, ordinal int, verdict model.Verdict, reason string) model.Judgment {
	t.Helper()
	parsed, err := model.ParseVerdictReason(reason)
	if err != nil {
		t.Fatal(err)
	}
	return model.Judgment{Ordinal: ordinal, Verdict: verdict, Reason: parsed}
}

func recordTestVerdicts(t *testing.T, ledger VerdictStore, review model.ReviewID, judgments ...model.Judgment) VerdictTally {
	t.Helper()
	tally, err := ledger.RecordVerdicts(VerdictBatch{ReviewID: review, Judgments: judgments, RecordedBy: "pj", RecordedAt: verdictTestTime})
	if err != nil {
		t.Fatal(err)
	}
	return tally
}

func listTestVerdicts(t *testing.T, ledger VerdictStore, query VerdictQuery) []model.FindingVerdict {
	t.Helper()
	verdicts, err := ledger.ListVerdicts(query)
	if err != nil {
		t.Fatal(err)
	}
	return verdicts
}

// onlyTestVerdict lists every Verdict and requires exactly one.
func onlyTestVerdict(t *testing.T, ledger VerdictStore) model.FindingVerdict {
	t.Helper()
	verdicts := listTestVerdicts(t, ledger, VerdictQuery{})
	if len(verdicts) != 1 {
		t.Fatalf("verdicts = %#v, want one", verdicts)
	}
	return verdicts[0]
}

func countVerdictRows(t *testing.T, ledger *LedgerRecordStore) int {
	t.Helper()
	var count int
	if err := ledger.db.QueryRow("SELECT COUNT(*) FROM finding_verdicts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func threeFindingReview() model.ReviewRecord {
	review := ledgerFixture(model.LifecycleCompleted)
	first := review.Result.Findings[0]
	for ordinal := 2; ordinal <= 3; ordinal++ {
		next := first
		next.Ordinal = ordinal
		next.Failure = first.Failure + string(rune('0'+ordinal))
		review.Result.Findings = append(review.Result.Findings, next)
	}
	return review
}

func TestLedgerVerdictRepeatWritesNothingAndADifferentVerdictSupersedes(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := ledgerFixture(model.LifecycleCompleted)
	saveTestReviews(t, ledger, review)

	steps := []struct {
		judgment model.Judgment
		want     VerdictTally
	}{
		{judgment(t, 1, model.VerdictAccepted, "real nil deref"), VerdictTally{Recorded: 1}},
		{judgment(t, 1, model.VerdictAccepted, "  real nil deref "), VerdictTally{Unchanged: 1}},
		{judgment(t, 1, model.VerdictRejected, "guarded upstream"), VerdictTally{Recorded: 1, Changed: 1}},
	}
	for _, step := range steps {
		if tally := recordTestVerdicts(t, ledger, review.ID, step.judgment); tally != step.want {
			t.Fatalf("tally = %+v, want %+v", tally, step.want)
		}
	}

	want := []model.FindingVerdict{{
		ReviewID: review.ID, Repository: "/repo", Profile: "bugs", Ordinal: 1, Location: "a.go:1",
		Verdict: model.VerdictRejected, Reason: "guarded upstream", RecordedBy: "pj", RecordedAt: verdictTestTime,
	}}
	if got := listTestVerdicts(t, ledger, VerdictQuery{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("verdicts = %#v, want %#v", got, want)
	}
	if rows := countVerdictRows(t, ledger); rows != 2 {
		t.Fatalf("ledger rows = %d, want 2: the repeat must not append", rows)
	}
}

func TestLedgerVerdictBatchNamingAMissingFindingWritesNothing(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := threeFindingReview()
	saveTestReviews(t, ledger, review)

	_, err := ledger.RecordVerdicts(VerdictBatch{
		ReviewID:   review.ID,
		Judgments:  []model.Judgment{judgment(t, 1, model.VerdictAccepted, "real"), judgment(t, 4, model.VerdictRejected, "not real")},
		RecordedBy: "pj", RecordedAt: verdictTestTime,
	})

	var missing MissingFindingError
	if !errors.As(err, &missing) || err.Error() != "review rp_1723200000000_0123456789abcdef (completed) has findings 1-3, not 4" {
		t.Fatalf("error = %v, want a missing-finding error", err)
	}
	if rows := countVerdictRows(t, ledger); rows != 0 {
		t.Fatalf("ledger rows = %d, want 0 after a rejected batch", rows)
	}
}

func TestLedgerVerdictOnUnknownReviewNamesIt(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)

	_, err := ledger.RecordVerdicts(VerdictBatch{ReviewID: "rp_1723200000000_ffffffffffffffff", Judgments: []model.Judgment{judgment(t, 1, model.VerdictAccepted, "real")}, RecordedBy: "pj", RecordedAt: verdictTestTime})

	if err == nil || err.Error() != `no review with id "rp_1723200000000_ffffffffffffffff"` {
		t.Fatalf("error = %v, want the unknown review named", err)
	}
}

func TestLedgerVerdictAcceptsIncompleteReviewFindingsOnly(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	partial := ledgerFixture(model.LifecycleCompleted)
	partial.Lifecycle = model.LifecycleIncomplete
	empty := secondLedgerReview()
	empty.Lifecycle = model.LifecycleIncomplete
	empty.Result = nil
	saveTestReviews(t, ledger, partial, empty)

	if tally := recordTestVerdicts(t, ledger, partial.ID, judgment(t, 1, model.VerdictDeferred, "needs repro")); tally != (VerdictTally{Recorded: 1}) {
		t.Fatalf("tally = %+v, want one recorded", tally)
	}
	_, err := ledger.RecordVerdicts(VerdictBatch{ReviewID: empty.ID, Judgments: []model.Judgment{judgment(t, 1, model.VerdictDeferred, "needs repro")}, RecordedBy: "pj", RecordedAt: verdictTestTime})
	if err == nil || err.Error() != "review rp_1723200000001_fedcba9876543210 (incomplete) has no findings" {
		t.Fatalf("error = %v, want the review without findings named", err)
	}
}

func TestLedgerVerdictSurvivesResavingTheSameFindings(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := ledgerFixture(model.LifecycleCompleted)
	saveTestReviews(t, ledger, review)
	recordTestVerdicts(t, ledger, review.ID, judgment(t, 1, model.VerdictAccepted, "real"))

	saveTestReviews(t, ledger, review)

	if got := onlyTestVerdict(t, ledger); got.Stale || got.Verdict != model.VerdictAccepted {
		t.Fatalf("verdict = %#v, want the accepted verdict current", got)
	}
	if tally := recordTestVerdicts(t, ledger, review.ID, judgment(t, 1, model.VerdictAccepted, "real")); tally != (VerdictTally{Unchanged: 1}) {
		t.Fatalf("repeat tally = %+v, want unchanged", tally)
	}
}

func TestLedgerVerdictGoesStaleWhenTheFindingTextIsReplaced(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := ledgerFixture(model.LifecycleCompleted)
	saveTestReviews(t, ledger, review)
	recordTestVerdicts(t, ledger, review.ID, judgment(t, 1, model.VerdictAccepted, "real"))

	review.Result.Findings[0].Evidence = "different evidence"
	review.Result.Findings[0].Location = "b.go:9"
	saveTestReviews(t, ledger, review)

	if got := onlyTestVerdict(t, ledger); !got.Stale || got.Location != "b.go:9" {
		t.Fatalf("verdict = %#v, want a stale verdict at the live location", got)
	}
	if tally := recordTestVerdicts(t, ledger, review.ID, judgment(t, 1, model.VerdictAccepted, "real")); tally != (VerdictTally{Recorded: 1}) {
		t.Fatalf("tally = %+v, want a fresh verdict that supersedes nothing current", tally)
	}
	if got := onlyTestVerdict(t, ledger); got.Stale {
		t.Fatalf("verdict = %#v, want the fresh verdict current", got)
	}
}

func TestLedgerVerdictOnRestoredTextIsCurrentAgain(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	review := ledgerFixture(model.LifecycleCompleted)
	original := review.Result.Findings[0]
	saveTestReviews(t, ledger, review)
	recordTestVerdicts(t, ledger, review.ID, judgment(t, 1, model.VerdictAccepted, "real"))
	review.Result.Findings[0].Failure = "other failure"
	saveTestReviews(t, ledger, review)
	recordTestVerdicts(t, ledger, review.ID, judgment(t, 1, model.VerdictRejected, "other text is wrong"))

	review.Result.Findings[0] = original
	saveTestReviews(t, ledger, review)

	if got := onlyTestVerdict(t, ledger); got.Stale || got.Verdict != model.VerdictAccepted {
		t.Fatalf("verdict = %#v, want the verdict on the restored text current", got)
	}
}

func TestLedgerListsVerdictsByRepositoryProfileAndReviews(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	first, second := threeFindingReview(), secondLedgerReview()
	saveTestReviews(t, ledger, first, second)
	recordTestVerdicts(t, ledger, first.ID, judgment(t, 3, model.VerdictRejected, "style"), judgment(t, 1, model.VerdictAccepted, "real"))
	recordTestVerdicts(t, ledger, second.ID, judgment(t, 1, model.VerdictDeferred, "later"))

	type key struct {
		review  model.ReviewID
		ordinal int
	}
	firstOne, firstThree, secondOne := key{first.ID, 1}, key{first.ID, 3}, key{second.ID, 1}
	cases := map[string]struct {
		query VerdictQuery
		want  []key
	}{
		"all":        {VerdictQuery{}, []key{firstOne, firstThree, secondOne}},
		"repository": {VerdictQuery{Repository: "/other"}, []key{secondOne}},
		"profile":    {VerdictQuery{Profile: "bugs"}, []key{firstOne, firstThree}},
		"reviews":    {VerdictQuery{ReviewIDs: []model.ReviewID{second.ID, "rp_1723200000000_ffffffffffffffff"}}, []key{secondOne}},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			got := []key{}
			for _, verdict := range listTestVerdicts(t, ledger, test.query) {
				got = append(got, key{verdict.ReviewID, verdict.Ordinal})
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("verdicts = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPrepareUpgradesSchemaElevenLedgerToTakeVerdicts(t *testing.T) {
	directory := t.TempDir()
	review := writeLedgerAtSchema(t, directory, 11)

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
	if loaded, err := ledger.Load(review.ID); err != nil || !reflect.DeepEqual(loaded, review) {
		t.Fatalf("loaded = %#v, %v; want the preserved review", loaded, err)
	}
	if tally := recordTestVerdicts(t, ledger, review.ID, judgment(t, 1, model.VerdictAccepted, "real")); tally != (VerdictTally{Recorded: 1}) {
		t.Fatalf("tally = %+v, want one recorded", tally)
	}
}
