package engine

// Under the judged requirement a covered Profile also needs its Findings
// judged. A Profile passes when Coverage holds over Reviews whose every
// Finding has a current Verdict, so judging any one covering Review, or one
// Review per commit, is enough. A Finding located in a path the Checkpoint
// exempts needs no Verdict. Verdicts are read only
// for Profiles that Coverage already holds for.

import (
	"fmt"
	"strings"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

// UnjudgedReview is a covering Review with Findings that have no current
// Verdict, by ordinal.
type UnjudgedReview struct {
	Review   model.ReviewID `json:"review_id"`
	Ordinals []int          `json:"ordinals"`
}

// findingJudge decides the judged requirement for one Checkpoint check. It
// remembers each Review's unjudged ordinals, so a Review read for one pass is
// not read again.
type findingJudge struct {
	conductor   *Conductor
	declaration configuration.Checkpoint
	unjudged    map[model.ReviewID][]int
}

// newFindingJudge returns no judge unless the Checkpoint is declared judged.
func (conductor *Conductor) newFindingJudge(declaration *configuration.Checkpoint) *findingJudge {
	if declaration == nil || declaration.Requirement != configuration.RequirementJudged {
		return nil
	}
	return &findingJudge{conductor: conductor, declaration: *declaration, unjudged: map[model.ReviewID][]int{}}
}

// decide decides a Profile's Coverage and, when there is a judge, its
// verdicts. Without a judge, or for a Profile that is not covered, it is
// Coverage alone.
func (judge *findingJudge) decide(lookup coverageLookup, content CoverageSubject) (CoverageState, []model.ReviewID, []UnjudgedReview, error) {
	state, covering, err := decideCoverage(lookup, content)
	if judge == nil || state != CoverageCovered {
		return state, covering, nil, err
	}
	reviews, unjudged, err := judge.judge(lookup, content, covering)
	return state, reviews, unjudged, err
}

// judge decides a covered Profile again over only its fully judged Reviews.
// When those cover the content, they are the Profile's Reviews. Otherwise the
// Profile keeps the Reviews that supplied Coverage, and each of them with an
// unjudged Finding is returned.
func (judge *findingJudge) judge(lookup coverageLookup, content CoverageSubject, covering []model.ReviewID) ([]model.ReviewID, []UnjudgedReview, error) {
	judged := func(changes []model.ContentChange) ([]store.CoverageCandidate, error) {
		candidates, err := lookup(changes)
		if err != nil {
			return nil, err
		}
		return judge.fullyJudged(candidates)
	}
	state, ids, err := decideCoverage(judged, content)
	if err != nil || state == CoverageCovered {
		return ids, nil, err
	}
	var gaps []UnjudgedReview
	for _, id := range covering {
		ordinals, err := judge.unjudgedOrdinals(id)
		if err != nil {
			return nil, nil, err
		}
		if len(ordinals) > 0 {
			gaps = append(gaps, UnjudgedReview{Review: id, Ordinals: ordinals})
		}
	}
	return covering, gaps, nil
}

// fullyJudged keeps, in order, the completed candidates with no unjudged
// Finding.
func (judge *findingJudge) fullyJudged(candidates []store.CoverageCandidate) ([]store.CoverageCandidate, error) {
	var kept []store.CoverageCandidate
	for _, candidate := range candidates {
		if !isCompleted(candidate.Lifecycle) {
			continue
		}
		ordinals, err := judge.unjudgedOrdinals(candidate.ID)
		if err != nil {
			return nil, err
		}
		if len(ordinals) == 0 {
			kept = append(kept, candidate)
		}
	}
	return kept, nil
}

// unjudgedOrdinals lists the Review's Findings without a current Verdict,
// leaving out those located in an exempt path.
func (judge *findingJudge) unjudgedOrdinals(id model.ReviewID) ([]int, error) {
	if ordinals, known := judge.unjudged[id]; known {
		return ordinals, nil
	}
	record, err := judge.conductor.store.Load(id)
	if err != nil {
		return nil, fmt.Errorf("load covering review %s: %w", id, err)
	}
	current, err := judge.currentVerdicts(id)
	if err != nil {
		return nil, err
	}
	var findings []model.Finding
	if record.Result != nil {
		findings = record.Result.Findings
	}
	ordinals := []int{}
	for _, finding := range findings {
		if !current[finding.Ordinal] && !judge.exempts(finding.Location) {
			ordinals = append(ordinals, finding.Ordinal)
		}
	}
	judge.unjudged[id] = ordinals
	return ordinals, nil
}

// currentVerdicts reads which of the Review's Findings have a current Verdict.
// A stale Verdict judged text the Finding no longer holds, so it does not
// count.
func (judge *findingJudge) currentVerdicts(id model.ReviewID) (map[int]bool, error) {
	ledger, err := judge.conductor.verdictStore()
	if err != nil {
		return nil, err
	}
	verdicts, err := ledger.ListVerdicts(store.VerdictQuery{ReviewIDs: []model.ReviewID{id}})
	if err != nil {
		return nil, fmt.Errorf("load verdicts on review %s: %w", id, err)
	}
	current := map[int]bool{}
	for _, verdict := range verdicts {
		if !verdict.Stale {
			current[verdict.Ordinal] = true
		}
	}
	return current, nil
}

// exempts reports whether a Finding's location is one path:line, as the
// result contract writes it, in a path the Checkpoint exempts. Any other
// location, such as one naming two files, needs a Verdict.
func (judge *findingJudge) exempts(location string) bool {
	path, line, found := strings.Cut(strings.TrimSpace(location), ":")
	return found && line != "" && strings.Trim(line, "0123456789-:") == "" && judge.declaration.Exempts(path)
}
