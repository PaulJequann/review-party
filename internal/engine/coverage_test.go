package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

var (
	coverageFirst  = []model.ContentChange{{Path: "a.go", Before: model.ZeroObjectID, After: "1111111111111111111111111111111111111111"}}
	coverageSecond = []model.ContentChange{{Path: "b.go", Before: model.ZeroObjectID, After: "2222222222222222222222222222222222222222"}}
	coverageWhole  = append(append([]model.ContentChange{}, coverageFirst...), coverageSecond...)
	twoCommits     = CoverageSubject{Changes: coverageWhole, Commits: []subject.CommitContentChanges{{Commit: "c1", Changes: coverageFirst}, {Commit: "c2", Changes: coverageSecond}}}
)

func candidate(id string, lifecycle model.Lifecycle) store.CoverageCandidate {
	return store.CoverageCandidate{ID: model.ReviewID(id), Lifecycle: lifecycle}
}

func changeSetKey(changes []model.ContentChange) string {
	return fmt.Sprint(changes)
}

func TestDecideCoverage(t *testing.T) {
	whole, first, second := changeSetKey(coverageWhole), changeSetKey(coverageFirst), changeSetKey(coverageSecond)
	for _, test := range []struct {
		name       string
		subject    CoverageSubject
		candidates map[string][]store.CoverageCandidate
		state      CoverageState
		ids        []model.ReviewID
	}{
		{name: "empty content is covered", subject: CoverageSubject{}, state: CoverageCovered},
		{name: "completed whole set covers", subject: twoCommits, candidates: map[string][]store.CoverageCandidate{
			whole: {candidate("rp_running", model.LifecycleRunning), candidate("rp_done", model.LifecycleCompleted), candidate("rp_older", model.LifecycleCompleted)},
		}, state: CoverageCovered, ids: []model.ReviewID{"rp_done"}},
		{name: "completed commits cover", subject: twoCommits, candidates: map[string][]store.CoverageCandidate{
			first: {candidate("rp_first", model.LifecycleCompleted)}, second: {candidate("rp_second", model.LifecycleCompleted)},
		}, state: CoverageCovered, ids: []model.ReviewID{"rp_first", "rp_second"}},
		{name: "incomplete never covers", subject: twoCommits, candidates: map[string][]store.CoverageCandidate{
			whole: {candidate("rp_broken", model.LifecycleIncomplete)}, first: {candidate("rp_first", model.LifecycleCompleted)}, second: {candidate("rp_broken_second", model.LifecycleIncomplete)},
		}, state: CoverageMissing},
		{name: "in-flight whole set runs", subject: twoCommits, candidates: map[string][]store.CoverageCandidate{
			whole: {candidate("rp_pending", model.LifecyclePending)}, first: {candidate("rp_first", model.LifecycleCompleted)},
		}, state: CoverageRunning, ids: []model.ReviewID{"rp_pending"}},
		{name: "in-flight commit with the rest completed runs", subject: twoCommits, candidates: map[string][]store.CoverageCandidate{
			first: {candidate("rp_first", model.LifecycleCompleted)}, second: {candidate("rp_second", model.LifecycleRunning)},
		}, state: CoverageRunning, ids: []model.ReviewID{"rp_second"}},
		{name: "in-flight commit with another commit missing is missing", subject: twoCommits, candidates: map[string][]store.CoverageCandidate{
			second: {candidate("rp_second", model.LifecycleRunning)},
		}, state: CoverageMissing},
		{name: "a range without commits needs the whole set", subject: CoverageSubject{Changes: coverageWhole}, state: CoverageMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			lookup := func(changes []model.ContentChange) ([]store.CoverageCandidate, error) {
				return test.candidates[changeSetKey(changes)], nil
			}
			state, ids, err := decideCoverage(lookup, test.subject)
			if err != nil {
				t.Fatal(err)
			}
			if state != test.state || !reflect.DeepEqual(ids, test.ids) {
				t.Fatalf("decision = %s %v, want %s %v", state, ids, test.state, test.ids)
			}
		})
	}
}

type coverageStore struct {
	candidates map[string][]store.CoverageCandidate
	queries    int
}

func (*coverageStore) Save(model.ReviewRecord) error { return nil }

func (*coverageStore) Load(model.ReviewID) (model.ReviewRecord, error) {
	return model.ReviewRecord{}, errors.New("not found")
}

func (fake *coverageStore) ContentChangeCoverage(query store.CoverageQuery) ([]store.CoverageCandidate, error) {
	fake.queries++
	return fake.candidates[query.ProfileSource+" "+changeSetKey(query.Changes)], nil
}

func newCoverageConductor(t *testing.T, recordStore store.RecordStore, selection *configuration.ReviewSelection) (*Conductor, string) {
	t.Helper()
	repository := t.TempDir()
	if selection != nil {
		writeRepositorySelection(t, repository, *selection)
	}
	conductor, err := newConductorWithManager(recordStore, defaultReviewerCatalog(), newTestConfigurationManager(t), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return conductor, repository
}

func TestCheckCoverageReportsSelectedProfilesInOrder(t *testing.T) {
	fake := &coverageStore{candidates: map[string][]store.CoverageCandidate{
		"global:profiles/documentation " + changeSetKey(coverageWhole): {candidate("rp_docs", model.LifecycleCompleted)},
		"global:profiles/bugs " + changeSetKey(coverageWhole):          {candidate("rp_bugs", model.LifecycleRunning)},
	}}
	selection := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: "documentation"}, {Profile: "bugs"}, {Profile: "code-quality"}}, Repository: []configuration.SelectionItem{}}
	conductor, repository := newCoverageConductor(t, fake, &selection)

	report, err := conductor.CheckCoverage(context.Background(), repository, CoverageSubject{Changes: coverageWhole})
	if err != nil {
		t.Fatal(err)
	}
	want := CoverageReport{Covered: false, Profiles: []ProfileCoverage{
		{Scope: configuration.ScopeGlobal, Profile: "documentation", State: CoverageCovered, ReviewIDs: []model.ReviewID{"rp_docs"}},
		{Scope: configuration.ScopeGlobal, Profile: "bugs", State: CoverageRunning, ReviewIDs: []model.ReviewID{"rp_bugs"}},
		{Scope: configuration.ScopeGlobal, Profile: "code-quality", State: CoverageMissing},
	}}
	if !reflect.DeepEqual(report, want) {
		t.Fatalf("report = %#v, want %#v", report, want)
	}
}

func TestCheckCoverageWithoutContentSkipsTheLedger(t *testing.T) {
	fake := &coverageStore{}
	selection := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: "bugs"}}, Repository: []configuration.SelectionItem{}}
	conductor, repository := newCoverageConductor(t, fake, &selection)

	report, err := conductor.CheckCoverage(context.Background(), repository, CoverageSubject{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Covered || fake.queries != 0 {
		t.Fatalf("report = %#v after %d ledger queries", report, fake.queries)
	}
}

func TestCheckCoverageWithoutSelectionNamesInit(t *testing.T) {
	conductor, repository := newCoverageConductor(t, &coverageStore{}, nil)
	_, err := conductor.CheckCoverage(context.Background(), repository, CoverageSubject{Changes: coverageWhole})
	if !errors.Is(err, configuration.ErrNoRepositorySelection) || !strings.Contains(err.Error(), "review-party init") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckCoverageRequiresTheLedger(t *testing.T) {
	selection := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: "bugs"}}, Repository: []configuration.SelectionItem{}}
	conductor, repository := newCoverageConductor(t, &failFinalRecordStore{}, &selection)
	_, err := conductor.CheckCoverage(context.Background(), repository, CoverageSubject{Changes: coverageWhole})
	if err == nil || !strings.Contains(err.Error(), "requires the SQLite ledger") {
		t.Fatalf("error = %v", err)
	}
}
