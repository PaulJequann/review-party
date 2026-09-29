package engine

import (
	"context"
	"errors"
	mathrand "math/rand/v2"
	"time"

	"reviewparty/internal/model"
)

func (conductor *Conductor) reviewEvalCase(ctx context.Context, selection model.ReviewSelection, policy model.RetryPolicy) (model.ReviewRecord, error) {
	prepared, started, err := conductor.prepareEvalReview(ctx, selection, policy)
	if err != nil {
		return model.ReviewRecord{}, err
	}
	record, err := conductor.runPreparedReview(ctx, prepared, nil, started)
	record, err = conductor.recordAvailabilityFailure(record, prepared, err)
	execution := retryReviewExecution{context: ctx, prepared: prepared, started: started, policy: policy}
	for attempt := 1; shouldRetryEval(record, err, attempt, policy); attempt++ {
		execution.attempt = attempt
		record, err = conductor.retryEvalReview(execution, record)
	}
	return record, err
}

type retryReviewExecution struct {
	context  context.Context
	prepared preparedReview
	started  time.Time
	policy   model.RetryPolicy
	attempt  int
}

func (conductor *Conductor) prepareEvalReview(ctx context.Context, selection model.ReviewSelection, policy model.RetryPolicy) (preparedReview, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return preparedReview{}, time.Time{}, err
	}
	if err := conductor.requirePreparedState(selection.Repository); err != nil {
		return preparedReview{}, time.Time{}, err
	}
	started := conductor.now().UTC()
	timings := model.ReviewTimings{}
	preparation, err := conductor.startReviewPreparation(selection.Repository, selection.Subject)
	if err != nil {
		return preparedReview{}, time.Time{}, err
	}
	profileStarted := conductor.now().UTC()
	profile, err := conductor.compileExperimentProfile(selection.ProfileSelection(), preparation.repository)
	timings.ProfileCompilationMS = elapsedMilliseconds(profileStarted, conductor.now().UTC())
	if err != nil {
		return preparedReview{}, time.Time{}, err
	}
	preparedSubject, err := conductor.completeReviewPreparation(preparation)
	if err != nil {
		return preparedReview{}, time.Time{}, err
	}
	profile.revision.AttemptLimit = policy.MaxAttempts
	return preparedSubject.review(profile, timings), started, nil
}

func shouldRetryEval(record model.ReviewRecord, err error, attempts int, policy model.RetryPolicy) bool {
	return err == nil && record.Lifecycle == model.LifecycleIncomplete && attempts < policy.MaxAttempts && retryableTermination(record.Termination)
}

func (conductor *Conductor) retryEvalReview(execution retryReviewExecution, record model.ReviewRecord) (model.ReviewRecord, error) {
	providerDelay := time.Duration(lastRetryAfterMS(record)) * time.Millisecond
	if err := conductor.wait(execution.context, conductor.retryDelay(execution.policy, execution.attempt, providerDelay)); err != nil {
		return record, err
	}
	// Salvaged partial evidence survives retries: a later attempt may still
	// produce a complete replacement, but if none does, the retained partial
	// is what the Review and its exported adjudication report. Only the failed
	// attempt's termination state is reset while the retry runs.
	retained := record.Result
	if retained != nil && retained.Status != model.ResultFindingsPartial {
		retained = nil
	}
	record.Lifecycle, record.Termination = model.LifecycleRunning, nil
	record.Result = nil
	record.UpdatedAt = conductor.now().UTC()
	if err := conductor.store.Save(record); err != nil {
		return record, err
	}
	next, err := conductor.getRunner().resumePreparedReview(execution.context, record, execution.prepared, execution.started)
	next, resumeErr := conductor.recordAvailabilityFailure(next, execution.prepared, err)
	if next.Lifecycle != model.LifecycleCompleted && next.Result == nil && retained != nil {
		next.Result = retained
		next.UpdatedAt = conductor.now().UTC()
		if saveErr := conductor.store.Save(next); saveErr != nil {
			return next, errors.Join(resumeErr, saveErr)
		}
	}
	return next, resumeErr
}

func (conductor *Conductor) recordAvailabilityFailure(record model.ReviewRecord, prepared preparedReview, err error) (model.ReviewRecord, error) {
	if err != nil || !isUnrecordedAvailabilityFailure(record, prepared) {
		return record, err
	}
	now := conductor.now().UTC()
	outcome := model.AttemptReviewerUnavailable
	if record.Termination.Category == model.TerminationAuthenticationFailure {
		outcome = model.AttemptUnknownFailure
	}
	record.Passes[0].Attempts = append(record.Passes[0].Attempts, model.AttemptRecord{Number: record.AttemptCount() + 1, Outcome: outcome, Provenance: prepared.profile.reviewer.candidate.provenance(), Diagnostic: record.Termination.Message, StartedAt: now, CompletedAt: now})
	return record, conductor.store.Save(record)
}

func isUnrecordedAvailabilityFailure(record model.ReviewRecord, prepared preparedReview) bool {
	if record.Termination == nil || record.Termination.Phase != model.PhaseAvailabilityCheck {
		return false
	}
	return record.AttemptCount() < prepared.profile.revision.AttemptLimit
}

func retryableTermination(termination *model.ReviewTermination) bool {
	if termination == nil {
		return false
	}
	switch termination.Category {
	case model.TerminationReviewerUnavailable, model.TerminationDeadlineExceeded, model.TerminationTransportFailure, model.TerminationMalformedOutput, model.TerminationResultValidationFailure:
		return true
	case model.TerminationAuthenticationFailure, model.TerminationCancelled, model.TerminationInputTooLarge, model.TerminationUnknownFailure:
		return false
	default:
		return false
	}
}

func lastRetryAfterMS(record model.ReviewRecord) int64 {
	if len(record.Passes) == 0 || len(record.Passes[0].Attempts) == 0 {
		return 0
	}
	attempts := record.Passes[0].Attempts
	return attempts[len(attempts)-1].RetryAfterMS
}

func retryDelay(policy model.RetryPolicy, attempt int, providerDelay time.Duration) time.Duration {
	initial, initialErr := time.ParseDuration(policy.InitialBackoff)
	maximum, maximumErr := time.ParseDuration(policy.MaxBackoff)
	if initialErr != nil || maximumErr != nil {
		// Retry policies are validated before execution; zero is the defensive
		// fallback for an invalid policy passed directly to this helper.
		return 0
	}
	delay := initial
	for step := 1; step < attempt && delay < maximum; step++ {
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	jittered := delay/2 + time.Duration(mathrand.Int64N(int64(delay/2)+1))
	if providerDelay > maximum {
		providerDelay = maximum
	}
	if providerDelay > jittered {
		return providerDelay
	}
	return jittered
}
