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
)

func (conductor *Conductor) RunParty(ctx context.Context, selection model.PartySelection) (model.ReviewBundle, error) {
	if err := ctx.Err(); err != nil {
		return model.ReviewBundle{}, err
	}
	if err := conductor.requirePreparedState(selection.Repository); err != nil {
		return model.ReviewBundle{}, err
	}
	party, ledger, err := conductor.prepareParty(selection)
	if err != nil {
		return model.ReviewBundle{}, err
	}
	if party.bundle.ConcurrencyLimit > 1 {
		return conductor.executePartyConcurrent(ctx, ledger, party)
	}
	return conductor.executePartySequential(ctx, ledger, party)
}

type preparedParty struct {
	bundle  model.ReviewBundle
	members []preparedReview
}

// effectivePartyDefinition applies explicit caller overrides on top of the
// stored definition. An override narrows every member to the explicitly
// selected Reviewer choice; it never substitutes or silently drops a choice.
func effectivePartyDefinition(definition model.PartyDefinition, selection model.PartySelection) model.PartyDefinition {
	effective := model.PartyDefinition{
		SchemaVersion:    definition.SchemaVersion,
		Name:             definition.Name,
		Description:      definition.Description,
		ConcurrencyLimit: definition.ConcurrencyLimit,
		Profiles:         append([]model.PartyMember(nil), definition.Profiles...),
	}
	if selection.ConcurrencyLimit > 0 {
		effective.ConcurrencyLimit = selection.ConcurrencyLimit
	}
	if effective.ConcurrencyLimit == 0 {
		effective.ConcurrencyLimit = 1
	}
	for index := range effective.Profiles {
		member := &effective.Profiles[index]
		if selection.Reviewer != "" {
			member.Reviewer = selection.Reviewer
		}
		if selection.Model != "" {
			member.Model = selection.Model
		}
		if selection.Effort != "" {
			member.Effort = selection.Effort
		}
	}
	return effective
}

type partyPlan struct {
	effective  model.PartyDefinition
	source     string
	repository string
	subject    ReviewSubject
	members    []compiledPartyMember
}

func (conductor *Conductor) prepareParty(selection model.PartySelection) (preparedParty, store.BundleStore, error) {
	plan, err := conductor.planParty(selection)
	if err != nil {
		return preparedParty{}, nil, err
	}
	ledger, ok := conductor.store.(store.BundleStore)
	if !ok {
		return preparedParty{}, nil, errors.New("party execution requires the SQLite ledger")
	}
	bundle, err := newPendingBundle(conductor.now().UTC(), plan)
	if err != nil {
		return preparedParty{}, nil, err
	}
	if err := ledger.CreateReviewBundle(bundle); err != nil {
		return preparedParty{}, nil, err
	}
	prepared := preparedParty{bundle: bundle, members: make([]preparedReview, 0, len(plan.members))}
	for _, member := range plan.members {
		prepared.members = append(prepared.members, preparedReview{subject: plan.subject, profile: member.profile, timings: member.timings, deadline: conductor.attemptDeadline})
	}
	return prepared, ledger, nil
}

func (conductor *Conductor) planParty(selection model.PartySelection) (partyPlan, error) {
	if selection.ConcurrencyLimit < 0 {
		return partyPlan{}, errors.New("party concurrency override must not be negative")
	}
	repository, err := resolveRepositoryRoot(selection.Repository)
	if err != nil {
		return partyPlan{}, err
	}
	selection.Repository = repository
	lookup, err := conductor.resolvePartyLookup(selection)
	if err != nil {
		return partyPlan{}, err
	}
	composed, err := conductor.composeParty(lookup)
	if err != nil {
		return partyPlan{}, err
	}
	effective := effectivePartyDefinition(composed.definition, selection)
	if err := validatePartyDefinition(effective); err != nil {
		return partyPlan{}, InvalidPartyDefinitionError{Name: effective.Name, Reason: err.Error()}
	}
	subject, subjectResolutionMS, err := conductor.resolveSharedSubject(selection.Subject, repository)
	if err != nil {
		return partyPlan{}, err
	}
	members, err := conductor.compilePartyMembers(repository, effective, subjectResolutionMS)
	if err != nil {
		return partyPlan{}, err
	}
	return partyPlan{effective: effective, source: composed.source, repository: repository, subject: subject, members: members}, nil
}

// resolvePartyLookup resolves the explicit or configured default Party through
// the same Configuration Manager that owns Profile and Reviewer precedence.
func (conductor *Conductor) resolvePartyLookup(selection model.PartySelection) (partyLookup, error) {
	if conductor.configuration == nil {
		return partyLookup{}, errors.New("party resolution requires a configuration manager")
	}
	effective, err := conductor.configuration.Resolve(configuration.Request{
		Repository: configuration.Repository(selection.Repository),
		Overrides:  configuration.Overrides{Party: selection.Name},
	})
	if err != nil {
		return partyLookup{}, err
	}
	return partyLookup{repository: selection.Repository, name: effective.DefaultParty.Value}, nil
}

// resolveSharedSubject freezes the one Review Subject every member will review,
// before any Profile Revision compiles or any harness launches.
func (conductor *Conductor) resolveSharedSubject(reference model.SubjectReference, repository string) (ReviewSubject, int64, error) {
	started := conductor.now().UTC()
	subject, err := resolveSubject(repository, reference)
	return subject, elapsedMilliseconds(started, conductor.now().UTC()), err
}

// compilePartyMembers compiles every member Profile Revision before the bundle
// row is created so an incompatible Reviewer fails closed before any launch.
func (conductor *Conductor) compilePartyMembers(repository string, effective model.PartyDefinition, subjectResolutionMS int64) ([]compiledPartyMember, error) {
	members := make([]compiledPartyMember, 0, len(effective.Profiles))
	for _, member := range effective.Profiles {
		compiledStarted := conductor.now().UTC()
		selection := ProfileSelection{Profile: member.Profile, Reviewer: member.Reviewer, Model: member.Model, Effort: member.Effort}
		profile, err := conductor.compileFilesystemProfile(selection, repository)
		if err != nil {
			return nil, fmt.Errorf("party %q member %q: %w", effective.Name, member.Profile, err)
		}
		timings := ReviewTimings{SubjectResolutionMS: subjectResolutionMS, ProfileCompilationMS: elapsedMilliseconds(compiledStarted, conductor.now().UTC())}
		members = append(members, compiledPartyMember{profile: profile, timings: timings})
	}
	return members, nil
}

type compiledPartyMember struct {
	profile compiledProfile
	timings ReviewTimings
}

func newPendingBundle(created time.Time, plan partyPlan) (model.ReviewBundle, error) {
	id, err := newDomainID("rb", created)
	if err != nil {
		return model.ReviewBundle{}, err
	}
	bundle := model.ReviewBundle{
		ID:               model.ReviewBundleID(id),
		Party:            plan.effective.Name,
		Description:      plan.effective.Description,
		Repository:       plan.repository,
		SubjectKind:      plan.subject.Kind,
		SubjectIdentity:  plan.subject.Identity,
		Lifecycle:        model.LifecyclePending,
		Members:          make([]model.BundleMember, 0, len(plan.members)),
		ConcurrencyLimit: plan.effective.ConcurrencyLimit,
		CreatedAt:        created,
		UpdatedAt:        created,
	}
	for _, member := range plan.members {
		bundle.Members = append(bundle.Members, model.BundleMember{Profile: member.profile.revision.Name, Lifecycle: model.LifecyclePending})
	}
	bundle.PartyRevision = partyRevisionIdentity(bundle, plan.effective, plan.members)
	return bundle, nil
}

// partyRevisionIdentity freezes the effective composition: stored name,
// concurrency limit, and each member's effective Reviewer choice plus its exact
// compiled Profile Revision. Caller flags therefore produce a distinct
// revision instead of silently overriding recorded provenance.
func partyRevisionIdentity(bundle model.ReviewBundle, effective model.PartyDefinition, members []compiledPartyMember) string {
	type revisionMember struct {
		Profile         string `json:"profile"`
		Reviewer        string `json:"reviewer"`
		Model           string `json:"model"`
		Effort          string `json:"effort"`
		ProfileRevision string `json:"profile_revision"`
	}
	composition := struct {
		SchemaVersion    int              `json:"schema_version"`
		Name             string           `json:"name"`
		ConcurrencyLimit int              `json:"concurrency_limit"`
		Members          []revisionMember `json:"members"`
	}{SchemaVersion: effective.SchemaVersion, Name: effective.Name, ConcurrencyLimit: bundle.ConcurrencyLimit}
	for index, member := range effective.Profiles {
		compiled := members[index].profile.revision
		composition.Members = append(composition.Members, revisionMember{
			Profile:         member.Profile,
			Reviewer:        compiled.ReviewerID,
			Model:           compiled.Model,
			Effort:          compiled.Effort,
			ProfileRevision: compiled.Revision,
		})
	}
	payload, err := json.Marshal(composition)
	if err != nil {
		panic(fmt.Sprintf("encode Party Revision identity: %v", err))
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func (conductor *Conductor) executePartySequential(ctx context.Context, ledger store.BundleStore, party preparedParty) (model.ReviewBundle, error) {
	bundle := party.bundle
	bundle.Lifecycle = model.LifecycleRunning
	bundle.UpdatedAt = conductor.now().UTC()
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		return conductor.stopParty(ledger, &bundle, evalFailureCategory(err), err)
	}
	for index := range party.members {
		if err := ctx.Err(); err != nil {
			return conductor.stopParty(ledger, &bundle, evalFailureCategory(err), err)
		}
		record, err := conductor.runPreparedReview(ctx, party.members[index], nil, conductor.now().UTC())
		result := concurrentPartyResult{index: index, record: record, err: err}
		next, hardErr := conductor.absorbPartyMember(ledger, bundle, result)
		bundle = next
		if hardErr != nil {
			return conductor.stopParty(ledger, &bundle, evalFailureCategory(hardErr), hardErr)
		}
	}
	return conductor.finalizeParty(ledger, bundle)
}

type concurrentPartyResult struct {
	index  int
	record ReviewRecord
	err    error
}

func (conductor *Conductor) executePartyConcurrent(ctx context.Context, ledger store.BundleStore, party preparedParty) (model.ReviewBundle, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	runContext = context.WithValue(runContext, attemptGateContextKey{}, make(chan struct{}, party.bundle.ConcurrencyLimit))
	bundle := party.bundle
	bundle.Lifecycle = model.LifecycleRunning
	bundle.UpdatedAt = conductor.now().UTC()
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		return conductor.stopParty(ledger, &bundle, evalFailureCategory(err), err)
	}
	results := make(chan concurrentPartyResult, len(party.members))
	launched, launchErr := conductor.launchPartyMembers(runContext, party, results)
	if launchErr != nil {
		cancel()
		bundle = conductor.absorbPendingPartyResults(ledger, bundle, results, launched)
		return conductor.stopParty(ledger, &bundle, evalFailureCategory(launchErr), launchErr)
	}
	consumed := 0
	for consumed < launched {
		result := <-results
		consumed++
		next, hardErr := conductor.absorbPartyMember(ledger, bundle, result)
		bundle = next
		if hardErr != nil {
			cancel()
			bundle = conductor.absorbPendingPartyResults(ledger, bundle, results, launched-consumed)
			return conductor.stopParty(ledger, &bundle, evalFailureCategory(hardErr), hardErr)
		}
	}
	return conductor.finalizeParty(ledger, bundle)
}

func (conductor *Conductor) launchPartyMembers(ctx context.Context, party preparedParty, results chan<- concurrentPartyResult) (int, error) {
	started := 0
	for index := range party.members {
		if err := ctx.Err(); err != nil {
			return started, err
		}
		started++
		go func(index int, prepared preparedReview) {
			record, err := conductor.runPreparedReview(ctx, prepared, nil, conductor.now().UTC())
			results <- concurrentPartyResult{index: index, record: record, err: err}
		}(index, party.members[index])
	}
	return started, nil
}

// absorbPendingPartyResults drains results of members that were already
// launched when a hard stop happened, so every persisted child Review stays
// linked in the bundle instead of being orphaned as a pending member.
func (conductor *Conductor) absorbPendingPartyResults(ledger store.BundleStore, bundle model.ReviewBundle, results chan concurrentPartyResult, pending int) model.ReviewBundle {
	for drained := 0; drained < pending; drained++ {
		result := <-results
		next, _ := conductor.absorbPartyMember(ledger, bundle, result)
		bundle = next
	}
	return bundle
}

// absorbPartyMember records one terminal member outcome on the bundle. A child
// Incomplete lifecycle is an honest member outcome; only persistence or
// cancellation-class errors are hard failures that stop remaining work.
func (conductor *Conductor) absorbPartyMember(ledger store.BundleStore, bundle model.ReviewBundle, result concurrentPartyResult) (model.ReviewBundle, error) {
	member := model.BundleMember{Profile: bundle.Members[result.index].Profile, ReviewID: result.record.ID, Lifecycle: result.record.Lifecycle}
	if result.record.Result != nil {
		member.Status = string(result.record.Result.Status)
		member.FindingCount = result.record.Result.FindingCount()
	}
	bundle.Members[result.index] = member
	bundle.UpdatedAt = conductor.now().UTC()
	if saveErr := ledger.SaveReviewBundle(bundle); saveErr != nil {
		return bundle, errors.Join(result.err, saveErr)
	}
	return bundle, result.err
}

func (conductor *Conductor) finalizeParty(ledger store.BundleStore, bundle model.ReviewBundle) (model.ReviewBundle, error) {
	bundle.CompletedAt = conductor.now().UTC()
	bundle.UpdatedAt = bundle.CompletedAt
	bundle.Lifecycle = model.LifecycleCompleted
	for _, member := range bundle.Members {
		if member.ReviewID == "" || member.Lifecycle != LifecycleCompleted {
			bundle.Lifecycle = model.LifecycleIncomplete
			break
		}
	}
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		return bundle, err
	}
	return bundle, nil
}

func (conductor *Conductor) stopParty(ledger store.BundleStore, bundle *model.ReviewBundle, category model.TerminationCategory, cause error) (model.ReviewBundle, error) {
	bundle.Lifecycle = model.LifecycleIncomplete
	bundle.Termination = &model.BundleTermination{Category: category, Message: cause.Error()}
	bundle.CompletedAt = conductor.now().UTC()
	bundle.UpdatedAt = bundle.CompletedAt
	if err := ledger.SaveReviewBundle(*bundle); err != nil {
		return *bundle, errors.Join(cause, err)
	}
	return *bundle, cause
}

func (conductor *Conductor) InspectBundle(_ context.Context, id model.ReviewBundleID) (model.ReviewBundle, error) {
	ledger, ok := conductor.store.(store.BundleStore)
	if !ok {
		return model.ReviewBundle{}, errors.New("bundle inspection requires the SQLite ledger")
	}
	bundle, err := ledger.LoadReviewBundle(id)
	if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
		return model.ReviewBundle{}, InitializationRequiredError{Repository: "."}
	}
	return bundle, err
}
