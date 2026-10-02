package engine

// Coverage answers whether every Review a repository selects already
// examined exactly the content a Checkpoint is about to accept. It reports
// facts; the Caller decides what an uncovered Checkpoint blocks.

import (
	"context"
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

// CoverageSubject is the content a Checkpoint accepts: the whole set, and for
// a committed range the set each commit introduces.
type CoverageSubject struct {
	Changes []model.ContentChange
	Commits []subject.CommitContentChanges
}

type ProfileCoverage struct {
	Scope     configuration.Scope
	Profile   string
	State     CoverageState
	ReviewIDs []model.ReviewID
}

type CoverageReport struct {
	Covered  bool
	Profiles []ProfileCoverage
}

type coverageLedger interface {
	ContentChangeCoverage(store.CoverageQuery) ([]store.CoverageCandidate, error)
}

// CheckCoverage reports, per selected Profile in selection order, whether a
// Review of that Profile covers the content.
func (conductor *Conductor) CheckCoverage(_ context.Context, repository string, coverage CoverageSubject) (CoverageReport, error) {
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
			return ledger.ContentChangeCoverage(store.CoverageQuery{ProfileSource: source, Changes: changes})
		}
		state, ids, err := decideCoverage(lookup, coverage)
		if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
			return CoverageReport{}, InitializationRequiredError{Repository: repository}
		}
		if err != nil {
			return CoverageReport{}, fmt.Errorf("check coverage for Profile %q: %w", profile.Profile, err)
		}
		report.Covered = report.Covered && state == CoverageCovered
		report.Profiles = append(report.Profiles, ProfileCoverage{Scope: profile.Scope, Profile: profile.Profile, State: state, ReviewIDs: ids})
	}
	return report, nil
}

type coverageLookup func([]model.ContentChange) ([]store.CoverageCandidate, error)

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
