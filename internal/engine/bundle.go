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
	members []pendingReview
}

// pendingReview pairs a prepared member with its persisted pending Review
// Record, so the member's Review ID exists before any member launches.
type pendingReview struct {
	prepared preparedReview
	record   model.ReviewRecord
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
	members := make([]pendingReview, 0, len(planned.members))
	records := make([]model.ReviewRecord, 0, len(planned.members))
	for index, slot := range planned.members {
		prepared := planned.preparedSubject.review(slot.profile, slot.timings)
		record, err := conductor.getRunner().pendingRecord(prepared, nil)
		if err != nil {
			return preparedBundle{}, nil, err
		}
		bundle.Members[index].ReviewID = record.ID
		members = append(members, pendingReview{prepared: prepared, record: record})
		records = append(records, record)
	}
	if err := ledger.CreateReviewBundle(bundle, records); err != nil {
		return preparedBundle{}, nil, err
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
		SubjectKind: plan.preparedSubject.value.Kind, SubjectIdentity: plan.preparedSubject.value.Identity,
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
		return conductor.stopBundle(ledger, &bundle, err)
	}
	for index := range prepared.members {
		if err := ctx.Err(); err != nil {
			return conductor.stopBundle(ledger, &bundle, err)
		}
		record, err := conductor.runReviewWithProgress(ctx, prepared.members[index], bundleMemberScope(bundle, index), index, len(prepared.members), conductor.now().UTC())
		var hardErr error
		bundle, hardErr = conductor.absorbBundleMember(ledger, bundle, concurrentMemberResult{index: index, record: record, err: err})
		if hardErr != nil {
			return conductor.stopBundle(ledger, &bundle, hardErr)
		}
	}
	return conductor.finalizeBundle(ledger, bundle)
}

func (conductor *Conductor) executeBundleConcurrent(ctx context.Context, ledger store.BundleStore, prepared preparedBundle) (model.ReviewBundle, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	runContext = context.WithValue(runContext, attemptGateContextKey{}, make(chan struct{}, prepared.bundle.ConcurrencyLimit))
	// Progress admission mirrors the attempt limit so a queued member reports
	// running only when the limit actually starts its execution.
	runContext = context.WithValue(runContext, progressGateContextKey{}, make(chan struct{}, prepared.bundle.ConcurrencyLimit))
	bundle := prepared.bundle
	bundle.Lifecycle = model.LifecycleRunning
	bundle.UpdatedAt = conductor.now().UTC()
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		return conductor.stopBundle(ledger, &bundle, err)
	}
	results := make(chan concurrentMemberResult, len(prepared.members))
	launched, launchErr := conductor.launchBundleMembers(runContext, prepared, results)
	if launchErr != nil {
		cancel()
		var absorbErr error
		bundle, absorbErr = conductor.absorbPendingBundleResults(ledger, bundle, results, launched)
		cause := errors.Join(launchErr, absorbErr)
		return conductor.stopBundle(ledger, &bundle, cause)
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
			return conductor.stopBundle(ledger, &bundle, cause)
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
		go func(index int, member pendingReview) {
			record, err := conductor.runReviewWithProgress(ctx, member, bundleMemberScope(prepared.bundle, index), index, len(prepared.members), conductor.now().UTC())
			results <- concurrentMemberResult{index: index, record: record, err: err}
		}(index, prepared.members[index])
	}
	return started, nil
}

// bundleMemberScope names the selection scope at index. An explicit Profile or
// Party run reports the "explicit" origin; saved selections report the member's
// authored scope. The renderer combines it with the Profile name.
func bundleMemberScope(bundle model.ReviewBundle, index int) string {
	if index < 0 || index >= len(bundle.Members) {
		return ""
	}
	member := bundle.Members[index]
	if member.Origin == "explicit" {
		return member.Origin
	}
	return member.Scope
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
	mirrorBundleMember(&bundle.Members[result.index], result.record)
	bundle.UpdatedAt = conductor.now().UTC()
	if saveErr := ledger.SaveReviewBundle(bundle); saveErr != nil {
		return bundle, errors.Join(result.err, saveErr)
	}
	return bundle, result.err
}

func mirrorBundleMember(member *model.BundleMember, record model.ReviewRecord) {
	member.Lifecycle = record.Lifecycle
	if record.Result != nil {
		member.Status = string(record.Result.Status)
		member.FindingCount = record.Result.FindingCount()
	}
}

func (conductor *Conductor) finalizeBundle(ledger store.BundleStore, bundle model.ReviewBundle) (model.ReviewBundle, error) {
	bundle.CompletedAt = conductor.now().UTC()
	bundle.UpdatedAt = bundle.CompletedAt
	bundle.Lifecycle = model.LifecycleCompleted
	for _, member := range bundle.Members {
		if member.Lifecycle != model.LifecycleCompleted {
			bundle.Lifecycle = model.LifecycleIncomplete
			break
		}
	}
	if err := ledger.SaveReviewBundle(bundle); err != nil {
		return bundle, err
	}
	return bundle, nil
}

// stopBundle ends the bundle as incomplete. Callers invoke it only after every
// launched member has returned, so no member Review is still executing.
func (conductor *Conductor) stopBundle(ledger store.BundleStore, bundle *model.ReviewBundle, cause error) (model.ReviewBundle, error) {
	category := evalFailureCategory(cause)
	memberErr := conductor.finishStoppedMembers(bundle, category, cause)
	bundle.Lifecycle = model.LifecycleIncomplete
	bundle.Termination = &model.BundleTermination{Category: category, Message: cause.Error()}
	bundle.CompletedAt = conductor.now().UTC()
	bundle.UpdatedAt = bundle.CompletedAt
	if err := ledger.SaveReviewBundle(*bundle); err != nil {
		return *bundle, errors.Join(cause, memberErr, err)
	}
	return *bundle, errors.Join(cause, memberErr)
}

// finishStoppedMembers moves every member Review the stop left pending or
// running to incomplete, so each member ID reaches a terminal lifecycle that
// inspect, status, and wait can report. It mirrors the ledger into the bundle.
func (conductor *Conductor) finishStoppedMembers(bundle *model.ReviewBundle, category model.TerminationCategory, cause error) error {
	var failures error
	for index := range bundle.Members {
		record, err := conductor.store.Load(bundle.Members[index].ReviewID)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if record.Lifecycle == model.LifecyclePending || record.Lifecycle == model.LifecycleRunning {
			record, err = conductor.finishStoppedMember(record, category, cause)
			failures = errors.Join(failures, err)
		}
		mirrorBundleMember(&bundle.Members[index], record)
	}
	return failures
}

// finishStoppedMember records why a member never finished. A pending member
// never reached its availability check, so it ends there with no elapsed time;
// a running member was stopped during execution.
func (conductor *Conductor) finishStoppedMember(record model.ReviewRecord, category model.TerminationCategory, cause error) (model.ReviewRecord, error) {
	termination := model.ReviewTermination{Category: category, Phase: model.PhaseAvailabilityCheck, Message: "the Review Bundle stopped before this Review finished: " + cause.Error()}
	started := conductor.now().UTC()
	if record.Lifecycle == model.LifecycleRunning {
		termination.Phase = model.PhaseReviewerExecution
		started = record.UpdatedAt
	}
	return conductor.getRunner().finishIncomplete(record, termination, started)
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
