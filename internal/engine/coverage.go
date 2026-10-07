package engine

// Coverage walks, per Profile and per path, the reviewed states a Checkpoint
// can reach from the base blob over completed Reviews, and measures what is
// left between the newest reviewed state and the current content. It reports
// facts; the Caller decides what an unreviewed Checkpoint blocks.

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

type CoverageState string

const (
	CoverageCovered  CoverageState = "covered"
	CoverageResidual CoverageState = "residual"
	CoverageUnjudged CoverageState = "unjudged"
	CoverageRunning  CoverageState = "running"
	CoverageSpent    CoverageState = "spent"
	CoverageMissing  CoverageState = "missing"
)

// ProfileCoverage is one Profile's reviewed state of the content. Reviews is
// the chain evidence from the base to the newest reviewed state of each path.
// Spent counts the distinct Reviews of any lifecycle that started from a
// reviewed state, so an incomplete Review spends budget and credits nothing.
// Unreviewed is the delta from the reviewed state to the current content.
type ProfileCoverage struct {
	Scope           configuration.Scope
	Profile         string
	State           CoverageState
	Reviews         []model.ReviewID
	Spent           int
	Running         []model.ReviewID
	Unreviewed      []model.ContentChange
	UnreviewedLines subject.DeltaLines
	Unjudged        []UnjudgedReview
}

// CoverageReport lists the selected Profiles in selection order. Unreviewed
// is the delta a run --unreviewed would review for every Profile with
// something unreviewed: from the newest state each of them reached.
type CoverageReport struct {
	Profiles        []ProfileCoverage
	Unreviewed      []model.ContentChange
	UnreviewedLines subject.DeltaLines
}

type transitionLedger interface {
	ContentTransitions(store.TransitionQuery) ([]store.ContentTransition, error)
}

// coverageCheck is one Checkpoint check's measurement of one content set
// against a declaration. Delta sizes are measured once per distinct delta.
type coverageCheck struct {
	conductor   *Conductor
	ledger      transitionLedger
	repository  string
	content     []model.ContentChange
	declaration configuration.Checkpoint
	judge       *findingJudge
	measured    map[string]subject.DeltaLines
}

// checkCoverage reports, per selected Profile in selection order, what that
// Profile has reviewed of the content and what remains.
func (conductor *Conductor) checkCoverage(repository string, content []model.ContentChange, declaration configuration.Checkpoint, judge *findingJudge) (CoverageReport, error) {
	resolved, err := conductor.configuration.ResolveRun(configuration.RunRequest{Repository: configuration.Repository(repository)})
	if errors.Is(err, configuration.ErrNoRepositorySelection) {
		return CoverageReport{}, fmt.Errorf("%w; run review-party init to choose the Reviews this repository runs", err)
	}
	if err != nil {
		return CoverageReport{}, err
	}
	ledger, ok := conductor.store.(transitionLedger)
	if !ok {
		return CoverageReport{}, errors.New("coverage check requires the SQLite ledger")
	}
	check := coverageCheck{conductor: conductor, ledger: ledger, repository: repository, content: content, declaration: declaration, judge: judge, measured: map[string]subject.DeltaLines{}}
	report := CoverageReport{}
	reaches := make([]profileReach, 0, len(resolved.Expanded))
	for _, profile := range resolved.Expanded {
		entry, reach, err := check.profile(profile.Scope, profile.Profile)
		if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
			return CoverageReport{}, InitializationRequiredError{Repository: repository}
		}
		if err != nil {
			return CoverageReport{}, fmt.Errorf("check coverage for Profile %q: %w", profile.Profile, err)
		}
		report.Profiles = append(report.Profiles, entry)
		reaches = append(reaches, reach)
	}
	report.Unreviewed = commonDelta(content, report.Profiles, reaches)
	report.UnreviewedLines, err = check.measure(report.Unreviewed)
	return report, err
}

// profileReach is, per path, the states one Profile reached and when.
type profileReach map[string]map[string]time.Time

func (check *coverageCheck) profile(scope configuration.Scope, name string) (ProfileCoverage, profileReach, error) {
	entry := ProfileCoverage{Scope: scope, Profile: name, State: CoverageCovered}
	reach := profileReach{}
	if len(check.content) == 0 {
		return entry, reach, nil
	}
	edges, err := check.ledger.ContentTransitions(store.TransitionQuery{ProfileSource: configuration.ProfileSource(scope, name), Paths: changedPaths(check.content)})
	if err != nil {
		return ProfileCoverage{}, nil, err
	}
	tally := newProfileTally()
	for _, change := range check.content {
		state := reviewedStates(edgesOf(edges, change.Path), change)
		reach[change.Path] = state.reached
		tally.add(state, change)
	}
	tally.fill(&entry)
	if entry.UnreviewedLines, err = check.measure(entry.Unreviewed); err != nil {
		return ProfileCoverage{}, nil, err
	}
	if entry.UnreviewedLines.Exceeds(check.declaration.UnreviewedLines) {
		entry.State = check.overState(entry)
		return entry, reach, nil
	}
	if entry.Unjudged, err = check.judge.unjudged(entry.Reviews); err != nil {
		return ProfileCoverage{}, nil, err
	}
	entry.State = check.withinState(entry)
	return entry, reach, nil
}

// overState decides a Profile with more unreviewed lines than the allowance:
// a Review of it still runs, its budget is spent, or a Review is missing.
func (check *coverageCheck) overState(entry ProfileCoverage) CoverageState {
	switch budget := check.declaration.ReviewBudget; {
	case len(entry.Running) > 0:
		return CoverageRunning
	case budget > 0 && entry.Spent >= budget:
		return CoverageSpent
	default:
		return CoverageMissing
	}
}

func (check *coverageCheck) withinState(entry ProfileCoverage) CoverageState {
	switch {
	case len(entry.Unjudged) > 0:
		return CoverageUnjudged
	case len(entry.Unreviewed) > 0:
		return CoverageResidual
	default:
		return CoverageCovered
	}
}

func (check *coverageCheck) measure(delta []model.ContentChange) (subject.DeltaLines, error) {
	key := model.ContentChangesDigest(delta)
	if lines, done := check.measured[key]; done {
		return lines, nil
	}
	lines, err := check.conductor.measureDelta(check.repository, delta)
	if err != nil {
		return subject.DeltaLines{}, fmt.Errorf("measure unreviewed lines: %w", err)
	}
	check.measured[key] = lines
	return lines, nil
}

func changedPaths(content []model.ContentChange) []string {
	paths := make([]string, 0, len(content))
	for _, change := range content {
		paths = append(paths, change.Path)
	}
	return paths
}

// edgesOf relies on the ledger grouping transitions by path.
func edgesOf(edges []store.ContentTransition, path string) []store.ContentTransition {
	start := sort.Search(len(edges), func(index int) bool { return edges[index].Path >= path })
	end := start
	for end < len(edges) && edges[end].Path == path {
		end++
	}
	return edges[start:end]
}

// pathState is what one Profile reviewed of one path: every state reachable
// from the base over completed Reviews, with the time it was reached, the
// newest one (the current state when it is reachable), the chain of Reviews
// from the base to it, and the Reviews of any lifecycle that started from a
// reachable state.
type pathState struct {
	reached  map[string]time.Time
	reviewed string
	evidence []store.ContentTransition
	spent    map[model.ReviewID]bool
	running  map[model.ReviewID]bool
}

// reviewedStates searches breadth-first from the base. Among parallel edges
// from one state the newest Review is the parent, so re-reviewing the same
// content makes the newer Review the evidence.
func reviewedStates(edges []store.ContentTransition, change model.ContentChange) pathState {
	state := pathState{reached: map[string]time.Time{change.Before: {}}, reviewed: change.Before, spent: map[model.ReviewID]bool{}, running: map[model.ReviewID]bool{}}
	parent := map[string]store.ContentTransition{}
	queue := []string{change.Before}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, edge := range slices.Backward(edges) {
			if edge.Before != node {
				continue
			}
			state.spent[edge.Review] = true
			if isInFlight(edge.Lifecycle) {
				state.running[edge.Review] = true
			}
			if _, seen := state.reached[edge.After]; seen || edge.Lifecycle != model.LifecycleCompleted {
				continue
			}
			state.reached[edge.After] = edge.CreatedAt
			parent[edge.After] = edge
			queue = append(queue, edge.After)
		}
	}
	state.reviewed = newestReached(state.reached, change.After)
	for node := state.reviewed; node != change.Before; node = parent[node].Before {
		state.evidence = append(state.evidence, parent[node])
	}
	slices.Reverse(state.evidence)
	return state
}

// newestReached is the current state when it was reached, else the reached
// state with the newest Review; the base, reached at the zero time, is the
// oldest.
func newestReached(reached map[string]time.Time, current string) string {
	if _, ok := reached[current]; ok {
		return current
	}
	newest, at := "", time.Time{}
	for node, reachedAt := range reached {
		if newest == "" || reachedAt.After(at) || (reachedAt.Equal(at) && node < newest) {
			newest, at = node, reachedAt
		}
	}
	return newest
}

func isInFlight(lifecycle model.Lifecycle) bool {
	return lifecycle == model.LifecyclePending || lifecycle == model.LifecycleRunning
}

// profileTally aggregates path states over the content into one Profile's
// facts, with Reviews ordered by creation.
type profileTally struct {
	evidence   map[model.ReviewID]time.Time
	spent      map[model.ReviewID]bool
	running    map[model.ReviewID]bool
	unreviewed []model.ContentChange
}

func newProfileTally() profileTally {
	return profileTally{evidence: map[model.ReviewID]time.Time{}, spent: map[model.ReviewID]bool{}, running: map[model.ReviewID]bool{}}
}

func (tally *profileTally) add(state pathState, change model.ContentChange) {
	for _, edge := range state.evidence {
		tally.evidence[edge.Review] = edge.CreatedAt
	}
	for id := range state.spent {
		tally.spent[id] = true
	}
	for id := range state.running {
		tally.running[id] = true
	}
	if state.reviewed != change.After {
		tally.unreviewed = append(tally.unreviewed, model.ContentChange{Path: change.Path, Before: state.reviewed, After: change.After})
	}
}

func (tally profileTally) fill(entry *ProfileCoverage) {
	for id := range tally.evidence {
		entry.Reviews = append(entry.Reviews, id)
	}
	slices.SortFunc(entry.Reviews, func(a, b model.ReviewID) int {
		if order := tally.evidence[a].Compare(tally.evidence[b]); order != 0 {
			return order
		}
		return strings.Compare(string(a), string(b))
	})
	entry.Spent = len(tally.spent)
	for id := range tally.running {
		entry.Running = append(entry.Running, id)
	}
	slices.Sort(entry.Running)
	entry.Unreviewed = tally.unreviewed
}

// commonDelta is the delta from the newest state every Profile with
// something unreviewed reached, per path, so one Review of it extends each of
// their chains. It re-reviews more than any one Profile needs, never less.
func commonDelta(content []model.ContentChange, profiles []ProfileCoverage, reaches []profileReach) []model.ContentChange {
	var participants []profileReach
	for index, profile := range profiles {
		if len(profile.Unreviewed) > 0 {
			participants = append(participants, reaches[index])
		}
	}
	if len(participants) == 0 {
		return nil
	}
	var delta []model.ContentChange
	for _, change := range content {
		shared := map[string]time.Time{}
		for node, at := range participants[0][change.Path] {
			shared[node] = at
		}
		for _, reach := range participants[1:] {
			for node, at := range shared {
				reachedAt, ok := reach[change.Path][node]
				if !ok {
					delete(shared, node)
				} else if reachedAt.After(at) {
					shared[node] = reachedAt
				}
			}
		}
		if reviewed := newestReached(shared, change.After); reviewed != change.After {
			delta = append(delta, model.ContentChange{Path: change.Path, Before: reviewed, After: change.After})
		}
	}
	return delta
}
