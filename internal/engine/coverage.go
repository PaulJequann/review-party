package engine

// Coverage answers whether every Review a repository selects already
// examined exactly the content a Checkpoint is about to accept. It reports
// facts; the Caller decides what an uncovered Checkpoint blocks.

import (
	"errors"
	"fmt"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

type CoverageState string

const (
	CoverageCovered CoverageState = "covered"
	CoverageRunning CoverageState = "running"
	CoverageMissing CoverageState = "missing"
)

// CoverageSubject is the content a Checkpoint accepts: the whole set, for a
// committed range the set each commit introduces, and the line counts that
// size the whole change.
type CoverageSubject struct {
	Changes []model.ContentChange
	Commits []subject.CommitContentChanges
	Lines   subject.LineCounts
}

// ProfileCoverage is one Profile's Coverage. Under the judged requirement,
// Unjudged lists the covering Reviews whose Findings still need Verdicts.
type ProfileCoverage struct {
	Scope     configuration.Scope
	Profile   string
	State     CoverageState
	ReviewIDs []model.ReviewID
	Unjudged  []UnjudgedReview
}

type CoverageReport struct {
	Covered  bool
	Profiles []ProfileCoverage
}

// judged reports whether every covered Profile has its Findings judged.
func (report CoverageReport) judged() bool {
	for _, profile := range report.Profiles {
		if len(profile.Unjudged) > 0 {
			return false
		}
	}
	return true
}

type coverageLedger interface {
	CoverageCandidates(store.CoverageQuery) ([]store.CoverageCandidate, error)
}

// checkCoverage reports, per selected Profile in selection order, whether a
// Review of that Profile covers the content. A Review covers a set when its
// recorded set, without the declaration's exempt paths, equals it. A judge,
// present under the judged requirement, then decides each covered Profile.
func (conductor *Conductor) checkCoverage(repository string, coverage CoverageSubject, declaration configuration.Checkpoint, judge *findingJudge) (CoverageReport, error) {
	resolved, err := conductor.configuration.ResolveRun(configuration.RunRequest{Repository: configuration.Repository(repository)})
	if errors.Is(err, configuration.ErrNoRepositorySelection) {
		return CoverageReport{}, fmt.Errorf("%w; run review-party init to choose the Reviews this repository runs", err)
	}
	if err != nil {
		return CoverageReport{}, err
	}
	ledger, ok := conductor.store.(coverageLedger)
	if !ok {
		return CoverageReport{}, errors.New("coverage check requires the SQLite ledger")
	}
	report := CoverageReport{Covered: true}
	for _, profile := range resolved.Expanded {
		source := configuration.ProfileSource(profile.Scope, profile.Profile)
		lookup := func(changes []model.ContentChange) ([]store.CoverageCandidate, error) {
			candidates, err := ledger.CoverageCandidates(store.CoverageQuery{ProfileSource: source, Changes: changes})
			return coveringCandidates(candidates, changes, declaration), err
		}
		entry := ProfileCoverage{Scope: profile.Scope, Profile: profile.Profile}
		entry.State, entry.ReviewIDs, entry.Unjudged, err = judge.decide(lookup, coverage)
		if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
			return CoverageReport{}, InitializationRequiredError{Repository: repository}
		}
		if err != nil {
			return CoverageReport{}, fmt.Errorf("check coverage for Profile %q: %w", profile.Profile, err)
		}
		report.Covered = report.Covered && entry.State == CoverageCovered
		report.Profiles = append(report.Profiles, entry)
	}
	return report, nil
}

type coverageLookup func([]model.ContentChange) ([]store.CoverageCandidate, error)

// coveringCandidates keeps, in order, the candidates whose recorded set
// without exempt paths equals the wanted set.
func coveringCandidates(candidates []store.CoverageCandidate, wanted []model.ContentChange, declaration configuration.Checkpoint) []store.CoverageCandidate {
	want := make(map[model.ContentChange]bool, len(wanted))
	for _, change := range wanted {
		want[change] = true
	}
	var covering []store.CoverageCandidate
	for _, candidate := range candidates {
		recorded, _ := partitionExempt(declaration, candidate.Changes)
		if len(recorded) != len(want) {
			continue
		}
		equal := true
		for _, change := range recorded {
			equal = equal && want[change]
		}
		if equal {
			covering = append(covering, candidate)
		}
	}
	return covering
}

// decideCoverage prefers one completed Review of the whole set, then
// completed Reviews of every commit, then Reviews still in flight. An
// incomplete Review never covers anything.
func decideCoverage(lookup coverageLookup, coverage CoverageSubject) (CoverageState, []model.ReviewID, error) {
	if len(coverage.Changes) == 0 {
		return CoverageCovered, nil, nil
	}
	whole, err := lookup(coverage.Changes)
	if err != nil {
		return "", nil, err
	}
	if id, ok := newestCandidate(whole, isCompleted); ok {
		return CoverageCovered, []model.ReviewID{id}, nil
	}
	commits, err := tallyCommitCoverage(lookup, coverage.Commits)
	if err != nil {
		return "", nil, err
	}
	commitState, commitIDs := commits.decision()
	if commitState == CoverageCovered {
		return commitState, commitIDs, nil
	}
	if id, ok := newestCandidate(whole, isInFlight); ok {
		return CoverageRunning, []model.ReviewID{id}, nil
	}
	return commitState, commitIDs, nil
}

type commitCoverageTally struct {
	completed []model.ReviewID
	inFlight  []model.ReviewID
	missing   int
}

// decision covers only when every commit has a completed Review, and runs
// only when no commit lacks a Review. A range with no commits is missing.
func (tally commitCoverageTally) decision() (CoverageState, []model.ReviewID) {
	switch {
	case tally.missing > 0 || len(tally.completed)+len(tally.inFlight) == 0:
		return CoverageMissing, nil
	case len(tally.inFlight) > 0:
		return CoverageRunning, tally.inFlight
	default:
		return CoverageCovered, tally.completed
	}
}

func tallyCommitCoverage(lookup coverageLookup, commits []subject.CommitContentChanges) (commitCoverageTally, error) {
	var tally commitCoverageTally
	for _, commit := range commits {
		candidates, err := lookup(commit.Changes)
		if err != nil {
			return commitCoverageTally{}, err
		}
		if id, ok := newestCandidate(candidates, isCompleted); ok {
			tally.completed = append(tally.completed, id)
		} else if id, ok := newestCandidate(candidates, isInFlight); ok {
			tally.inFlight = append(tally.inFlight, id)
		} else {
			tally.missing++
		}
	}
	return tally, nil
}

// newestCandidate relies on the ledger returning candidates newest first.
func newestCandidate(candidates []store.CoverageCandidate, accept func(model.Lifecycle) bool) (model.ReviewID, bool) {
	for _, candidate := range candidates {
		if accept(candidate.Lifecycle) {
			return candidate.ID, true
		}
	}
	return "", false
}

func isCompleted(lifecycle model.Lifecycle) bool { return lifecycle == model.LifecycleCompleted }

func isInFlight(lifecycle model.Lifecycle) bool {
	return lifecycle == model.LifecyclePending || lifecycle == model.LifecycleRunning
}
