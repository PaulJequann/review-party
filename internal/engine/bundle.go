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

// This file owns Review Bundle construction, execution, persistence, and
// inspection. Selection resolution and Profile compilation remain in run.go.

type preparedBundle struct {
	bundle  model.ReviewBundle
	members []preparedReview
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

// newPendingBundle records the authored selection, expanded execution list,
// Profile Revisions, provenance, warnings, and concurrency-limit facts before
// the first member launches.
func newPendingBundle(created time.Time, plan plannedSelection) (model.ReviewBundle, error) {
	id, err := newDomainID("rb", created)
	if err != nil {
		return model.ReviewBundle{}, err
	}
	bundle := model.ReviewBundle{
		ID: model.ReviewBundleID(id), Description: bundleDescription(plan),
		Revision: selectionRevisionIdentity(plan.resolved, plan.members), Repository: plan.repository,
		SubjectKind: plan.subject.Kind, SubjectIdentity: plan.subject.Identity,
		Lifecycle: model.LifecyclePending, Selection: bundleSelection(plan.resolved),
		Warnings: bundleWarnings(plan.resolved.Warnings), Deduplicated: bundleSkippedDuplicates(plan.resolved.Deduplicated),
		Members: make([]model.BundleMember, 0, len(plan.members)), ConcurrencyLimit: plan.resolved.ConcurrencyLimit,
		CreatedAt: created, UpdatedAt: created,
	}
	for _, member := range plan.members {
		bundle.Members = append(bundle.Members, model.BundleMember{
			Scope: string(member.slot.Scope), Profile: member.profile.revision.Name,
			ProfileRevision: member.profile.revision.Revision, Origin: member.slot.Origin,
			Lifecycle: model.LifecyclePending,
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
	selection := &model.BundleSelection{Kind: string(resolved.Kind), Source: resolved.Source, ConcurrencyLimit: resolved.ConcurrencyLimit, LimitSource: string(resolved.LimitSource)}
	for _, authored := range resolved.Authored {
		selection.Authored = append(selection.Authored, model.BundleAuthoredItem{Kind: string(authored.Kind), Name: authored.Name, Scope: string(authored.Scope)})
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
		duplicates = append(duplicates, model.SkippedDuplicate{Scope: string(occurrence.Scope), Profile: occurrence.Profile, Origin: occurrence.Origin, KeptOrigin: occurrence.KeptOrigin})
	}
	return duplicates
}

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
		composition.Members = append(composition.Members, revisionMember{Scope: string(member.slot.Scope), Profile: member.slot.Profile, ProfileRevision: member.profile.revision.Revision})
	}
	payload, err := json.Marshal(composition)
	if err != nil {
		panic(fmt.Sprintf("encode selection Revision identity: %v", err))
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

type concurrentMemberResult struct {
	index  int
	record model.ReviewRecord
	err    error
}

func (conductor *Conductor) executePreparedBundle(ctx context.Context, ledger store.BundleStore, prepared preparedBundle) (model.ReviewBundle, error) {
	if prepared.bundle.ConcurrencyLimit > 1 {
		return conductor.executeBundleConcurrent(ctx, ledger, prepared)
	}
	return conductor.executeBundleSequential(ctx, ledger, prepared)
}

func (conductor *Conductor) executeBundleSequential(ctx context.Context, ledger store.BundleStore, prepared preparedBundle) (model.ReviewBundle, error) {
	bundle := prepared.bundle
	bundle.Lifecycle = model.LifecycleRunning
	bundle.UpdatedAt = conductor.now().UTC()
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		return conductor.stopBundle(ledger, &bundle, evalFailureCategory(err), err)
	}
	for index := range prepared.members {
		if err := ctx.Err(); err != nil {
			return conductor.stopBundle(ledger, &bundle, evalFailureCategory(err), err)
		}
		record, err := conductor.runPreparedReview(ctx, prepared.members[index], nil, conductor.now().UTC())
		var hardErr error
		bundle, hardErr = conductor.absorbBundleMember(ledger, bundle, concurrentMemberResult{index: index, record: record, err: err})
		if hardErr != nil {
			return conductor.stopBundle(ledger, &bundle, evalFailureCategory(hardErr), hardErr)
		}
	}
	return conductor.finalizeBundle(ledger, bundle)
}

func (conductor *Conductor) executeBundleConcurrent(ctx context.Context, ledger store.BundleStore, prepared preparedBundle) (model.ReviewBundle, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	runContext = context.WithValue(runContext, attemptGateContextKey{}, make(chan struct{}, prepared.bundle.ConcurrencyLimit))
	bundle := prepared.bundle
	bundle.Lifecycle = model.LifecycleRunning
	bundle.UpdatedAt = conductor.now().UTC()
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		return conductor.stopBundle(ledger, &bundle, evalFailureCategory(err), err)
	}
	results := make(chan concurrentMemberResult, len(prepared.members))
	launched, launchErr := conductor.launchBundleMembers(runContext, prepared, results)
	if launchErr != nil {
		cancel()
		var absorbErr error
		bundle, absorbErr = conductor.absorbPendingBundleResults(ledger, bundle, results, launched)
		cause := errors.Join(launchErr, absorbErr)
		return conductor.stopBundle(ledger, &bundle, evalFailureCategory(cause), cause)
	}
	consumed := 0
	for consumed < launched {
		result := <-results
		consumed++
		var hardErr error
		bundle, hardErr = conductor.absorbBundleMember(ledger, bundle, result)
		if hardErr != nil {
			cancel()
			var absorbErr error
			bundle, absorbErr = conductor.absorbPendingBundleResults(ledger, bundle, results, launched-consumed)
			cause := errors.Join(hardErr, absorbErr)
			return conductor.stopBundle(ledger, &bundle, evalFailureCategory(cause), cause)
		}
	}
	return conductor.finalizeBundle(ledger, bundle)
}

func (conductor *Conductor) launchBundleMembers(ctx context.Context, prepared preparedBundle, results chan<- concurrentMemberResult) (int, error) {
	started := 0
	for index := range prepared.members {
		if err := ctx.Err(); err != nil {
			return started, err
		}
		started++
		go func(index int, member preparedReview) {
			record, err := conductor.runPreparedReview(ctx, member, nil, conductor.now().UTC())
			results <- concurrentMemberResult{index: index, record: record, err: err}
		}(index, prepared.members[index])
	}
	return started, nil
}

func (conductor *Conductor) absorbPendingBundleResults(ledger store.BundleStore, bundle model.ReviewBundle, results chan concurrentMemberResult, pending int) (model.ReviewBundle, error) {
	var absorbErr error
	for drained := 0; drained < pending; drained++ {
		result := <-results
		var err error
		bundle, err = conductor.absorbBundleMember(ledger, bundle, result)
		absorbErr = errors.Join(absorbErr, err)
	}
	return bundle, absorbErr
}

func (conductor *Conductor) absorbBundleMember(ledger store.BundleStore, bundle model.ReviewBundle, result concurrentMemberResult) (model.ReviewBundle, error) {
	member := bundle.Members[result.index]
	member.ReviewID = result.record.ID
	member.Lifecycle = result.record.Lifecycle
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

func (conductor *Conductor) finalizeBundle(ledger store.BundleStore, bundle model.ReviewBundle) (model.ReviewBundle, error) {
	bundle.CompletedAt = conductor.now().UTC()
	bundle.UpdatedAt = bundle.CompletedAt
	bundle.Lifecycle = model.LifecycleCompleted
	for _, member := range bundle.Members {
		if member.ReviewID == "" || member.Lifecycle != model.LifecycleCompleted {
			bundle.Lifecycle = model.LifecycleIncomplete
			break
		}
	}
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		return bundle, err
	}
	return bundle, nil
}

func (conductor *Conductor) stopBundle(ledger store.BundleStore, bundle *model.ReviewBundle, category model.TerminationCategory, cause error) (model.ReviewBundle, error) {
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
