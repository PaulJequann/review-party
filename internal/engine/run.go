package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

// This file resolves one run request into the Effective Review Selection and
// compiles it into an executable, inspectable Review Bundle. Every reference
// and Profile Revision is validated here, before Subject resolution or any
// Reviewer launch; execution mechanics live in party.go.

// Run executes one ordinary review request: the repository's saved selection,
// or one explicit Profile or Party that replaces it for this run.
func (conductor *Conductor) Run(ctx context.Context, selection model.RunSelection) (model.ReviewBundle, error) {
	if err := ctx.Err(); err != nil {
		return model.ReviewBundle{}, err
	}
	if err := conductor.requirePreparedState(selection.Repository); err != nil {
		return model.ReviewBundle{}, err
	}
	prepared, ledger, err := conductor.prepareRun(selection)
	if err != nil {
		return model.ReviewBundle{}, err
	}
	return conductor.executePreparedBundle(ctx, ledger, prepared)
}

// ReviewExplicitProfile runs one explicitly selected Profile as an ordinary
// Review. It shares Run's scoped resolution and complete preflight, but does
// not create a Review Bundle; explicit Profile dogfood remains a single
// Review Record.
func (conductor *Conductor) ReviewExplicitProfile(ctx context.Context, selection model.RunSelection) (model.ReviewRecord, error) {
	if err := ctx.Err(); err != nil {
		return model.ReviewRecord{}, err
	}
	if selection.Profile == "" {
		return model.ReviewRecord{}, errors.New("an explicit Profile is required")
	}
	if err := conductor.requirePreparedState(selection.Repository); err != nil {
		return model.ReviewRecord{}, err
	}
	reviewStarted := conductor.now().UTC()
	planned, err := conductor.planSelection(selection)
	if err != nil {
		return model.ReviewRecord{}, err
	}
	if len(planned.members) != 1 {
		return model.ReviewRecord{}, errors.New("explicit Profile must resolve to exactly one Profile")
	}
	member := planned.members[0]
	prepared := preparedReview{subject: planned.subject, profile: member.profile, timings: member.timings, deadline: member.profile.deadline}
	return conductor.runPreparedReview(ctx, prepared, nil, reviewStarted)
}

type plannedSelection struct {
	resolved   configuration.ResolvedReviews
	repository string
	subject    subject.Subject
	members    []compiledSlot
}

type compiledSlot struct {
	slot    configuration.ExpandedProfile
	profile compiledProfile
	timings model.ReviewTimings
}

// planSelection preflights the complete selection before anything persists or
// launches. It fails closed on a missing selection, missing reference,
// incomplete Profile, unavailable saved Reviewer, or rejected model.
func (conductor *Conductor) planSelection(selection model.RunSelection) (plannedSelection, error) {
	repository, err := resolveSubjectRepository(selection.Repository, selection.Subject)
	if err != nil {
		return plannedSelection{}, err
	}
	resolved, err := conductor.resolveEffectiveReviews(repository, selection)
	if err != nil {
		return plannedSelection{}, err
	}
	members, err := conductor.compileSlots(resolved.snapshot)
	if err != nil {
		return plannedSelection{}, err
	}
	resolvedRepository, subject, subjectResolutionMS, err := conductor.prepareReviewSubject(selection.Repository, selection.Subject)
	if err == nil && resolvedRepository != repository {
		repository = resolvedRepository
	}
	if err != nil {
		return plannedSelection{}, err
	}
	for index := range members {
		members[index].timings.SubjectResolutionMS = subjectResolutionMS
	}
	return plannedSelection{resolved: resolved.snapshot.Selection(), repository: repository, subject: subject, members: members}, nil
}

// resolveSharedSubject freezes the one Review Subject every member will review,
// after preflight compiles every Profile Revision and before any launch.
func (conductor *Conductor) prepareReviewSubject(repository string, reference model.SubjectReference) (string, subject.Subject, int64, error) {
	started := conductor.now().UTC()
	resolvedRepository, err := resolveSubjectRepository(repository, reference)
	if err != nil {
		return "", subject.Subject{}, elapsedMilliseconds(started, conductor.now().UTC()), err
	}
	resolved, err := subject.ResolveSubject(resolvedRepository, reference)
	return resolvedRepository, resolved, elapsedMilliseconds(started, conductor.now().UTC()), err
}

// resolveSubjectRepository is the single repository rule shared by ordinary
// and Eval preparation, including captured Subjects that already carry paths.
func resolveSubjectRepository(repository string, reference model.SubjectReference) (string, error) {
	if reference.Kind == model.SubjectCapturedChange {
		return repository, nil
	}
	return subject.ResolveRepositoryRoot(repository)
}

// resolvedRunSelection carries the one configuration snapshot for a run.
type resolvedRunSelection struct {
	snapshot configuration.RuntimeSnapshot
}

func (conductor *Conductor) resolveEffectiveReviews(repository string, selection model.RunSelection) (resolvedRunSelection, error) {
	return resolveRuntimeSelection(conductor.configuration, repository, selection)
}

// resolveRuntimeSelection keeps run planning on the typed Configuration
// Manager seam and gives the compiler one immutable result to consume.
func resolveRuntimeSelection(resolver configuration.RuntimeResolver, repository string, selection model.RunSelection) (resolvedRunSelection, error) {
	repositoryID := configuration.Repository(repository)
	snapshot, err := resolver.ResolveRuntime(configuration.RunRequest{
		Repository: repositoryID, Profile: selection.Profile, Party: selection.Party,
	})
	if err != nil {
		return resolvedRunSelection{}, err
	}
	return resolvedRunSelection{snapshot: snapshot}, nil
}

// compileSlots compiles every expanded Profile Revision so an incompatible
// Reviewer fails closed before the bundle row exists and before any launch.
func (conductor *Conductor) compileSlots(snapshot configuration.RuntimeSnapshot) ([]compiledSlot, error) {
	selection := snapshot.Selection()
	slots := make([]compiledSlot, 0, len(selection.Expanded))
	for _, slot := range selection.Expanded {
		compiledStarted := conductor.now().UTC()
		profile, err := conductor.compileSlotProfile(snapshot, slot)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", slot.Origin, err)
		}
		slots = append(slots, compiledSlot{
			slot:    slot,
			profile: profile,
			timings: model.ReviewTimings{ProfileCompilationMS: elapsedMilliseconds(compiledStarted, conductor.now().UTC())},
		})
	}
	return slots, nil
}

func (conductor *Conductor) compileSlotProfile(snapshot configuration.RuntimeSnapshot, slot configuration.ExpandedProfile) (compiledProfile, error) {
	profile, found := snapshot.ProfileFor(slot)
	if !found {
		return compiledProfile{}, fmt.Errorf("%s Profile %q was not found", slot.Scope, slot.Profile)
	}
	deadline, err := time.ParseDuration(profile.AttemptDeadline)
	if err != nil {
		return compiledProfile{}, fmt.Errorf("%s Profile %q: %w", slot.Scope, slot.Profile, err)
	}
	return conductor.compileProfile(profileCompileRequest{
		profile:   profile,
		effective: snapshot.Effective(),
		selection: model.ProfileSelection{Profile: profile.Name, Reviewer: profile.Reviewer, Model: profile.Model, Effort: profile.ReasoningEffort},
		deadline:  deadline,
	})
}
