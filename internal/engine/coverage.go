package engine

// Coverage walks, per Profile and per path, the reviewed states a Checkpoint
// can reach from the base blob over completed Reviews, and measures what is
// left between the newest reviewed state and the current content. It reports
// facts; the Caller decides what an unreviewed Checkpoint blocks.

import (
	"errors"
	"fmt"
	"maps"
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
// absent is the reviewed blobs the repository no longer has.
type coverageCheck struct {
	conductor   *Conductor
	ledger      transitionLedger
	repository  string
	content     []model.ContentChange
	declaration configuration.Checkpoint
	judge       *findingJudge
	measured    map[string]subject.DeltaLines
	absent      map[string]bool
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
	check, err := conductor.newCoverageCheck(repository, content, declaration, judge)
	if err != nil {
		return CoverageReport{}, err
	}
	report := CoverageReport{}
	var participants []profileReach
	if report.Profiles, participants, err = check.profiles(resolved.Expanded); err != nil {
		return CoverageReport{}, err
	}
	report.Unreviewed = commonDelta(content, participants)
	report.UnreviewedLines, err = check.measure(report.Unreviewed)
	return report, err
}

// profiles reports each selected Profile in selection order, with the reach
// of each one that has something unreviewed.
func (check *coverageCheck) profiles(selected []configuration.ExpandedProfile) ([]ProfileCoverage, []profileReach, error) {
	sources := make([]string, 0, len(selected))
	for _, profile := range selected {
		sources = append(sources, configuration.ProfileSource(profile.Scope, profile.Profile))
	}
	edges, err := check.loadEdges(sources)
	if err != nil {
		return nil, nil, err
	}
	var entries []ProfileCoverage
	var participants []profileReach
	for index, profile := range selected {
		entry, reach, err := check.profile(profile.Scope, profile.Profile, edges[sources[index]])
		if err != nil {
			return nil, nil, fmt.Errorf("check coverage for Profile %q: %w", profile.Profile, err)
		}
		entries = append(entries, entry)
		if len(entry.Unreviewed) > 0 {
			participants = append(participants, reach)
		}
	}
	return entries, participants, nil
}

func (conductor *Conductor) newCoverageCheck(repository string, content []model.ContentChange, declaration configuration.Checkpoint, judge *findingJudge) (*coverageCheck, error) {
	ledger, ok := conductor.store.(transitionLedger)
	if !ok {
		return nil, errors.New("coverage check requires the SQLite ledger")
	}
	return &coverageCheck{conductor: conductor, ledger: ledger, repository: repository, content: content, declaration: declaration, judge: judge, measured: map[string]subject.DeltaLines{}}, nil
}

// loadEdges reads each Profile source's edges on the content's paths and
// asks the repository once which reached blobs it no longer has, such as a
// reviewed working-tree blob that git gc pruned. Such a state counts as
// unreached, so the delta falls back to an older state: more, never less.
func (check *coverageCheck) loadEdges(sources []string) (map[string][]store.ContentTransition, error) {
	edges := map[string][]store.ContentTransition{}
	if len(check.content) == 0 {
		return edges, nil
	}
	paths := changedPaths(check.content)
	for _, source := range sources {
		transitions, err := check.ledger.ContentTransitions(store.TransitionQuery{ProfileSource: source, Paths: paths})
		if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
			return nil, InitializationRequiredError{Repository: check.repository}
		}
		if err != nil {
			return nil, err
		}
		edges[source] = transitions
	}
	var err error
	check.absent, err = check.conductor.missingObjects(check.repository, maps.Keys(check.reachedBlobs(edges)))
	return edges, err
}

// reachedBlobs is every blob a completed Review reached on the edges. A
// gitlink path's states are commits of its nested repository, never objects
// of this one, so they are left out.
func (check *coverageCheck) reachedBlobs(edges map[string][]store.ContentTransition) map[string]bool {
	gitlink := map[string]bool{}
	for _, change := range check.content {
		gitlink[change.Path] = change.BeforeGitlink || change.AfterGitlink
	}
	reached := map[string]bool{}
	for _, transitions := range edges {
		for _, edge := range transitions {
			if gitlink[edge.Path] || edge.Lifecycle != model.LifecycleCompleted {
				continue
			}
			if edge.After != model.ZeroObjectID {
				reached[edge.After] = true
			}
		}
	}
	return reached
}

// profileReach is, per path, the states one Profile reached and when.
type profileReach map[string]map[string]time.Time

func (check *coverageCheck) profile(scope configuration.Scope, name string, edges []store.ContentTransition) (ProfileCoverage, profileReach, error) {
	entry := ProfileCoverage{Scope: scope, Profile: name, State: CoverageCovered}
	reach := profileReach{}
	if len(check.content) == 0 {
		return entry, reach, nil
	}
	tally := newProfileTally()
	for _, change := range check.content {
		state := reviewedStates(edgesOf(edges, change.Path), change, check.absent)
		reach[change.Path] = state.reached
		tally.add(state, change)
	}
	tally.fill(&entry)
	var err error
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
// content makes the newer Review the evidence. An edge into an absent blob
// spends budget and reaches nothing.
func reviewedStates(edges []store.ContentTransition, change model.ContentChange, absent map[string]bool) pathState {
	state := pathState{reached: map[string]time.Time{change.Before: {}}, reviewed: change.Before, spent: map[model.ReviewID]bool{}, running: map[model.ReviewID]bool{}}
	parent := map[string]store.ContentTransition{}
	queue := []string{change.Before}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, edge := range slices.Backward(edges) {
			if edge.Before == node && state.follow(edge, absent) {
				parent[edge.After] = edge
				queue = append(queue, edge.After)
			}
		}
	}
	state.reviewed = newestReached(state.reached, change.After)
	for node := state.reviewed; node != change.Before; node = parent[node].Before {
		state.evidence = append(state.evidence, parent[node])
	}
	slices.Reverse(state.evidence)
	return state
}

// follow spends the edge's Review and reports whether the edge reaches a new
// state: its Review completed and its blob is still in the repository.
func (state *pathState) follow(edge store.ContentTransition, absent map[string]bool) bool {
	state.spent[edge.Review] = true
	if isInFlight(edge.Lifecycle) {
		state.running[edge.Review] = true
	}
	credits := edge.Lifecycle == model.LifecycleCompleted && !absent[edge.After]
	if _, seen := state.reached[edge.After]; seen || !credits {
		return false
	}
	state.reached[edge.After] = edge.CreatedAt
	return true
}

// newestReached is the current state when it was reached, else the reached
// state with the newest Review; the base, reached at the zero time, is the
// oldest.
func newestReached(reached map[string]time.Time, current string) string {
	if _, ok := reached[current]; ok {
		return current
	}
	nodes := slices.Collect(maps.Keys(reached))
	if len(nodes) == 0 {
		return ""
	}
	slices.SortFunc(nodes, func(a, b string) int {
		if order := reached[b].Compare(reached[a]); order != 0 {
			return order
		}
		return strings.Compare(a, b)
	})
	return nodes[0]
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
		tally.unreviewed = append(tally.unreviewed, change.From(state.reviewed))
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

// commonDelta is the delta from the newest state every participating Profile
// reached, per path, so one Review of it extends each of their chains. It
// re-reviews more than any one Profile needs, never less.
func commonDelta(content []model.ContentChange, participants []profileReach) []model.ContentChange {
	if len(participants) == 0 {
		return nil
	}
	var delta []model.ContentChange
	for _, change := range content {
		if reviewed := newestReached(sharedReach(participants, change.Path), change.After); reviewed != change.After {
			delta = append(delta, change.From(reviewed))
		}
	}
	return delta
}

// sharedReach is the states of one path every participant reached, each at
// the time the last of them reached it.
func sharedReach(participants []profileReach, path string) map[string]time.Time {
	shared := maps.Clone(participants[0][path])
	for _, reach := range participants[1:] {
		for node, at := range shared {
			reachedAt, ok := reach[path][node]
			switch {
			case !ok:
				delete(shared, node)
			case reachedAt.After(at):
				shared[node] = reachedAt
			}
		}
	}
	return shared
}
