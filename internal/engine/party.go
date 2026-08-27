package engine

import (
	"context"
	"errors"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

// This file owns Review Bundle execution mechanics for every resolved
// selection: sequential and concurrent member launches, absorption of member
// outcomes, lifecycle finalization, and bundle inspection. Resolution-driven
// planning lives in run.go.

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
		result := concurrentMemberResult{index: index, record: record, err: err}
		next, hardErr := conductor.absorbBundleMember(ledger, bundle, result)
		bundle = next
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
		bundle = conductor.absorbPendingBundleResults(ledger, bundle, results, launched)
		return conductor.stopBundle(ledger, &bundle, evalFailureCategory(launchErr), launchErr)
	}
	consumed := 0
	for consumed < launched {
		result := <-results
		consumed++
		next, hardErr := conductor.absorbBundleMember(ledger, bundle, result)
		bundle = next
		if hardErr != nil {
			cancel()
			bundle = conductor.absorbPendingBundleResults(ledger, bundle, results, launched-consumed)
			return conductor.stopBundle(ledger, &bundle, evalFailureCategory(hardErr), hardErr)
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

// absorbPendingBundleResults drains results of members that were already
// launched when a hard stop happened, so every persisted child Review stays
// linked in the bundle instead of being orphaned as a pending member.
func (conductor *Conductor) absorbPendingBundleResults(ledger store.BundleStore, bundle model.ReviewBundle, results chan concurrentMemberResult, pending int) model.ReviewBundle {
	for drained := 0; drained < pending; drained++ {
		result := <-results
		next, _ := conductor.absorbBundleMember(ledger, bundle, result)
		bundle = next
	}
	return bundle
}

// absorbBundleMember records one terminal member outcome on the bundle. A child
// Incomplete lifecycle is an honest member outcome; only persistence or
// cancellation-class errors are hard failures that stop remaining work. The
// member's scoped identity, origin, and Profile Revision survive the update.
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
