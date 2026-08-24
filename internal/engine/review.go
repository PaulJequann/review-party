package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"reviewparty/internal/artifact"
)

// reviewRunner owns the single-Review lifecycle: pending → availability →
// execution → validation → persistence. Callers and tests cross one seam
// (runPreparedReview / executePass) instead of five scattered helpers.
// It concentrates timing, termination, and attempt construction behind a deep
// interface; Conductor remains a coordinator for subject + profile concerns.
type reviewRunner struct {
	store           recordStore
	now             func() time.Time
	buildProvenance func() RuntimeProvenance
	artifacts       *artifact.Store
}

func newReviewRunner(store recordStore, now func() time.Time, buildProvenance func() RuntimeProvenance, artifacts *artifact.Store) *reviewRunner {
	return &reviewRunner{
		store:           store,
		now:             now,
		buildProvenance: buildProvenance,
		artifacts:       artifacts,
	}
}

func (runner *reviewRunner) pendingRecord(subject ReviewSubject, profile compiledProfile, timings ReviewTimings, replaysReviewID *ReviewID) (ReviewRecord, error) {
	id, err := newReviewID(runner.now())
	if err != nil {
		return ReviewRecord{}, err
	}
	now := runner.now().UTC()
	runtime := runner.buildProvenance()
	passes := make([]PassRecord, 0, len(profile.revision.Passes))
	for _, planned := range profile.revision.Passes {
		passes = append(passes, PassRecord{Name: planned.Name, Required: planned.Required, Attempts: []AttemptRecord{}})
	}
	return ReviewRecord{
		SchemaVersion:   currentReviewRecordSchemaVersion,
		ID:              id,
		ReplaysReviewID: replaysReviewID,
		Lifecycle:       LifecyclePending,
		Subject:         subject,
		ProfileRevision: profile.revision,
		ProfileSnapshot: profile.snapshot,
		Passes:          passes,
		Runtime:         &runtime,
		Timings:         &timings,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func (runner *reviewRunner) runPreparedReview(ctx context.Context, prepared preparedReview, replaysReviewID *ReviewID, reviewStarted time.Time) (ReviewRecord, error) {
	record, err := runner.pendingRecord(prepared.subject, prepared.profile, prepared.timings, replaysReviewID)
	if err != nil {
		return ReviewRecord{}, err
	}
	if err := runner.store.Save(record); err != nil {
		return ReviewRecord{}, err
	}

	record.Lifecycle = LifecycleRunning
	record.UpdatedAt = runner.now().UTC()
	if err := runner.store.Save(record); err != nil {
		return record, err
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
		profile:       prepared.profile,
		executor:      executor,
		reviewStarted: reviewStarted,
		deadline:      prepared.deadline,
	})
}

func (runner *reviewRunner) finishIncomplete(record ReviewRecord, termination ReviewTermination, reviewStarted time.Time) (ReviewRecord, error) {
	record.Lifecycle = LifecycleIncomplete
	record.Termination = &termination
	runner.finalizeOperationalRecord(&record, reviewStarted)
	if err := runner.store.Save(record); err != nil {
		return record, err
	}
	return record, nil
}

func (runner *reviewRunner) finalizeOperationalRecord(record *ReviewRecord, reviewStarted time.Time) {
	completed := runner.now().UTC()
	record.UpdatedAt = completed
	record.Timings.TotalMS = elapsedMilliseconds(reviewStarted, completed)
}

type passExecution struct {
	record        ReviewRecord
	profile       compiledProfile
	executor      attemptExecutor
	reviewStarted time.Time
	deadline      time.Duration
}

func (runner *reviewRunner) executePass(ctx context.Context, pass passExecution) (ReviewRecord, error) {
	record := pass.record
	started := runner.now().UTC()
	attemptContext, cancel := context.WithTimeout(ctx, pass.deadline)
	defer cancel()
	prompt := pass.profile.prompt(record.Subject)
	execution, cleanupErr := runner.executeAttempt(attemptContext, record, pass, prompt)
	completed := runner.now().UTC()
	record.Timings.AttemptExecutionMS = elapsedMilliseconds(started, completed)

	validationStarted := runner.now().UTC()
	result, parseErr := canonicalReviewResultContract.Parse(execution.AssistantText)
	record.Timings.ResultValidationMS = elapsedMilliseconds(validationStarted, runner.now().UTC())
	outcome := applyAttemptResult(&record, result, execution, parseErr)
	attempt, artifactErr := runner.buildAttempt(record.ID, prompt, pass.profile.reviewer.candidate, execution, outcome, started, completed)
	if artifactErr != nil {
		return record, artifactErr
	}
	attempt.Number = record.AttemptCount() + 1
	record.Passes[0].Attempts = append(record.Passes[0].Attempts, attempt)
	runner.finalizeOperationalRecord(&record, pass.reviewStarted)
	if err := runner.store.Save(record); err != nil {
		runner.removeArtifacts(attempt.Artifacts)
		return record, err
	}
	if cleanupErr != nil {
		return record, fmt.Errorf("Review %s was persisted but its Subject execution checkout could not be cleaned: %w", record.ID, cleanupErr)
	}
	return record, nil
}

func applyAttemptResult(record *ReviewRecord, result ReviewResult, execution attemptExecution, parseErr error) AttemptOutcome {
	outcome := execution.Outcome
	if outcome == AttemptCompleted && parseErr == nil {
		record.Result = &result
		record.Lifecycle = LifecycleCompleted
		return outcome
	}
	if outcome == AttemptCompleted {
		outcome = AttemptInvalidResult
	} else if outcome == "" {
		outcome = AttemptUnknownFailure
	}
	record.Lifecycle = LifecycleIncomplete
	termination := terminationForAttempt(execution, outcome, parseErr)
	record.Termination = &termination
	return outcome
}

func (runner *reviewRunner) executeAttempt(ctx context.Context, record ReviewRecord, pass passExecution, prompt string) (attemptExecution, error) {
	checkout, err := prepareSubjectExecution(record.Subject, string(record.ID)+"-1")
	if err != nil {
		return failedExecution(AttemptUnknownFailure, TerminationTransportFailure, PhaseHarnessLaunch, err.Error()), nil
	}
	defer checkout.Close()
	if gate := attemptGateFromContext(ctx); gate != nil {
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return contextExecution(ctx.Err()), checkout.Close()
		}
	}
	execution := pass.executor.Execute(ctx, attemptSpec{Repository: checkout.Repository, Prompt: prompt, Candidate: pass.profile.reviewer.candidate})
	return execution, checkout.Close()
}

func (runner *reviewRunner) buildAttempt(id ReviewID, prompt string, candidate reviewerCandidate, execution attemptExecution, outcome AttemptOutcome, started, completed time.Time) (AttemptRecord, error) {
	attempt := AttemptRecord{
		Number:       1,
		Outcome:      outcome,
		Provenance:   resolvedProvenance(candidate, execution),
		Diagnostic:   execution.Diagnostic,
		RawOutput:    boundedAttemptOutput(execution.AssistantText),
		RetryAfterMS: execution.RetryAfter.Milliseconds(),
		StartedAt:    started,
		CompletedAt:  completed,
	}
	if runner.artifacts == nil {
		return attempt, nil
	}
	references, err := runner.publishAttemptArtifacts(id, attempt.Number, prompt, execution)
	if err != nil {
		return AttemptRecord{}, err
	}
	attempt.Artifacts = references
	attempt.RawOutput = ""
	return attempt, nil
}

func (runner *reviewRunner) publishAttemptArtifacts(id ReviewID, number int, prompt string, execution attemptExecution) ([]ArtifactReference, error) {
	inputs := []struct {
		kind      string
		contents  []byte
		truncated bool
	}{
		{kind: "constructed-prompt", contents: []byte(prompt), truncated: len(prompt) > maxHarnessStdout},
		{kind: "assistant-text", contents: []byte(execution.AssistantText), truncated: execution.ArtifactTruncated || len(execution.AssistantText) > maxHarnessStdout},
	}
	references := make([]ArtifactReference, 0, len(inputs))
	for _, input := range inputs {
		contents := boundedArtifactContents(input.contents)
		reference, err := runner.artifacts.Publish(id, number, input.kind, contents, input.truncated)
		if err != nil {
			runner.removeArtifacts(references)
			return nil, err
		}
		references = append(references, reference)
	}
	return references, nil
}

func boundedArtifactContents(contents []byte) []byte {
	if len(contents) <= maxHarnessStdout {
		return contents
	}
	return contents[:maxHarnessStdout]
}

func (runner *reviewRunner) removeArtifacts(references []ArtifactReference) {
	for _, reference := range references {
		_ = runner.artifacts.Remove(reference)
	}
}

func (runner *reviewRunner) VerifyArtifacts(record ReviewRecord) error {
	if runner.artifacts == nil {
		return nil
	}
	for _, pass := range record.Passes {
		for _, attempt := range pass.Attempts {
			for _, reference := range attempt.Artifacts {
				if _, err := runner.artifacts.Read(reference); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func terminationForAvailability(diagnostic string) ReviewTermination {
	category := TerminationReviewerUnavailable
	if diagnosticFailureCategory(diagnostic) == TerminationAuthenticationFailure {
		category = TerminationAuthenticationFailure
	}
	return ReviewTermination{Category: category, Phase: PhaseAvailabilityCheck, Message: diagnostic}
}

func terminationForAttempt(execution attemptExecution, outcome AttemptOutcome, parseErr error) ReviewTermination {
	message := attemptTerminationMessage(outcome, execution.Diagnostic, parseErr)
	if execution.FailureCategory != "" {
		return ReviewTermination{Category: execution.FailureCategory, Phase: execution.FailurePhase, Message: message}
	}
	switch {
	case execution.Outcome == AttemptCompleted && parseErr != nil:
		return ReviewTermination{Category: TerminationResultValidationFailure, Phase: PhaseResultValidation, Message: message}
	case outcome == AttemptInvalidResult:
		return ReviewTermination{Category: TerminationMalformedOutput, Phase: PhaseOutputDecode, Message: message}
	case outcome == AttemptReviewerUnavailable:
		return ReviewTermination{Category: TerminationReviewerUnavailable, Phase: PhaseHarnessLaunch, Message: message}
	case outcome == AttemptTransientFailure:
		return ReviewTermination{Category: TerminationTransportFailure, Phase: PhaseReviewerExecution, Message: message}
	case outcome == AttemptCancelled:
		return ReviewTermination{Category: TerminationCancelled, Phase: PhaseReviewerExecution, Message: message}
	default:
		return ReviewTermination{Category: TerminationUnknownFailure, Phase: PhaseReviewerExecution, Message: message}
	}
}

func boundedAttemptOutput(output string) string {
	if len(output) <= maxResultSize {
		return output
	}
	return "[truncated to final bytes]\n" + output[len(output)-maxResultSize:]
}

func resolvedProvenance(candidate reviewerCandidate, execution attemptExecution) ReviewerProvenance {
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

func attemptTerminationMessage(outcome AttemptOutcome, diagnostic string, parseErr error) string {
	if outcome == AttemptInvalidResult && parseErr != nil {
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

func newReviewID(now time.Time) (ReviewID, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate review id: %w", err)
	}
	return ReviewID(fmt.Sprintf("rp_%d_%s", now.UTC().UnixMilli(), hex.EncodeToString(random))), nil
}

func validReviewID(id ReviewID) bool {
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
