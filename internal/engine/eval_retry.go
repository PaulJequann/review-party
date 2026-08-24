package engine

import (
	"context"
	mathrand "math/rand/v2"
	"time"

	"reviewparty/internal/model"
)

func (conductor *Conductor) reviewEvalCase(ctx context.Context, selection model.ReviewSelection, policy model.RetryPolicy) (ReviewRecord, error) {
	prepared, started, err := conductor.prepareEvalReview(ctx, selection, policy)
	if err != nil {
		return ReviewRecord{}, err
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
	prepared, err := conductor.prepareReview(selection)
	if err != nil {
		return preparedReview{}, time.Time{}, err
	}
	prepared.profile.revision.AttemptLimit = policy.MaxAttempts
	return prepared, started, nil
}

func shouldRetryEval(record ReviewRecord, err error, attempts int, policy model.RetryPolicy) bool {
	return err == nil && record.Lifecycle == LifecycleIncomplete && attempts < policy.MaxAttempts && retryableTermination(record.Termination)
}

func (conductor *Conductor) retryEvalReview(execution retryReviewExecution, record ReviewRecord) (ReviewRecord, error) {
	providerDelay := time.Duration(lastRetryAfterMS(record)) * time.Millisecond
	if err := conductor.wait(execution.context, conductor.retryDelay(execution.policy, execution.attempt, providerDelay)); err != nil {
		return record, err
	}
	record.Lifecycle, record.Termination, record.Result = LifecycleRunning, nil, nil
	record.UpdatedAt = conductor.now().UTC()
	if err := conductor.store.Save(record); err != nil {
		return record, err
	}
	next, err := conductor.resumePreparedReview(execution.context, record, execution.prepared, execution.started)
	return conductor.recordAvailabilityFailure(next, execution.prepared, err)
}

func (conductor *Conductor) recordAvailabilityFailure(record ReviewRecord, prepared preparedReview, err error) (ReviewRecord, error) {
	if err != nil || !isUnrecordedAvailabilityFailure(record, prepared) {
		return record, err
	}
	now := conductor.now().UTC()
	outcome := AttemptReviewerUnavailable
	if record.Termination.Category == TerminationAuthenticationFailure {
		outcome = AttemptUnknownFailure
	}
	record.Passes[0].Attempts = append(record.Passes[0].Attempts, AttemptRecord{Number: record.AttemptCount() + 1, Outcome: outcome, Provenance: prepared.profile.reviewer.candidate.provenance(), Diagnostic: record.Termination.Message, StartedAt: now, CompletedAt: now})
	return record, conductor.store.Save(record)
}

func isUnrecordedAvailabilityFailure(record ReviewRecord, prepared preparedReview) bool {
	if record.Termination == nil || record.Termination.Phase != PhaseAvailabilityCheck {
		return false
	}
	return record.AttemptCount() < prepared.profile.revision.AttemptLimit
}

func (conductor *Conductor) resumePreparedReview(ctx context.Context, record ReviewRecord, prepared preparedReview, reviewStarted time.Time) (ReviewRecord, error) {
	executor := prepared.profile.reviewer.executor
	check := executor.Check(ctx, prepared.profile.reviewer.candidate)
	if !check.Available {
		return conductor.getRunner().finishIncomplete(record, terminationForAvailability(check.Diagnostic), reviewStarted)
	}
	return conductor.getRunner().executePass(ctx, passExecution{record: record, profile: prepared.profile, executor: executor, reviewStarted: reviewStarted, deadline: prepared.deadline})
}

func retryableTermination(termination *ReviewTermination) bool {
	if termination == nil {
		return false
	}
	switch termination.Category {
	case TerminationReviewerUnavailable, TerminationDeadlineExceeded, TerminationTransportFailure, TerminationMalformedOutput, TerminationResultValidationFailure:
		return true
	default:
		return false
	}
}

func lastRetryAfterMS(record ReviewRecord) int64 {
	if len(record.Passes) == 0 || len(record.Passes[0].Attempts) == 0 {
		return 0
	}
	attempts := record.Passes[0].Attempts
	return attempts[len(attempts)-1].RetryAfterMS
}

func retryDelay(policy model.RetryPolicy, attempt int, providerDelay time.Duration) time.Duration {
	initial, _ := time.ParseDuration(policy.InitialBackoff)
	maximum, _ := time.ParseDuration(policy.MaxBackoff)
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
