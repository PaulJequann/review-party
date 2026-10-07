package engine

// Under the judged requirement a Profile within its allowance also needs the
// Findings of its chain evidence Reviews judged: every Finding has a current
// Verdict. A Finding located in a path the Checkpoint exempts needs no
// Verdict. Verdicts are read only for Profiles within their allowance.

import (
	"fmt"
	"strings"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

// UnjudgedReview is an evidence Review with Findings that have no current
// Verdict, by ordinal.
type UnjudgedReview struct {
	Review   model.ReviewID `json:"review_id"`
	Ordinals []int          `json:"ordinals"`
}

// findingJudge decides the judged requirement for one Checkpoint check. It
// remembers each Review's unjudged ordinals, so a Review read for one
// Profile is not read again for another.
type findingJudge struct {
	conductor   *Conductor
	declaration configuration.Checkpoint
	ordinals    map[model.ReviewID][]int
}

// newFindingJudge returns no judge unless the Checkpoint is declared judged.
func (conductor *Conductor) newFindingJudge(declaration *configuration.Checkpoint) *findingJudge {
	if declaration == nil || declaration.Requirement != configuration.RequirementJudged {
		return nil
	}
	return &findingJudge{conductor: conductor, declaration: *declaration, ordinals: map[model.ReviewID][]int{}}
}

// unjudged lists, in order, the evidence Reviews with a Finding that has no
// current Verdict. Without a judge there are none.
func (judge *findingJudge) unjudged(reviews []model.ReviewID) ([]UnjudgedReview, error) {
	if judge == nil {
		return nil, nil
	}
	var gaps []UnjudgedReview
	for _, id := range reviews {
		ordinals, err := judge.unjudgedOrdinals(id)
		if err != nil {
			return nil, err
		}
		if len(ordinals) > 0 {
			gaps = append(gaps, UnjudgedReview{Review: id, Ordinals: ordinals})
		}
	}
	return gaps, nil
}

// unjudgedOrdinals lists the Review's Findings without a current Verdict,
// leaving out those located in an exempt path.
func (judge *findingJudge) unjudgedOrdinals(id model.ReviewID) ([]int, error) {
	if ordinals, known := judge.ordinals[id]; known {
		return ordinals, nil
	}
	record, err := judge.conductor.store.Load(id)
	if err != nil {
		return nil, fmt.Errorf("load evidence review %s: %w", id, err)
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
	judge.ordinals[id] = ordinals
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

// exempts reports whether a Finding is located in a path the Checkpoint
// exempts. A Finding at any other location needs a Verdict.
func (judge *findingJudge) exempts(location string) bool {
	path := findingPath(location)
	return path != "" && judge.declaration.Exempts(path)
}

// findingPath is the path of a Finding located at one path:line, as the
// result contract writes it. Any other location, such as one naming two
// files, has no path.
func findingPath(location string) string {
	path, line, found := strings.Cut(strings.TrimSpace(location), ":")
	if found && isLineReference(line) {
		return path
	}
	return ""
}

func isLineReference(line string) bool {
	return line != "" && strings.Trim(line, "0123456789-:") == ""
}
