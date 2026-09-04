package engine

import (
	"context"
	"time"

	"reviewparty/internal/model"
)

// Live run progress: optional per-reviewer events emitted while a Review
// selection executes. The Conductor owns the sink; bundle execution and
// explicit Profile reviews cross runReviewWithProgress so progress lines stay
// uniform across sequential and concurrent members. Eval and Replay keep the
// silent runPreparedReview seam.

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
// so the reported elapsed time matches the persisted Review Record.
func (conductor *Conductor) runReviewWithProgress(ctx context.Context, member preparedReview, scope string, index, total int, started time.Time) (model.ReviewRecord, error) {
	conductor.emitRunProgress(reviewProgressStarted(member, scope, index, total))
	record, err := conductor.runPreparedReview(ctx, member, nil, started)
	conductor.emitRunProgress(conductor.reviewProgressFinished(member, record, err, scope, index, total, started))
	return record, err
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
