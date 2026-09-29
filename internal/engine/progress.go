package engine

import (
	"context"
	"time"

	"reviewparty/internal/model"
)

// emitRunProgress reports one progress fact to the configured sink. A nil sink
// keeps execution fully silent.
func (conductor *Conductor) emitRunProgress(event model.RunProgressEvent) {
	if conductor.progress == nil {
		return
	}
	conductor.progress(event)
}

type memberProgress struct {
	bundleID model.ReviewBundleID
	reviewID model.ReviewID
	scope    string
	index    int
	total    int
	revision model.ProfileRevision
}

func bundleMemberProgress(bundle model.ReviewBundle, index int, record model.ReviewRecord) memberProgress {
	return memberProgress{
		bundleID: bundle.ID, reviewID: record.ID, scope: bundleMemberScope(bundle, index),
		index: index, total: len(bundle.Members), revision: record.ProfileRevision,
	}
}

func (member memberProgress) event(kind model.RunProgressKind) model.RunProgressEvent {
	return model.RunProgressEvent{
		Kind: kind, BundleID: member.bundleID, ReviewID: member.reviewID,
		Index: member.index, Total: member.total, Scope: member.scope,
		Profile: member.revision.Name, Reviewer: member.revision.ReviewerID, Model: member.revision.Model,
	}
}

func (member memberProgress) finished(record model.ReviewRecord, err error, elapsedMS int64) model.RunProgressEvent {
	event := member.event(model.RunProgressFinished)
	event.Lifecycle = record.Lifecycle
	event.ElapsedMS = elapsedMS
	if record.Result != nil {
		event.Status = string(record.Result.Status)
		event.FindingCount = record.Result.FindingCount()
	}
	switch {
	case err != nil:
		event.Error = err.Error()
	case record.Termination != nil:
		event.Category = record.Termination.Category
		event.Message = record.Termination.Message
	}
	return event
}

func (conductor *Conductor) runReviewWithProgress(ctx context.Context, member pendingReview, progress memberProgress, started time.Time) (model.ReviewRecord, error) {
	runner := conductor.getRunner()
	release, admitted := acquireProgressGate(ctx)
	if !admitted {
		record, err := runner.runPendingReview(ctx, member.record, member.prepared, started, nil)
		conductor.emitRunProgress(progress.finished(record, err, elapsedMilliseconds(started, conductor.now().UTC())))
		return record, err
	}
	defer release()
	conductor.emitRunProgress(progress.event(model.RunProgressStarted))
	record, err := runner.runPendingReview(ctx, member.record, member.prepared, started, func(number int) {
		event := progress.event(model.RunProgressAttempt)
		event.Attempt = number
		conductor.emitRunProgress(event)
	})
	conductor.emitRunProgress(progress.finished(record, err, elapsedMilliseconds(started, conductor.now().UTC())))
	return record, err
}

// progressGateContextKey marks the progress admission gate on a run context.
// It is deliberately a separate channel from the attempt gate: a member must
// never wait on the same channel that guards its own execution slot, or the
// two gated phases would deadlock at a limit of one.
type progressGateContextKey struct{}

func progressGateFromContext(ctx context.Context) chan struct{} {
	gate, ok := ctx.Value(progressGateContextKey{}).(chan struct{})
	if !ok {
		return nil
	}
	return gate
}

// acquireProgressGate waits until the selection's concurrency limit admits
// this member, so its running line coincides with real execution instead of
// queue time. It reports whether admission happened; the returned func
// releases the slot. Without a gate the member is admitted immediately.
func acquireProgressGate(ctx context.Context) (func(), bool) {
	gate := progressGateFromContext(ctx)
	if gate == nil {
		return func() {}, true
	}
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, true
	case <-ctx.Done():
		return func() {}, false
	}
}
