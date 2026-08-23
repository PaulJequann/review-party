package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"reviewparty/internal/artifact"
	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"time"
)

type Config struct {
	AttemptDeadline time.Duration
	// UserConfigurationPath optionally overrides the canonical Personal
	// Configuration file location for tests and explicit --config flags.
	UserConfigurationPath string
}

type Conductor struct {
	store           recordStore
	reviewers       reviewerCatalog
	profiles        profileLibrary
	attemptDeadline time.Duration
	now             func() time.Time
	buildProvenance func() RuntimeProvenance
	artifacts       *artifact.Store
	retryDelay      func(model.RetryPolicy, int, time.Duration) time.Duration
	wait            func(context.Context, time.Duration) error
}

func New(config Config) (*Conductor, error) {
	if config.AttemptDeadline <= 0 {
		config.AttemptDeadline = 10 * time.Minute
	}
	manager := newConfigurationManager(config.UserConfigurationPath)
	effective, err := manager.Resolve(configuration.Request{})
	if err != nil {
		return nil, err
	}
	stateDirectory := firstNonempty(effective.StateDirectory.Value, defaultStateDirectory())
	store, err := newDeferredLedgerRecordStore(stateDirectory)
	if err != nil {
		return nil, err
	}
	reviewers, err := configureReviewerCatalog(defaultReviewerCatalog(), effective)
	if err != nil {
		return nil, err
	}
	conductor, err := newConductorWithProfiles(store, reviewers, profileLibrary{configuration: manager}, config.AttemptDeadline)
	if err != nil {
		return nil, err
	}
	conductor.artifacts, err = artifact.NewStore(stateDirectory)
	if err != nil {
		return nil, err
	}
	return conductor, nil
}

func newConductor(store recordStore, executors map[string]attemptExecutor, deadline time.Duration) (*Conductor, error) {
	return newConductorWithCatalog(store, catalogWithExecutors(executors), deadline)
}

func newConductorWithCatalog(store recordStore, reviewers reviewerCatalog, deadline time.Duration) (*Conductor, error) {
	return newConductorWithProfiles(store, reviewers, newProfileLibrary(""), deadline)
}

func newConductorWithProfiles(store recordStore, reviewers reviewerCatalog, profiles profileLibrary, deadline time.Duration) (*Conductor, error) {
	if profiles.configuration == nil {
		return nil, errProfileLibraryNotConfigured
	}
	return &Conductor{
		store:           store,
		reviewers:       reviewers,
		profiles:        profiles,
		attemptDeadline: deadline,
		now:             time.Now,
		buildProvenance: currentRuntimeProvenance,
		retryDelay:      retryDelay,
		wait:            waitForRetry,
	}, nil
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (conductor *Conductor) Review(ctx context.Context, selection ReviewSelection) (ReviewRecord, error) {
	if err := ctx.Err(); err != nil {
		return ReviewRecord{}, err
	}
	if err := conductor.requirePreparedState(selection.Repository); err != nil {
		return ReviewRecord{}, err
	}
	reviewStarted := conductor.now().UTC()
	prepared, err := conductor.prepareReview(selection)
	if err != nil {
		return ReviewRecord{}, err
	}
	return conductor.runPreparedReview(ctx, prepared, nil, reviewStarted)
}

func (conductor *Conductor) runPreparedReview(ctx context.Context, prepared preparedReview, replaysReviewID *ReviewID, reviewStarted time.Time) (ReviewRecord, error) {
	record, err := conductor.pendingRecord(prepared.subject, prepared.profile, prepared.timings, replaysReviewID)
	if err != nil {
		return ReviewRecord{}, err
	}
	if err := conductor.store.Save(record); err != nil {
		return ReviewRecord{}, err
	}

	record.Lifecycle = LifecycleRunning
	record.UpdatedAt = conductor.now().UTC()
	if err := conductor.store.Save(record); err != nil {
		return record, err
	}

	executor := prepared.profile.reviewer.executor
	availabilityStarted := conductor.now().UTC()
	check := executor.Check(ctx, prepared.profile.reviewer.candidate)
	record.Timings.AvailabilityCheckMS = elapsedMilliseconds(availabilityStarted, conductor.now().UTC())
	if !check.Available {
		termination := terminationForAvailability(check.Diagnostic)
		return conductor.finishIncomplete(record, termination, reviewStarted)
	}
	return conductor.executePass(ctx, passExecution{
		record:        record,
		profile:       prepared.profile,
		executor:      executor,
		reviewStarted: reviewStarted,
		deadline:      prepared.deadline,
	})
}

func (conductor *Conductor) Replay(ctx context.Context, selection ReplaySelection) (ReviewRecord, error) {
	if err := ctx.Err(); err != nil {
		return ReviewRecord{}, err
	}
	if !validReviewID(selection.SourceReviewID) {
		return ReviewRecord{}, fmt.Errorf("invalid source review id %q", selection.SourceReviewID)
	}
	if err := conductor.requirePreparedState("."); err != nil {
		return ReviewRecord{}, err
	}
	source, err := conductor.store.Load(selection.SourceReviewID)
	if err != nil {
		return ReviewRecord{}, fmt.Errorf("load replay source %q: %w", selection.SourceReviewID, err)
	}
	reviewStarted := conductor.now().UTC()
	prepared, err := conductor.prepareReplay(source, selection)
	if err != nil {
		return ReviewRecord{}, err
	}
	sourceID := source.ID
	return conductor.runPreparedReview(ctx, prepared, &sourceID, reviewStarted)
}

type InitializationRequiredError struct {
	Repository string
}

func (failure InitializationRequiredError) Error() string {
	repository := firstNonempty(failure.Repository, ".")
	return fmt.Sprintf("Review Party is not initialized; run review-party init --repo %q", repository)
}

func (conductor *Conductor) requirePreparedState(repository string) error {
	requirement, ok := conductor.store.(interface{ RequirePrepared() error })
	if !ok {
		return nil
	}
	err := requirement.RequirePrepared()
	if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
		return InitializationRequiredError{Repository: repository}
	}
	return err
}

type preparedReview struct {
	subject  ReviewSubject
	profile  compiledProfile
	timings  ReviewTimings
	deadline time.Duration
}

func (conductor *Conductor) prepareReview(selection ReviewSelection) (preparedReview, error) {
	profileSelection := selection.ProfileSelection()
	timings := ReviewTimings{}
	subjectStarted := conductor.now().UTC()
	repository, repositoryErr := resolveReviewRepository(selection)
	timings.SubjectResolutionMS += elapsedMilliseconds(subjectStarted, conductor.now().UTC())
	if repositoryErr != nil {
		if selection.Reviewer != "" {
			if err := conductor.profiles.validateExplicitReviewer(selection.ProfileSelection(), selection.Repository, conductor.reviewers); err != nil {
				return preparedReview{}, err
			}
		}
		return preparedReview{}, repositoryErr
	}
	resolved, err := conductor.profiles.resolve(profileRequest{repository: repository, name: profileSelection.Profile, reviewer: profileSelection.Reviewer})
	if err != nil {
		return preparedReview{}, err
	}
	profileStarted := conductor.now().UTC()
	profile, err := conductor.compileProfile(profileSelection, resolved)
	timings.ProfileCompilationMS = elapsedMilliseconds(profileStarted, conductor.now().UTC())
	if err != nil {
		return preparedReview{}, err
	}
	subjectStarted = conductor.now().UTC()
	subject, err := resolveSubject(repository, selection.Subject)
	timings.SubjectResolutionMS += elapsedMilliseconds(subjectStarted, conductor.now().UTC())
	if err != nil {
		return preparedReview{}, err
	}
	return preparedReview{subject: subject, profile: profile, timings: timings, deadline: conductor.attemptDeadline}, nil
}

func resolveReviewRepository(selection ReviewSelection) (string, error) {
	if selection.Subject.Kind == SubjectCapturedChange {
		return selection.Repository, nil
	}
	return resolveRepositoryRoot(selection.Repository)
}

func (conductor *Conductor) Profiles(ctx context.Context) ([]ProfileSummary, error) {
	return conductor.profilesAt(ctx, "")
}

func (conductor *Conductor) ProfilesForRepository(ctx context.Context, repository string) ([]ProfileSummary, error) {
	root, err := resolveRepositoryRoot(repository)
	if err != nil {
		return nil, err
	}
	return conductor.profilesAt(ctx, root)
}

func (conductor *Conductor) profilesAt(ctx context.Context, repository string) ([]ProfileSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return conductor.profileSummaries(repository)
}

func (conductor *Conductor) Explain(ctx context.Context, selection ProfileSelection) (ProfileExplanation, error) {
	return conductor.explainAt(ctx, selection, "")
}

func (conductor *Conductor) ExplainForRepository(ctx context.Context, selection ProfileSelection, repository string) (ProfileExplanation, error) {
	root, err := resolveRepositoryRoot(repository)
	if err != nil {
		return ProfileExplanation{}, err
	}
	return conductor.explainAt(ctx, selection, root)
}

func (conductor *Conductor) explainAt(ctx context.Context, selection ProfileSelection, repository string) (ProfileExplanation, error) {
	if err := ctx.Err(); err != nil {
		return ProfileExplanation{}, err
	}
	profile, err := conductor.compileFilesystemProfile(selection, repository)
	if err != nil {
		return ProfileExplanation{}, err
	}
	return ProfileExplanation{
		ProfileRevision:    profile.revision,
		ReviewerWasDefault: profile.reviewerWasDefault,
		Instructions:       profile.snapshot.Instructions,
	}, nil
}

func (conductor *Conductor) Inspect(_ context.Context, id ReviewID) (ReviewRecord, error) {
	if !validReviewID(id) {
		return ReviewRecord{}, fmt.Errorf("invalid review id %q", id)
	}
	record, err := conductor.store.Load(id)
	if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
		return ReviewRecord{}, InitializationRequiredError{Repository: "."}
	}
	return record, err
}

func (conductor *Conductor) History(_ context.Context, query store.HistoryQuery) (store.HistoryPage, error) {
	ledger, ok := conductor.store.(interface {
		History(store.HistoryQuery) (store.HistoryPage, error)
	})
	if !ok {
		return store.HistoryPage{}, fmt.Errorf("review history requires the SQLite ledger")
	}
	page, err := ledger.History(query)
	if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
		return store.HistoryPage{}, InitializationRequiredError{Repository: "."}
	}
	return page, err
}

func (conductor *Conductor) pendingRecord(subject ReviewSubject, profile compiledProfile, timings ReviewTimings, replaysReviewID *ReviewID) (ReviewRecord, error) {
	id, err := newReviewID(conductor.now())
	if err != nil {
		return ReviewRecord{}, err
	}
	now := conductor.now().UTC()
	runtime := conductor.buildProvenance()
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

type passExecution struct {
	record        ReviewRecord
	profile       compiledProfile
	executor      attemptExecutor
	reviewStarted time.Time
	deadline      time.Duration
}

func (conductor *Conductor) executePass(ctx context.Context, pass passExecution) (ReviewRecord, error) {
	record := pass.record
	started := conductor.now().UTC()
	attemptContext, cancel := context.WithTimeout(ctx, pass.deadline)
	defer cancel()
	prompt := pass.profile.prompt(record.Subject)
	execution, cleanupErr := conductor.executeAttempt(attemptContext, record, pass, prompt)
	completed := conductor.now().UTC()
	record.Timings.AttemptExecutionMS = elapsedMilliseconds(started, completed)

	validationStarted := conductor.now().UTC()
	result, parseErr := canonicalReviewResultContract.Parse(execution.AssistantText)
	record.Timings.ResultValidationMS = elapsedMilliseconds(validationStarted, conductor.now().UTC())
	outcome := applyAttemptResult(&record, result, execution, parseErr)
	attempt, artifactErr := conductor.buildAttempt(record.ID, prompt, pass.profile.reviewer.candidate, execution, outcome, started, completed)
	if artifactErr != nil {
		return record, artifactErr
	}
	attempt.Number = record.AttemptCount() + 1
	record.Passes[0].Attempts = append(record.Passes[0].Attempts, attempt)
	conductor.finalizeOperationalRecord(&record, pass.reviewStarted)
	if err := conductor.store.Save(record); err != nil {
		conductor.removeArtifacts(attempt.Artifacts)
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

func (conductor *Conductor) executeAttempt(ctx context.Context, record ReviewRecord, pass passExecution, prompt string) (attemptExecution, error) {
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

func (conductor *Conductor) finishIncomplete(record ReviewRecord, termination ReviewTermination, reviewStarted time.Time) (ReviewRecord, error) {
	record.Lifecycle = LifecycleIncomplete
	record.Termination = &termination
	conductor.finalizeOperationalRecord(&record, reviewStarted)
	if err := conductor.store.Save(record); err != nil {
		return record, err
	}
	return record, nil
}

func (conductor *Conductor) finalizeOperationalRecord(record *ReviewRecord, reviewStarted time.Time) {
	completed := conductor.now().UTC()
	record.UpdatedAt = completed
	record.Timings.TotalMS = elapsedMilliseconds(reviewStarted, completed)
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

func elapsedMilliseconds(started, completed time.Time) int64 {
	if completed.Before(started) {
		return 0
	}
	return completed.Sub(started).Milliseconds()
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

func defaultStateDirectory() string {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "review-party")
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".local", "state", "review-party")
	}
	return filepath.Join(os.TempDir(), "review-party")
}
