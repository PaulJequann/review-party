package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reviewparty/internal/hostrun"
	"reviewparty/internal/model"
	"reviewparty/internal/result"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
	"time"
)

// reviewRunner owns the single-Review lifecycle: pending → availability →
// execution → validation → persistence. Callers and tests cross one seam
// (runPreparedReview / executePass) instead of five scattered helpers.
// It concentrates timing, termination, and attempt construction behind a deep
// interface; Conductor remains a coordinator for subject + profile concerns.
type reviewRunner struct {
	store           store.RecordStore
	now             func() time.Time
	buildProvenance func() model.RuntimeProvenance
	publisher       *artifactPublisher
	warn            func(string)
}

func newReviewRunner(store store.RecordStore, now func() time.Time, buildProvenance func() model.RuntimeProvenance, publisher *artifactPublisher) *reviewRunner {
	return &reviewRunner{
		store:           store,
		now:             now,
		buildProvenance: buildProvenance,
		publisher:       publisher,
	}
}

func (runner *reviewRunner) pendingRecord(prepared preparedReview, replaysReviewID *model.ReviewID) (model.ReviewRecord, error) {
	id, err := newReviewID(runner.now())
	if err != nil {
		return model.ReviewRecord{}, err
	}
	now := runner.now().UTC()
	runtime := runner.buildProvenance()
	timings := prepared.timings
	passes := make([]model.PassRecord, 0, len(prepared.profile.revision.Passes))
	for _, planned := range prepared.profile.revision.Passes {
		passes = append(passes, model.PassRecord{Name: planned.Name, Required: planned.Required, Attempts: []model.AttemptRecord{}})
	}
	return model.ReviewRecord{
		SchemaVersion:   model.CurrentReviewRecordSchemaVersion,
		ID:              id,
		ReplaysReviewID: replaysReviewID,
		Lifecycle:       model.LifecyclePending,
		Subject:         prepared.subject.ReviewSubject,
		ProfileRevision: prepared.profile.revision,
		ProfileSnapshot: prepared.profile.snapshot,
		Passes:          passes,
		Runtime:         &runtime,
		Timings:         &timings,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func (runner *reviewRunner) startReview(prepared preparedReview, replaysReviewID *model.ReviewID) (model.ReviewRecord, error) {
	record, err := runner.pendingRecord(prepared, replaysReviewID)
	if err != nil {
		return model.ReviewRecord{}, err
	}
	if err := runner.store.Save(record); err != nil {
		return model.ReviewRecord{}, err
	}
	return record, nil
}

func (runner *reviewRunner) runPreparedReview(ctx context.Context, prepared preparedReview, replaysReviewID *model.ReviewID, reviewStarted time.Time) (model.ReviewRecord, error) {
	record, err := runner.startReview(prepared, replaysReviewID)
	if err != nil {
		return model.ReviewRecord{}, err
	}
	return runner.runPendingReview(ctx, pendingReview{prepared: prepared, record: record}, reviewStarted, nil)
}

func (runner *reviewRunner) runPendingReview(ctx context.Context, member pendingReview, reviewStarted time.Time, onAttempt func(int)) (model.ReviewRecord, error) {
	record, prepared := member.record, member.prepared
	record.Lifecycle = model.LifecycleRunning
	record.UpdatedAt = runner.now().UTC()
	if err := runner.store.Save(record); err != nil {
		return record, err
	}

	prompt := prepared.profile.prompt(record.Subject)
	if ctx.Err() == nil {
		if termination := runner.preflightInput(prepared.profile.reviewer, prompt); termination != nil {
			return runner.finishIncomplete(record, *termination, reviewStarted)
		}
	}

	executor := prepared.profile.reviewer.executor
	availabilityStarted := runner.now().UTC()
	check := executor.Check(ctx, prepared.profile.reviewer.candidate)
	record.Timings.AvailabilityCheckMS = elapsedMilliseconds(availabilityStarted, runner.now().UTC())
	if !check.Available {
		termination := terminationForAvailability(check.Diagnostic)
		return runner.finishIncomplete(record, termination, reviewStarted)
	}
	return runner.executePass(ctx, passExecution{
		record:        record,
		subject:       prepared.subject,
		profile:       prepared.profile,
		prompt:        prompt,
		executor:      executor,
		reviewStarted: reviewStarted,
		deadline:      prepared.deadline,
		onAttempt:     onAttempt,
	})
}

// resumePreparedReview continues an already persisted Review for an Eval
// retry. Eval owns retry policy; the runner owns the availability and attempt
// lifecycle details needed to resume the same prepared Review.
func (runner *reviewRunner) resumePreparedReview(ctx context.Context, record model.ReviewRecord, prepared preparedReview, reviewStarted time.Time) (model.ReviewRecord, error) {
	executor := prepared.profile.reviewer.executor
	check := executor.Check(ctx, prepared.profile.reviewer.candidate)
	if !check.Available {
		return runner.finishIncomplete(record, terminationForAvailability(check.Diagnostic), reviewStarted)
	}
	return runner.executePass(ctx, passExecution{record: record, subject: prepared.subject, profile: prepared.profile, prompt: prepared.profile.prompt(record.Subject), executor: executor, reviewStarted: reviewStarted, deadline: prepared.deadline})
}

func (runner *reviewRunner) finishIncomplete(record model.ReviewRecord, termination model.ReviewTermination, reviewStarted time.Time) (model.ReviewRecord, error) {
	record.Lifecycle = model.LifecycleIncomplete
	record.Termination = &termination
	runner.finalizeOperationalRecord(&record, reviewStarted)
	if err := runner.store.Save(record); err != nil {
		return record, err
	}
	return record, nil
}

func (runner *reviewRunner) finalizeOperationalRecord(record *model.ReviewRecord, reviewStarted time.Time) {
	completed := runner.now().UTC()
	record.UpdatedAt = completed
	record.Timings.TotalMS = elapsedMilliseconds(reviewStarted, completed)
}

type passExecution struct {
	record        model.ReviewRecord
	subject       subject.Subject
	profile       compiledProfile
	prompt        string
	executor      attemptExecutor
	reviewStarted time.Time
	deadline      time.Duration
	onAttempt     func(int)
}

func (runner *reviewRunner) executePass(ctx context.Context, pass passExecution) (model.ReviewRecord, error) {
	record := pass.record
	started := runner.now().UTC()
	attemptContext, cancel := context.WithTimeout(ctx, pass.deadline)
	defer cancel()
	prompt := pass.prompt
	execution, err := runner.executeAttempt(attemptContext, record, pass, prompt)
	if err != nil {
		return record, err
	}
	completed := runner.now().UTC()
	record.Timings.AttemptExecutionMS = elapsedMilliseconds(started, completed)

	validationStarted := runner.now().UTC()
	result, parseErr := result.CanonicalReviewResultContract.Parse(execution.AssistantText)
	record.Timings.ResultValidationMS = elapsedMilliseconds(validationStarted, runner.now().UTC())
	outcome := applyAttemptResult(&record, result, execution, parseErr)
	attempt, artifactErr := runner.buildAttempt(attemptDraft{
		reviewID:  record.ID,
		number:    record.AttemptCount() + 1,
		prompt:    prompt,
		candidate: pass.profile.reviewer.candidate,
		execution: execution,
		outcome:   outcome,
		started:   started,
		completed: completed,
	})
	if artifactErr != nil {
		return record, artifactErr
	}
	record.Passes[0].Attempts = append(record.Passes[0].Attempts, attempt)
	runner.finalizeOperationalRecord(&record, pass.reviewStarted)
	if err := runner.store.Save(record); err != nil {
		cleanupErr := runner.publisher.removeArtifacts(attempt.Artifacts)
		return record, errors.Join(err, cleanupErr)
	}
	return record, nil
}

func applyAttemptResult(record *model.ReviewRecord, result model.ReviewResult, execution attemptExecution, parseErr error) model.AttemptOutcome {
	outcome := execution.Outcome
	if outcome == model.AttemptCompleted && parseErr == nil {
		record.Result = &result
		record.Lifecycle = model.LifecycleCompleted
		return outcome
	}
	// A completed attempt can still contribute salvaged, explicitly-partial
	// evidence. The review stays incomplete — partial evidence never reports
	// a fully clean or fully complete lifecycle — and the paired error names
	// every dropped section.
	if outcome == model.AttemptCompleted && result.Status == model.ResultFindingsPartial {
		record.Result = &result
		record.Lifecycle = model.LifecycleIncomplete
		record.Termination = &model.ReviewTermination{
			Category: model.TerminationResultValidationFailure,
			Phase:    model.PhaseResultValidation,
			Message:  attemptTerminationMessage(model.AttemptInvalidResult, execution.Diagnostic, parseErr),
		}
		return outcome
	}
	if outcome == model.AttemptCompleted {
		outcome = model.AttemptInvalidResult
		record.Lifecycle = model.LifecycleIncomplete
		termination := model.ReviewTermination{
			Category: model.TerminationResultValidationFailure,
			Phase:    model.PhaseResultValidation,
			Message:  attemptTerminationMessage(outcome, execution.Diagnostic, parseErr),
		}
		record.Termination = &termination
		return outcome
	}
	if outcome == "" {
		outcome = model.AttemptUnknownFailure
	}
	record.Lifecycle = model.LifecycleIncomplete
	termination := terminationForAttempt(execution, outcome, parseErr)
	record.Termination = &termination
	return outcome
}

func (runner *reviewRunner) executeAttempt(ctx context.Context, record model.ReviewRecord, pass passExecution, prompt string) (attemptExecution, error) {
	if gate := attemptGateFromContext(ctx); gate != nil {
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return contextExecution(ctx.Err()), nil
		}
	}
	repository, err := executionRepository(ctx, pass.subject)
	if err != nil {
		return viewFailure(ctx, err)
	}
	if pass.onAttempt != nil {
		pass.onAttempt(record.AttemptCount() + 1)
	}
	return pass.executor.Execute(ctx, attemptSpec{Repository: repository, Prompt: prompt, Candidate: pass.profile.reviewer.candidate}), nil
}

// viewFailure classifies a view that could not be built: a missing run ends
// the Review, a cancelled context is the caller's stop, and anything else is
// a launch failure recorded on the Attempt.
func viewFailure(ctx context.Context, err error) (attemptExecution, error) {
	switch {
	case errors.Is(err, hostrun.ErrNoRun):
		return attemptExecution{}, err
	case ctx.Err() != nil:
		return contextExecution(ctx.Err()), nil
	default:
		return failedExecution(model.AttemptUnknownFailure, model.TerminationTransportFailure, model.PhaseHarnessLaunch, err.Error()), nil
	}
}

// executionRepository is the directory a Reviewer reads: the repository
// itself for working changes, else the run's view of the Subject, built once
// per run and shared by every member that reviews the same content.
func executionRepository(ctx context.Context, subject subject.Subject) (string, error) {
	if repository, inPlace := subject.InPlace(); inPlace {
		return repository, nil
	}
	run, err := hostrun.From(ctx)
	if err != nil {
		return "", err
	}
	return run.View(ctx, subject)
}

type attemptDraft struct {
	reviewID  model.ReviewID
	number    int
	prompt    string
	candidate reviewerCandidate
	execution attemptExecution
	outcome   model.AttemptOutcome
	started   time.Time
	completed time.Time
}

func (runner *reviewRunner) buildAttempt(draft attemptDraft) (model.AttemptRecord, error) {
	execution := draft.execution
	attempt := model.AttemptRecord{
		Number:       draft.number,
		Outcome:      draft.outcome,
		Provenance:   resolvedProvenance(draft.candidate, execution),
		Diagnostic:   execution.Diagnostic,
		RawOutput:    boundedAttemptOutput(execution.AssistantText),
		RetryAfterMS: execution.RetryAfter.Milliseconds(),
		StartedAt:    draft.started,
		CompletedAt:  draft.completed,
	}
	if runner.publisher == nil || runner.publisher.store == nil {
		return attempt, nil
	}
	references, err := runner.publisher.publishAttemptArtifacts(draft.reviewID, attempt.Number, draft.prompt, execution)
	if err != nil {
		return model.AttemptRecord{}, err
	}
	attempt.Artifacts = references
	attempt.RawOutput = ""
	return attempt, nil
}

func (runner *reviewRunner) VerifyArtifacts(record model.ReviewRecord) error {
	if runner.publisher == nil {
		return nil
	}
	return runner.publisher.verifyArtifacts(record)
}

func terminationForAvailability(diagnostic string) model.ReviewTermination {
	category := model.TerminationReviewerUnavailable
	if diagnosticFailureCategory(diagnostic) == model.TerminationAuthenticationFailure {
		category = model.TerminationAuthenticationFailure
	}
	return model.ReviewTermination{Category: category, Phase: model.PhaseAvailabilityCheck, Message: diagnostic}
}

func terminationForAttempt(execution attemptExecution, outcome model.AttemptOutcome, parseErr error) model.ReviewTermination {
	message := attemptTerminationMessage(outcome, execution.Diagnostic, parseErr)
	return model.ReviewTermination{Category: execution.FailureCategory, Phase: execution.FailurePhase, Message: message}
}

func boundedAttemptOutput(output string) string {
	if len(output) <= result.MaxResultSize {
		return output
	}
	return "[truncated to final bytes]\n" + output[len(output)-result.MaxResultSize:]
}

func resolvedProvenance(candidate reviewerCandidate, execution attemptExecution) model.ReviewerProvenance {
	provenance := candidate.provenance()
	if execution.ResolvedModel != "" {
		provenance.Model = execution.ResolvedModel
	}
	if execution.ResolvedEffort != "" {
		provenance.Effort = execution.ResolvedEffort
	}
	return provenance
}

func elapsedMilliseconds(started, completed time.Time) int64 {
	if completed.Before(started) {
		return 0
	}
	return completed.Sub(started).Milliseconds()
}

func attemptTerminationMessage(outcome model.AttemptOutcome, diagnostic string, parseErr error) string {
	if outcome == model.AttemptInvalidResult && parseErr != nil {
		if diagnostic == "" {
			return parseErr.Error()
		}
		return parseErr.Error() + "; adapter diagnostic: " + diagnostic
	}
	if diagnostic != "" {
		return diagnostic
	}
	return fmt.Sprintf("attempt ended with %s", outcome)
}

func newReviewID(now time.Time) (model.ReviewID, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate review id: %w", err)
	}
	return model.ReviewID(fmt.Sprintf("rp_%d_%s", now.UTC().UnixMilli(), hex.EncodeToString(random))), nil
}

func validReviewID(id model.ReviewID) bool {
	if len(id) < 24 || len(id) > 64 {
		return false
	}
	for _, character := range id {
		if !validReviewIDCharacter(character) {
			return false
		}
	}
	return true
}

func validReviewIDCharacter(character rune) bool {
	if character == '_' {
		return true
	}
	if character >= 'a' && character <= 'z' {
		return true
	}
	return character >= '0' && character <= '9'
}
