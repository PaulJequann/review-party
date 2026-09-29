package engine

import (
	"context"
	"time"

	"reviewparty/internal/model"
)

// Live run progress: one event per Review lifecycle transition while a Review
// selection executes. Every member reports pending before any member starts,
// then started, each attempt, and finished. The Conductor owns the sink;
// bundle execution and explicit Profile reviews cross runReviewWithProgress so
// progress stays uniform across sequential and concurrent members. Concurrent
// selections admit members through a progress gate so a member reports
// started only when the concurrency limit actually starts it, never while it
// waits in the launch queue. Eval and Replay keep the silent runPreparedReview
// seam.

// emitRunProgress reports one progress fact to the configured sink. A nil sink
// keeps execution fully silent.
func (conductor *Conductor) emitRunProgress(event model.RunProgressEvent) {
	if conductor.progress == nil {
		return
	}
	conductor.progress(event)
}

// memberProgress identifies one Review in every progress event it emits.
type memberProgress struct {
	bundleID model.ReviewBundleID
	reviewID model.ReviewID
	scope    string
	index    int
	total    int
	revision model.ProfileRevision
}

// bundleMemberProgress identifies the member at index. record supplies the
// Review ID and Profile Revision, so a stopped member that never launched
// reports the same identity as one that ran.
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

// finished reports the Review's outcome. A hard execution error wins over the
// recorded termination because it explains why the selection stopped.
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
		event.Message = err.Error()
	case record.Termination != nil:
		event.Category = record.Termination.Category
		event.Message = record.Termination.Message
	}
	return event
}

// runReviewWithProgress executes one pending Review and reports its start,
// each attempt, and its outcome. started is the Review timing origin so the
// reported elapsed time matches the persisted Review Record. When the context
// carries a progress gate, the start event waits for admission so queued
// members stay silent until the concurrency limit starts them.
func (conductor *Conductor) runReviewWithProgress(ctx context.Context, member pendingReview, progress memberProgress, started time.Time) (model.ReviewRecord, error) {
	runner := conductor.getRunner()
	release, admitted := acquireProgressGate(ctx)
	if !admitted {
		// The run was cancelled while this member waited for admission; the
		// execution path records the cancellation without a started line.
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
