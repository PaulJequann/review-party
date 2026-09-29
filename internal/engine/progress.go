package engine

import (
	"context"
	"time"

	"reviewparty/internal/model"
)

// Live run progress: optional per-reviewer events emitted while a Review
// selection executes. The Conductor owns the sink; bundle execution and
// explicit Profile reviews cross runReviewWithProgress so progress lines stay
// uniform across sequential and concurrent members. Concurrent selections
// admit members through a progress gate so a member reports running only when
// the concurrency limit actually starts it, never while it waits in the
// launch queue. Eval and Replay keep the silent runPreparedReview seam.

// emitRunProgress reports one progress fact to the configured sink. A nil sink
// keeps execution fully silent.
func (conductor *Conductor) emitRunProgress(event model.RunProgressEvent) {
	if conductor.progress == nil {
		return
	}
	conductor.progress(event)
}

// runReviewWithProgress executes one prepared Review and reports its start and
// completion as live progress events. scope is the bare selection scope the
// renderer combines with the Profile name; index is the zero-based member
// ordinal within the total selection, and started is the Review timing origin
// so the reported elapsed time matches the persisted Review Record. When the
// context carries a progress gate, the start event waits for admission so
// queued members stay silent until the concurrency limit starts them.
func (conductor *Conductor) runReviewWithProgress(ctx context.Context, member pendingReview, scope string, index, total int, started time.Time) (model.ReviewRecord, error) {
	runner := conductor.getRunner()
	release, admitted := acquireProgressGate(ctx)
	if !admitted {
		// The run was cancelled while this member waited for admission; the
		// execution path below records the cancellation without a running line.
		return runner.runPendingReview(ctx, member.record, member.prepared, started)
	}
	defer release()
	conductor.emitRunProgress(reviewProgressStarted(member.prepared, scope, index, total))
	record, err := runner.runPendingReview(ctx, member.record, member.prepared, started)
	conductor.emitRunProgress(conductor.reviewProgressFinished(member.prepared, record, err, scope, index, total, started))
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

// reviewProgressStarted builds the start event for one prepared member.
func reviewProgressStarted(member preparedReview, scope string, index, total int) model.RunProgressEvent {
	revision := member.profile.revision
	return model.RunProgressEvent{
		Kind: model.RunProgressStarted, Index: index, Total: total, Scope: scope,
		Profile: revision.Name, Reviewer: revision.ReviewerID, Model: revision.Model,
	}
}

// reviewProgressFinished builds the completion event for one executed member.
// A hard execution error wins over the recorded termination because it
// explains why the selection stopped.
func (conductor *Conductor) reviewProgressFinished(member preparedReview, record model.ReviewRecord, err error, scope string, index, total int, started time.Time) model.RunProgressEvent {
	revision := member.profile.revision
	event := model.RunProgressEvent{
		Kind: model.RunProgressFinished, Index: index, Total: total, Scope: scope,
		Profile: revision.Name, Reviewer: revision.ReviewerID, Model: revision.Model,
		ReviewID: record.ID, Lifecycle: record.Lifecycle,
		ElapsedMS: elapsedMilliseconds(started, conductor.now().UTC()),
	}
	if record.Result != nil {
		event.Status = string(record.Result.Status)
		event.FindingCount = record.Result.FindingCount()
	}
	switch {
	case err != nil:
		event.Message = err.Error()
	case record.Termination != nil:
		event.Message = string(record.Termination.Category) + ": " + record.Termination.Message
	}
	return event
}
