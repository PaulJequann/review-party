package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"reviewparty/internal/artifact"
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
	runner          *reviewRunner
}

func New(config Config) (*Conductor, error) {
	if config.AttemptDeadline <= 0 {
		config.AttemptDeadline = 10 * time.Minute
	}
	manager := newConfigurationManager(config.UserConfigurationPath)
	configuredState, err := manager.ResolveStateDirectory()
	if err != nil {
		return nil, err
	}
	stateDirectory := firstNonempty(configuredState.Value, defaultStateDirectory())
	store, err := newDeferredLedgerRecordStore(stateDirectory)
	if err != nil {
		return nil, err
	}
	// Repository-scoped profile compilation applies the complete Personal and
	// Repository reviewer policy before validating a selection.
	reviewers := defaultReviewerCatalog()
	conductor, err := newConductorWithProfiles(store, reviewers, profileLibrary{configuration: manager}, config.AttemptDeadline)
	if err != nil {
		return nil, err
	}
	conductor.artifacts, err = artifact.NewStore(stateDirectory)
	if err != nil {
		return nil, err
	}
	conductor.runner.artifacts = conductor.artifacts
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
	conductor := &Conductor{
		store:           store,
		reviewers:       reviewers,
		profiles:        profiles,
		attemptDeadline: deadline,
		now:             time.Now,
		buildProvenance: currentRuntimeProvenance,
		retryDelay:      retryDelay,
		wait:            waitForRetry,
	}
	conductor.runner = newReviewRunner(store, func() time.Time { return conductor.now() }, func() RuntimeProvenance { return conductor.buildProvenance() }, nil)
	return conductor, nil
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
	return conductor.runner.runPreparedReview(ctx, prepared, nil, reviewStarted)
}

// runPreparedReview is the deep lifecycle seam: one place owns pending →
// availability → execution → validation → persistence. Tests and Party/Eval
// cross this seam instead of duplicating the sequence.
func (conductor *Conductor) runPreparedReview(ctx context.Context, prepared preparedReview, replaysReviewID *ReviewID, reviewStarted time.Time) (ReviewRecord, error) {
	return conductor.runner.runPreparedReview(ctx, prepared, replaysReviewID, reviewStarted)
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

// lifecycle delegation — the deep Review module owns these seams.
// Conductor keeps thin wrappers so existing callers (party, eval, tests) cross
// one seam without duplication.

func (conductor *Conductor) executePass(ctx context.Context, pass passExecution) (ReviewRecord, error) {
	if conductor.runner == nil {
		runner := newReviewRunner(conductor.store, func() time.Time { return conductor.now() }, func() RuntimeProvenance { return conductor.buildProvenance() }, conductor.artifacts)
		return runner.executePass(ctx, pass)
	}
	return conductor.runner.executePass(ctx, pass)
}

func (conductor *Conductor) finishIncomplete(record ReviewRecord, termination ReviewTermination, reviewStarted time.Time) (ReviewRecord, error) {
	if conductor.runner == nil {
		runner := newReviewRunner(conductor.store, func() time.Time { return conductor.now() }, func() RuntimeProvenance { return conductor.buildProvenance() }, conductor.artifacts)
		return runner.finishIncomplete(record, termination, reviewStarted)
	}
	return conductor.runner.finishIncomplete(record, termination, reviewStarted)
}

func (conductor *Conductor) finalizeOperationalRecord(record *ReviewRecord, reviewStarted time.Time) {
	if conductor.runner == nil {
		runner := newReviewRunner(conductor.store, func() time.Time { return conductor.now() }, func() RuntimeProvenance { return conductor.buildProvenance() }, conductor.artifacts)
		runner.finalizeOperationalRecord(record, reviewStarted)
		return
	}
	conductor.runner.finalizeOperationalRecord(record, reviewStarted)
}

func (conductor *Conductor) buildAttempt(id ReviewID, prompt string, candidate reviewerCandidate, execution attemptExecution, outcome AttemptOutcome, started, completed time.Time) (AttemptRecord, error) {
	if conductor.runner == nil {
		runner := &reviewRunner{artifacts: conductor.artifacts}
		return runner.buildAttempt(id, prompt, candidate, execution, outcome, started, completed)
	}
	return conductor.runner.buildAttempt(id, prompt, candidate, execution, outcome, started, completed)
}

func (conductor *Conductor) publishAttemptArtifacts(id ReviewID, number int, prompt string, execution attemptExecution) ([]ArtifactReference, error) {
	if conductor.runner == nil {
		runner := &reviewRunner{artifacts: conductor.artifacts}
		return runner.publishAttemptArtifacts(id, number, prompt, execution)
	}
	return conductor.runner.publishAttemptArtifacts(id, number, prompt, execution)
}

func (conductor *Conductor) removeArtifacts(references []ArtifactReference) {
	if conductor.runner == nil {
		runner := &reviewRunner{artifacts: conductor.artifacts}
		runner.removeArtifacts(references)
		return
	}
	conductor.runner.removeArtifacts(references)
}

func (conductor *Conductor) VerifyArtifacts(record ReviewRecord) error {
	if conductor.runner == nil {
		runner := &reviewRunner{artifacts: conductor.artifacts}
		return runner.VerifyArtifacts(record)
	}
	return conductor.runner.VerifyArtifacts(record)
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
