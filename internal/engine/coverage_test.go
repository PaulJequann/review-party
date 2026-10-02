package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
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

// coverageStore keeps the ledger contract: a Profile source's Reviews that
// share an entry with the query, newest first, each with its full set.
type coverageStore struct {
	reviews  []recordedCoverage
	waivers  []model.CheckpointWaiver
	queries  int
	findings map[model.ReviewID][]model.Finding
	verdicts []model.FindingVerdict
	loads    int
}

type recordedCoverage struct {
	source    string
	candidate store.CoverageCandidate
}

func recorded(source, id string, lifecycle model.Lifecycle, changes ...[]model.ContentChange) recordedCoverage {
	var set []model.ContentChange
	for _, part := range changes {
		set = append(set, part...)
	}
	return recordedCoverage{source: source, candidate: store.CoverageCandidate{ID: model.ReviewID(id), Lifecycle: lifecycle, Changes: set}}
}

func (*coverageStore) Save(model.ReviewRecord) error { return nil }

func (fake *coverageStore) Load(id model.ReviewID) (model.ReviewRecord, error) {
	fake.loads++
	findings, found := fake.findings[id]
	if !found {
		return model.ReviewRecord{}, errors.New("not found")
	}
	return model.ReviewRecord{ID: id, Result: &model.ReviewResult{Findings: findings}}, nil
}

func (fake *coverageStore) ListVerdicts(query store.VerdictQuery) ([]model.FindingVerdict, error) {
	var verdicts []model.FindingVerdict
	for _, verdict := range fake.verdicts {
		if slices.Contains(query.ReviewIDs, verdict.ReviewID) {
			verdicts = append(verdicts, verdict)
		}
	}
	return verdicts, nil
}

func (*coverageStore) RecordVerdicts(store.VerdictBatch) (store.VerdictTally, error) {
	return store.VerdictTally{}, nil
}

func (fake *coverageStore) CoverageCandidates(query store.CoverageQuery) ([]store.CoverageCandidate, error) {
	fake.queries++
	var candidates []store.CoverageCandidate
	for _, review := range fake.reviews {
		if review.source == query.ProfileSource && slices.ContainsFunc(review.candidate.Changes, func(change model.ContentChange) bool { return slices.Contains(query.Changes, change) }) {
			candidates = append(candidates, review.candidate)
		}
	}
	return candidates, nil
}

func (fake *coverageStore) CheckpointWaiver(key model.WaiverKey) (model.CheckpointWaiver, bool, error) {
	for index := len(fake.waivers) - 1; index >= 0; index-- {
		if fake.waivers[index].Key == key {
			return fake.waivers[index], true, nil
		}
	}
	return model.CheckpointWaiver{}, false, nil
}

func (fake *coverageStore) RecordCheckpointWaiver(waiver model.CheckpointWaiver) error {
	fake.waivers = append(fake.waivers, waiver)
	return nil
}

func (fake *coverageStore) CheckpointWaiversSince(repository string, since time.Time) ([]model.CheckpointWaiver, error) {
	waivers := []model.CheckpointWaiver{}
	for _, waiver := range slices.Backward(fake.waivers) {
		if waiver.Repository == repository && !waiver.CreatedAt.Before(since) {
			waivers = append(waivers, waiver)
		}
	}
	return waivers, nil
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
	fake := &coverageStore{reviews: []recordedCoverage{
		recorded("global:profiles/documentation", "rp_docs", model.LifecycleCompleted, coverageWhole),
		recorded("global:profiles/bugs", "rp_bugs", model.LifecycleRunning, coverageWhole),
		recorded("global:profiles/code-quality", "rp_partial", model.LifecycleCompleted, coverageFirst),
	}}
	selection := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: "documentation"}, {Profile: "bugs"}, {Profile: "code-quality"}}, Repository: []configuration.SelectionItem{}}
	conductor, repository := newCoverageConductor(t, fake, &selection)

	report, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: CoverageSubject{Changes: coverageWhole}})
	if err != nil {
		t.Fatal(err)
	}
	if report.State != CheckpointMissing {
		t.Fatalf("state = %s, want missing", report.State)
	}
	want := CoverageReport{Covered: false, Profiles: []ProfileCoverage{
		{Scope: configuration.ScopeGlobal, Profile: "documentation", State: CoverageCovered, ReviewIDs: []model.ReviewID{"rp_docs"}},
		{Scope: configuration.ScopeGlobal, Profile: "bugs", State: CoverageRunning, ReviewIDs: []model.ReviewID{"rp_bugs"}},
		{Scope: configuration.ScopeGlobal, Profile: "code-quality", State: CoverageMissing},
	}}
	if !reflect.DeepEqual(report.Coverage, want) {
		t.Fatalf("coverage = %#v, want %#v", report.Coverage, want)
	}
}

func TestCheckCoverageWithoutContentSkipsTheLedger(t *testing.T) {
	fake := &coverageStore{}
	selection := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: "bugs"}}, Repository: []configuration.SelectionItem{}}
	conductor, repository := newCoverageConductor(t, fake, &selection)

	report, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush})
	if err != nil {
		t.Fatal(err)
	}
	if report.State != CheckpointCovered {
		t.Fatalf("state = %s, want covered", report.State)
	}
	if fake.queries != 0 {
		t.Fatalf("empty content made %d ledger queries", fake.queries)
	}
}

func TestCheckCoverageWithoutSelectionNamesInit(t *testing.T) {
	conductor, repository := newCoverageConductor(t, &coverageStore{}, nil)
	_, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: CoverageSubject{Changes: coverageWhole}})
	if !errors.Is(err, configuration.ErrNoRepositorySelection) || !strings.Contains(err.Error(), "review-party init") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckCoverageRequiresTheLedger(t *testing.T) {
	selection := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: "bugs"}}, Repository: []configuration.SelectionItem{}}
	conductor, repository := newCoverageConductor(t, &failFinalRecordStore{}, &selection)
	_, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: CoverageSubject{Changes: coverageWhole}})
	if err == nil || !strings.Contains(err.Error(), "requires the SQLite ledger") {
		t.Fatalf("error = %v", err)
	}
}
