package engine

import (
	"context"
	"errors"
	"iter"
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
)

// coverageStore keeps the ledger contract: each Profile source's Review edges
// on the queried paths, grouped by path, oldest Review first. It also stands
// in for the repository: the lines between two blobs, 100 unless the test
// says otherwise, and the paths whose delta is binary.
type coverageStore struct {
	reviews  []recordedCoverage
	waivers  []model.CheckpointWaiver
	queries  int
	findings map[model.ReviewID][]model.Finding
	verdicts []model.FindingVerdict
	loads    int
	lines    map[model.ContentChange]int
	binary   map[string]bool
	absent   map[string]bool
}

type recordedCoverage struct {
	source    string
	id        model.ReviewID
	lifecycle model.Lifecycle
	changes   []model.ContentChange
}

func recorded(source, id string, lifecycle model.Lifecycle, changes ...[]model.ContentChange) recordedCoverage {
	var set []model.ContentChange
	for _, part := range changes {
		set = append(set, part...)
	}
	return recordedCoverage{source: source, id: model.ReviewID(id), lifecycle: lifecycle, changes: set}
}

func (*coverageStore) Save(model.ReviewRecord) error { return nil }

func (*coverageStore) ExpireEvidence(int, func([]model.ArtifactReference) error) error { return nil }

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

// ContentTransitions dates each Review by its position: a later entry is a
// newer Review.
func (fake *coverageStore) ContentTransitions(query store.TransitionQuery) ([]store.ContentTransition, error) {
	fake.queries++
	edges := []store.ContentTransition{}
	for index, review := range fake.reviews {
		if review.source != query.ProfileSource {
			continue
		}
		for _, change := range review.changes {
			if slices.Contains(query.Paths, change.Path) {
				edges = append(edges, store.ContentTransition{Review: review.id, Lifecycle: review.lifecycle, CreatedAt: time.Unix(int64(1700000000+index), 0), Path: change.Path, Before: change.Before, After: change.After})
			}
		}
	}
	slices.SortStableFunc(edges, func(a, b store.ContentTransition) int { return strings.Compare(a.Path, b.Path) })
	return edges, nil
}

func (fake *coverageStore) measure(_ string, delta []model.ContentChange) (subject.DeltaLines, error) {
	lines := subject.DeltaLines{ByPath: map[string]int{}}
	for _, change := range delta {
		if fake.binary[change.Path] {
			lines.Binary = append(lines.Binary, change.Path)
			continue
		}
		count, known := fake.lines[change]
		if !known {
			count = 100
		}
		lines.ByPath[change.Path] = count
		lines.Total += count
	}
	return lines, nil
}

func (fake *coverageStore) missingObjects(_ string, objects iter.Seq[string]) (map[string]bool, error) {
	missing := map[string]bool{}
	for object := range objects {
		if fake.absent[object] {
			missing[object] = true
		}
	}
	return missing, nil
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
	if fake, ok := recordStore.(*coverageStore); ok {
		conductor.measureDelta = fake.measure
		conductor.missingObjects = fake.missingObjects
	}
	return conductor, repository
}

func globalSelection(profiles ...string) *configuration.ReviewSelection {
	selection := configuration.ReviewSelection{ConcurrencyLimit: 1, Repository: []configuration.SelectionItem{}}
	for _, profile := range profiles {
		selection.Global = append(selection.Global, configuration.SelectionItem{Profile: profile})
	}
	return &selection
}

func TestCheckCoverageReportsSelectedProfilesInOrder(t *testing.T) {
	fake := &coverageStore{reviews: []recordedCoverage{
		recorded("global:profiles/documentation", "rp_docs", model.LifecycleCompleted, coverageWhole),
		recorded("global:profiles/bugs", "rp_bugs", model.LifecycleRunning, coverageWhole),
		recorded("global:profiles/code-quality", "rp_partial", model.LifecycleCompleted, coverageFirst),
	}}
	conductor, repository := newCoverageConductor(t, fake, globalSelection("documentation", "bugs", "code-quality"))

	report, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: coverageWhole})
	if err != nil {
		t.Fatal(err)
	}
	if report.State != CheckpointRunning {
		t.Fatalf("state = %s, want running", report.State)
	}
	none := subject.DeltaLines{ByPath: map[string]int{}}
	want := CoverageReport{
		Profiles: []ProfileCoverage{
			{Scope: configuration.ScopeGlobal, Profile: "documentation", State: CoverageCovered, Reviews: []model.ReviewID{"rp_docs"}, Spent: 1, UnreviewedLines: none},
			{Scope: configuration.ScopeGlobal, Profile: "bugs", State: CoverageRunning, Spent: 1, Running: []model.ReviewID{"rp_bugs"}, Unreviewed: coverageWhole, UnreviewedLines: subject.DeltaLines{ByPath: map[string]int{"a.go": 100, "b.go": 100}, Total: 200}},
			{Scope: configuration.ScopeGlobal, Profile: "code-quality", State: CoverageMissing, Reviews: []model.ReviewID{"rp_partial"}, Spent: 1, Unreviewed: coverageSecond, UnreviewedLines: subject.DeltaLines{ByPath: map[string]int{"b.go": 100}, Total: 100}},
		},
		Unreviewed:      coverageWhole,
		UnreviewedLines: subject.DeltaLines{ByPath: map[string]int{"a.go": 100, "b.go": 100}, Total: 200},
	}
	if !reflect.DeepEqual(report.Coverage, want) {
		t.Fatalf("coverage = %#v, want %#v", report.Coverage, want)
	}
}

func TestCommonDeltaStartsFromTheNewestStateEveryUnreviewedProfileReached(t *testing.T) {
	chain := func(source string, states ...string) []recordedCoverage {
		var reviews []recordedCoverage
		for index := 1; index < len(states); index++ {
			reviews = append(reviews, recorded(source, source+states[index][:5], model.LifecycleCompleted, []model.ContentChange{{Path: "a.go", Before: states[index-1], After: states[index]}}))
		}
		return reviews
	}
	fake := &coverageStore{reviews: slices.Concat(
		chain("global:profiles/documentation", model.ZeroObjectID, a1, a2),
		chain("global:profiles/bugs", model.ZeroObjectID, a1),
		chain("global:profiles/code-quality", model.ZeroObjectID, a1, a2, a3),
	)}
	conductor, repository := newCoverageConductor(t, fake, globalSelection("documentation", "bugs", "code-quality"))

	report, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: []model.ContentChange{{Path: "a.go", Before: model.ZeroObjectID, After: a3}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []model.ContentChange{{Path: "a.go", Before: a1, After: a3}}; !reflect.DeepEqual(report.Coverage.Unreviewed, want) {
		t.Fatalf("common delta = %v, want %v", report.Coverage.Unreviewed, want)
	}
}

func TestCheckCoverageWithoutContentSkipsTheLedger(t *testing.T) {
	fake := &coverageStore{}
	conductor, repository := newCoverageConductor(t, fake, globalSelection("bugs"))

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
	_, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: coverageWhole})
	if !errors.Is(err, configuration.ErrNoRepositorySelection) || !strings.Contains(err.Error(), "review-party init") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckCoverageRequiresTheLedger(t *testing.T) {
	conductor, repository := newCoverageConductor(t, &failFinalRecordStore{}, globalSelection("bugs"))
	_, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: coverageWhole})
	if err == nil || !strings.Contains(err.Error(), "requires the SQLite ledger") {
		t.Fatalf("error = %v", err)
	}
}
