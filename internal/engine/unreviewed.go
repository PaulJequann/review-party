package engine

// A run is bounded by what the selected Profiles already reviewed of its
// Subject. Every run names each Review's place in the budget the scope's
// Checkpoint declares, and run --unreviewed narrows the Subject to the delta
// from the newest state the participating Profiles reached, so the Review
// extends each of their chains.

import (
	"errors"
	"fmt"
	"slices"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

// ErrNothingUnreviewed reports a run --unreviewed with nothing left to review.
var ErrNothingUnreviewed = errors.New("nothing unreviewed: every selected Profile reviewed the current content")

// boundMember is one planned member with what it reviewed of the Subject.
type boundMember struct {
	slot     compiledSlot
	coverage ProfileCoverage
	reach    profileReach
}

// label names the member as the run heartbeat does.
func (member boundMember) label() string {
	return string(member.slot.slot.Scope) + ":" + member.slot.slot.Profile
}

// priorFinding is a Finding of a chain evidence Review, given to the Review of
// the delta that follows it as context.
type priorFinding struct {
	review  model.ReviewID
	finding model.Finding
}

func (conductor *Conductor) boundPlan(planned plannedSelection, unreviewed bool) (plannedSelection, error) {
	declaration, err := conductor.scopeCheckpoint(planned.repository, planned.preparedSubject.value.Kind)
	if err != nil {
		return plannedSelection{}, err
	}
	if !unreviewed && declaration.ReviewBudget == 0 {
		return planned, nil
	}
	content, _ := partitionExempt(declaration, planned.preparedSubject.value.ContentChanges)
	members, err := conductor.measureMembers(planned, content, declaration)
	if err != nil {
		return plannedSelection{}, err
	}
	if unreviewed {
		if members, err = conductor.narrowToUnreviewed(&planned, content, members); err != nil {
			return plannedSelection{}, err
		}
	}
	for _, member := range members {
		conductor.announceBudget(member, declaration.ReviewBudget)
	}
	return planned, nil
}

// scopeCheckpoint is the declared Checkpoint whose allowance and budget bound
// a Subject of this kind: pre-push for a committed range, pre-commit for
// working changes. An undeclared one allows nothing and has no budget.
func (conductor *Conductor) scopeCheckpoint(repository string, kind model.SubjectKind) (configuration.Checkpoint, error) {
	names := map[model.SubjectKind]configuration.CheckpointName{model.SubjectCommittedRange: configuration.CheckpointPrePush, model.SubjectWorkingChanges: configuration.CheckpointPreCommit}
	name, bounded := names[kind]
	if !bounded {
		return configuration.Checkpoint{}, nil
	}
	checkpoints, err := conductor.configuration.Checkpoints(configuration.Repository(repository))
	if err != nil {
		return configuration.Checkpoint{}, err
	}
	return checkpoints[name], nil
}

// measureMembers measures what each member reviewed of the content the
// Checkpoint requires Reviews for.
func (conductor *Conductor) measureMembers(planned plannedSelection, content []model.ContentChange, declaration configuration.Checkpoint) ([]boundMember, error) {
	check, err := conductor.newCoverageCheck(planned.repository, content, declaration, nil)
	if err != nil {
		return nil, err
	}
	sources := make([]string, 0, len(planned.members))
	for _, slot := range planned.members {
		sources = append(sources, configuration.ProfileSource(slot.slot.Scope, slot.profile.revision.Name))
	}
	edges, err := check.loadEdges(sources)
	if err != nil {
		return nil, err
	}
	members := make([]boundMember, 0, len(planned.members))
	for index, slot := range planned.members {
		coverage, reach, err := check.profile(slot.slot.Scope, slot.profile.revision.Name, edges[sources[index]])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", slot.slot.Origin, err)
		}
		members = append(members, boundMember{slot: slot, coverage: coverage, reach: reach})
	}
	return members, nil
}

// narrowToUnreviewed keeps the members with something unreviewed, replaces
// the Subject with their common unreviewed delta of the required content
// unless that is the whole Subject, and frames the prompt of each member with
// earlier Reviews.
func (conductor *Conductor) narrowToUnreviewed(planned *plannedSelection, content []model.ContentChange, members []boundMember) ([]boundMember, error) {
	participants := conductor.withUnreviewed(members)
	if len(participants) == 0 {
		return nil, ErrNothingUnreviewed
	}
	scope := planned.preparedSubject.value.ReviewSubject
	delta := commonDelta(content, reaches(participants))
	if model.ContentChangesDigest(delta) != model.ContentChangesDigest(scope.ContentChanges) {
		resolved, err := subject.ResolveUnreviewedDelta(scope, delta)
		if err != nil {
			return nil, err
		}
		planned.preparedSubject.value = resolved
	}
	if err := conductor.frameDeltaPrompts(participants, changedPaths(delta)); err != nil {
		return nil, err
	}
	planned.members = make([]compiledSlot, 0, len(participants))
	for _, member := range participants {
		planned.members = append(planned.members, member.slot)
	}
	return participants, nil
}

// withUnreviewed keeps the members with something unreviewed and names the
// skipped ones.
func (conductor *Conductor) withUnreviewed(members []boundMember) []boundMember {
	var participants []boundMember
	for _, member := range members {
		if len(member.coverage.Unreviewed) == 0 {
			conductor.warn(fmt.Sprintf("%s: nothing unreviewed; skipped", member.label()))
			continue
		}
		participants = append(participants, member)
	}
	return participants
}

func reaches(members []boundMember) []profileReach {
	result := make([]profileReach, 0, len(members))
	for _, member := range members {
		result = append(result, member.reach)
	}
	return result
}

// frameDeltaPrompts gives the prompt of each member with earlier Reviews the
// delta framing and the prior Findings of its own chain on the delta's paths.
func (conductor *Conductor) frameDeltaPrompts(members []boundMember, paths []string) error {
	for index := range members {
		if len(members[index].coverage.Reviews) == 0 {
			continue
		}
		prior, err := conductor.priorFindings(members[index].coverage.Reviews, paths)
		if err != nil {
			return err
		}
		profile := &members[index].slot.profile
		snapshot := profile.snapshot
		profile.buildPrompt = func(subject model.ReviewSubject) string { return renderDeltaPrompt(snapshot, subject, prior) }
	}
	return nil
}

// priorFindings are the Findings of the chain evidence Reviews located in a
// path the delta touches. They are context for the Reviewer, not a stored
// lineage.
func (conductor *Conductor) priorFindings(evidence []model.ReviewID, paths []string) ([]priorFinding, error) {
	var prior []priorFinding
	for _, id := range evidence {
		record, err := conductor.store.Load(id)
		if err != nil {
			return nil, fmt.Errorf("load evidence review %s: %w", id, err)
		}
		prior = append(prior, findingsOn(record, paths)...)
	}
	return prior, nil
}

func findingsOn(record model.ReviewRecord, paths []string) []priorFinding {
	if record.Result == nil {
		return nil
	}
	var located []priorFinding
	for _, finding := range record.Result.Findings {
		if slices.Contains(paths, findingPath(finding.Location)) {
			located = append(located, priorFinding{review: record.ID, finding: finding})
		}
	}
	return located
}

// announceBudget names the Review's place in the scope's budget: the Reviews
// already spent on this content plus this one.
func (conductor *Conductor) announceBudget(member boundMember, budget int) {
	if budget > 0 {
		conductor.warn(fmt.Sprintf("%s: Review %d of %d", member.label(), member.coverage.Spent+1, budget))
	}
}

func (conductor *Conductor) warn(line string) {
	if warn := conductor.getRunner().warn; warn != nil {
		warn(line)
	}
}
