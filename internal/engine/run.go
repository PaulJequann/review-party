package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

// This file resolves one run request into the Effective Review Selection and
// compiles it into an executable, inspectable Review Bundle. Every reference
// and Profile Revision is validated here, before Subject resolution or any
// Reviewer launch; execution mechanics live in party.go.

type preparedBundle struct {
	bundle  model.ReviewBundle
	members []preparedReview
}

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
	subject    model.ReviewSubject
	members    []compiledSlot
}

type compiledSlot struct {
	slot    configuration.ExpandedProfile
	profile compiledProfile
	timings model.ReviewTimings
}

func (conductor *Conductor) prepareRun(selection model.RunSelection) (preparedBundle, store.BundleStore, error) {
	planned, err := conductor.planSelection(selection)
	if err != nil {
		return preparedBundle{}, nil, err
	}
	ledger, ok := conductor.store.(store.BundleStore)
	if !ok {
		return preparedBundle{}, nil, errors.New("run execution requires the SQLite ledger")
	}
	bundle, err := newPendingBundle(conductor.now().UTC(), planned)
	if err != nil {
		return preparedBundle{}, nil, err
	}
	if err := ledger.CreateReviewBundle(bundle); err != nil {
		return preparedBundle{}, nil, err
	}
	members := make([]preparedReview, 0, len(planned.members))
	for _, slot := range planned.members {
		members = append(members, preparedReview{subject: planned.subject, profile: slot.profile, timings: slot.timings, deadline: slot.profile.deadline})
	}
	return preparedBundle{bundle: bundle, members: members}, ledger, nil
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
	members, err := conductor.compileSlots(repository, resolved.selections.Expanded, resolved.effective)
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
	return plannedSelection{resolved: resolved.selections, repository: repository, subject: subject, members: members}, nil
}

// resolveSharedSubject freezes the one Review Subject every member will review,
// after preflight compiles every Profile Revision and before any launch.
func (conductor *Conductor) prepareReviewSubject(repository string, reference model.SubjectReference) (string, model.ReviewSubject, int64, error) {
	started := conductor.now().UTC()
	resolvedRepository, err := resolveSubjectRepository(repository, reference)
	if err != nil {
		return "", model.ReviewSubject{}, elapsedMilliseconds(started, conductor.now().UTC()), err
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

// resolvedRunSelection pairs the expanded selection with its reviewer policy.
type resolvedRunSelection struct {
	selections configuration.ResolvedReviews
	effective  configuration.Effective
}

func (conductor *Conductor) resolveEffectiveReviews(repository string, selection model.RunSelection) (resolvedRunSelection, error) {
	repositoryID := configuration.Repository(repository)
	resolved, err := conductor.configuration.ResolveRun(configuration.RunRequest{
		Repository: repositoryID, Profile: selection.Profile, Party: selection.Party,
	})
	if err != nil {
		return resolvedRunSelection{}, err
	}
	effective, err := conductor.configuration.Resolve(configuration.Request{Repository: repositoryID})
	if err != nil {
		return resolvedRunSelection{}, err
	}
	return resolvedRunSelection{selections: resolved, effective: effective}, nil
}

// compileSlots compiles every expanded Profile Revision so an incompatible
// Reviewer fails closed before the bundle row exists and before any launch.
func (conductor *Conductor) compileSlots(repository string, expanded []configuration.ExpandedProfile, effective configuration.Effective) ([]compiledSlot, error) {
	slots := make([]compiledSlot, 0, len(expanded))
	for _, slot := range expanded {
		compiledStarted := conductor.now().UTC()
		profile, err := conductor.compileSlotProfile(repository, slot, effective)
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

func (conductor *Conductor) compileSlotProfile(repository string, slot configuration.ExpandedProfile, effective configuration.Effective) (compiledProfile, error) {
	reference := configuration.ProfileReference{Scope: slot.Scope, Profile: slot.Profile}
	profile, found, err := conductor.configuration.ResolveProfileReference(configuration.Repository(repository), reference)
	if err != nil {
		return compiledProfile{}, fmt.Errorf("%s Profile %q: %w", slot.Scope, slot.Profile, err)
	}
	if !found {
		return compiledProfile{}, fmt.Errorf("%s Profile %q was not found", slot.Scope, slot.Profile)
	}
	resolved, err := resolvedFromProfile(profile, effective)
	if err != nil {
		return compiledProfile{}, fmt.Errorf("%s Profile %q: %w", slot.Scope, slot.Profile, err)
	}
	return conductor.compileResolvedProfile(resolved)
}

// newPendingBundle records the authored selection, the expanded execution list
// with exact Profile Revisions, deduplication facts, warnings, limit
// provenance, and a revision digest binding all of them.
func newPendingBundle(created time.Time, plan plannedSelection) (model.ReviewBundle, error) {
	id, err := newDomainID("rb", created)
	if err != nil {
		return model.ReviewBundle{}, err
	}
	bundle := model.ReviewBundle{
		ID:               model.ReviewBundleID(id),
		Description:      bundleDescription(plan),
		Revision:         selectionRevisionIdentity(plan.resolved, plan.members),
		Repository:       plan.repository,
		SubjectKind:      plan.subject.Kind,
		SubjectIdentity:  plan.subject.Identity,
		Lifecycle:        model.LifecyclePending,
		Selection:        bundleSelection(plan.resolved),
		Warnings:         bundleWarnings(plan.resolved.Warnings),
		Deduplicated:     bundleSkippedDuplicates(plan.resolved.Deduplicated),
		Members:          make([]model.BundleMember, 0, len(plan.members)),
		ConcurrencyLimit: plan.resolved.ConcurrencyLimit,
		CreatedAt:        created,
		UpdatedAt:        created,
	}
	for _, member := range plan.members {
		bundle.Members = append(bundle.Members, model.BundleMember{
			Scope:           string(member.slot.Scope),
			Profile:         member.profile.revision.Name,
			ProfileRevision: member.profile.revision.Revision,
			Origin:          member.slot.Origin,
			Lifecycle:       model.LifecyclePending,
		})
	}
	return bundle, nil
}

func bundleDescription(plan plannedSelection) string {
	if plan.resolved.Kind == configuration.SelectionExplicitParty {
		return fmt.Sprintf("explicit Party %q", plan.resolved.Authored[0].Name)
	}
	return ""
}

func bundleSelection(resolved configuration.ResolvedReviews) *model.BundleSelection {
	selection := &model.BundleSelection{
		Kind:             string(resolved.Kind),
		Source:           resolved.Source,
		ConcurrencyLimit: resolved.ConcurrencyLimit,
		LimitSource:      string(resolved.LimitSource),
	}
	for _, authored := range resolved.Authored {
		selection.Authored = append(selection.Authored, model.BundleAuthoredItem{
			Kind: string(authored.Kind), Name: authored.Name, Scope: string(authored.Scope),
		})
	}
	return selection
}

func bundleWarnings(warnings []configuration.ResolverWarning) []model.BundleWarning {
	bundled := make([]model.BundleWarning, 0, len(warnings))
	for _, warning := range warnings {
		bundled = append(bundled, model.BundleWarning{Category: string(warning.Category), Name: warning.Name, Message: warning.Message})
	}
	return bundled
}

func bundleSkippedDuplicates(skipped []configuration.SkippedProfile) []model.SkippedDuplicate {
	duplicates := make([]model.SkippedDuplicate, 0, len(skipped))
	for _, occurrence := range skipped {
		duplicates = append(duplicates, model.SkippedDuplicate{
			Scope: string(occurrence.Scope), Profile: occurrence.Profile, Origin: occurrence.Origin, KeptOrigin: occurrence.KeptOrigin,
		})
	}
	return duplicates
}

// selectionRevisionIdentity freezes the effective composition: kind,
// Concurrency Limit, and each executed slot's scoped identity plus its exact
// compiled Profile Revision. Configuration changes therefore produce a
// distinct revision instead of silently reusing recorded provenance.
func selectionRevisionIdentity(resolved configuration.ResolvedReviews, members []compiledSlot) string {
	type revisionMember struct {
		Scope           string `json:"scope"`
		Profile         string `json:"profile"`
		ProfileRevision string `json:"profile_revision"`
	}
	composition := struct {
		SchemaVersion    int              `json:"schema_version"`
		Kind             string           `json:"kind"`
		ConcurrencyLimit int              `json:"concurrency_limit"`
		LimitSource      string           `json:"limit_source"`
		Members          []revisionMember `json:"members"`
	}{SchemaVersion: 1, Kind: string(resolved.Kind), ConcurrencyLimit: resolved.ConcurrencyLimit, LimitSource: string(resolved.LimitSource)}
	for _, member := range members {
		composition.Members = append(composition.Members, revisionMember{
			Scope:           string(member.slot.Scope),
			Profile:         member.slot.Profile,
			ProfileRevision: member.profile.revision.Revision,
		})
	}
	payload, err := json.Marshal(composition)
	if err != nil {
		panic(fmt.Sprintf("encode selection Revision identity: %v", err))
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
