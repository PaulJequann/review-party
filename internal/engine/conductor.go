package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reviewparty/internal/provenance"
	"reviewparty/internal/subject"

	"reviewparty/internal/artifact"
	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"time"
)

type Config struct {
	AttemptDeadline time.Duration
	// UserConfigurationPath optionally overrides the canonical Global
	// Configuration file location for tests and explicit --config flags.
	UserConfigurationPath string
}

type Conductor struct {
	store               store.RecordStore
	reviewers           reviewerCatalog
	configuration       *configuration.Manager
	profiles            profileLibrary
	evalDefaultDeadline time.Duration
	now                 func() time.Time
	buildProvenance     func() model.RuntimeProvenance
	artifacts           *artifact.Store
	retryDelay          func(model.RetryPolicy, int, time.Duration) time.Duration
	wait                func(context.Context, time.Duration) error
	runner              *reviewRunner
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
	store, err := store.NewDeferredLedgerRecordStore(stateDirectory)
	if err != nil {
		return nil, err
	}
	// Repository-scoped profile compilation applies the complete Global and
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
	conductor.runner.publisher = newArtifactPublisher(conductor.artifacts)
	return conductor, nil
}

func newConductor(store store.RecordStore, executors map[string]attemptExecutor, deadline time.Duration) (*Conductor, error) {
	return newConductorWithCatalog(store, catalogWithExecutors(executors), deadline)
}

func newConductorWithCatalog(store store.RecordStore, reviewers reviewerCatalog, deadline time.Duration) (*Conductor, error) {
	return newConductorWithProfiles(store, reviewers, newProfileLibrary(""), deadline)
}

func newConductorWithProfiles(store store.RecordStore, reviewers reviewerCatalog, profiles profileLibrary, deadline time.Duration) (*Conductor, error) {
	if profiles.configuration == nil {
		return nil, errProfileLibraryNotConfigured
	}
	conductor := &Conductor{
		store:               store,
		reviewers:           reviewers,
		configuration:       profiles.manager(),
		profiles:            profiles,
		evalDefaultDeadline: deadline,
		now:                 time.Now,
		buildProvenance:     provenance.CurrentRuntimeProvenance,
		retryDelay:          retryDelay,
		wait:                waitForRetry,
	}
	conductor.runner = newReviewRunner(store, func() time.Time { return conductor.now() }, func() model.RuntimeProvenance { return conductor.buildProvenance() }, newArtifactPublisher(nil))
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

func (conductor *Conductor) Review(ctx context.Context, selection model.ReviewSelection) (model.ReviewRecord, error) {
	if err := ctx.Err(); err != nil {
		return model.ReviewRecord{}, err
	}
	if err := conductor.requirePreparedState(selection.Repository); err != nil {
		return model.ReviewRecord{}, err
	}
	reviewStarted := conductor.now().UTC()
	prepared, err := conductor.prepareReview(selection, false)
	if err != nil {
		return model.ReviewRecord{}, err
	}
	return conductor.runPreparedReview(ctx, prepared, nil, reviewStarted)
}

// runPreparedReview is the deep lifecycle seam: one place owns pending →
// availability → execution → validation → persistence. Tests and Party/Eval
// cross this seam instead of duplicating the sequence.
func (conductor *Conductor) runPreparedReview(ctx context.Context, prepared preparedReview, replaysReviewID *model.ReviewID, reviewStarted time.Time) (model.ReviewRecord, error) {
	return conductor.getRunner().runPreparedReview(ctx, prepared, replaysReviewID, reviewStarted)
}

func (conductor *Conductor) Replay(ctx context.Context, selection model.ReplaySelection) (model.ReviewRecord, error) {
	if err := ctx.Err(); err != nil {
		return model.ReviewRecord{}, err
	}
	if !validReviewID(selection.SourceReviewID) {
		return model.ReviewRecord{}, fmt.Errorf("invalid source review id %q", selection.SourceReviewID)
	}
	if err := conductor.requirePreparedState("."); err != nil {
		return model.ReviewRecord{}, err
	}
	source, err := conductor.store.Load(selection.SourceReviewID)
	if err != nil {
		return model.ReviewRecord{}, fmt.Errorf("load replay source %q: %w", selection.SourceReviewID, err)
	}
	reviewStarted := conductor.now().UTC()
	prepared, err := conductor.prepareReplay(source, selection)
	if err != nil {
		return model.ReviewRecord{}, err
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
	subject  model.ReviewSubject
	profile  compiledProfile
	timings  model.ReviewTimings
	deadline time.Duration
}

func (conductor *Conductor) prepareReview(selection model.ReviewSelection, allowExperimentOverrides bool) (preparedReview, error) {
	if !allowExperimentOverrides && reviewHasExecutionOverrides(selection) {
		return preparedReview{}, errors.New("ordinary Reviews do not accept Reviewer, model, or effort overrides")
	}
	timings := model.ReviewTimings{}
	subjectStarted := conductor.now().UTC()
	repository, repositoryErr := resolveReviewRepository(selection)
	timings.SubjectResolutionMS += elapsedMilliseconds(subjectStarted, conductor.now().UTC())
	if repositoryErr != nil {
		return preparedReview{}, repositoryErr
	}
	resolved, err := conductor.profiles.resolve(profileRequest{repository: repository, name: selection.Profile})
	if err != nil {
		return preparedReview{}, err
	}
	profileStarted := conductor.now().UTC()
	profile, err := conductor.compilePreparedProfile(selection, resolved, allowExperimentOverrides)
	timings.ProfileCompilationMS = elapsedMilliseconds(profileStarted, conductor.now().UTC())
	if err != nil {
		return preparedReview{}, err
	}
	subjectStarted = conductor.now().UTC()
	subject, err := subject.ResolveSubject(repository, selection.Subject)
	timings.SubjectResolutionMS += elapsedMilliseconds(subjectStarted, conductor.now().UTC())
	if err != nil {
		return preparedReview{}, err
	}
	return preparedReview{subject: subject, profile: profile, timings: timings, deadline: profile.deadline}, nil
}

func reviewHasExecutionOverrides(selection model.ReviewSelection) bool {
	return selection.Reviewer != "" || selection.Model != "" || selection.Effort != ""
}

func (conductor *Conductor) compilePreparedProfile(selection model.ReviewSelection, resolved resolvedProfile, allowExperimentOverrides bool) (compiledProfile, error) {
	if !allowExperimentOverrides {
		return conductor.compileResolvedProfile(resolved)
	}
	experiment := selection.ProfileSelection()
	if experiment.Reviewer == "" {
		experiment.Reviewer = resolved.reviewer
	}
	if experiment.Model == "" {
		experiment.Model = resolved.model
	}
	if experiment.Effort == "" {
		experiment.Effort = resolved.effort
	}
	return conductor.compileProfile(experiment, resolved)
}

func resolveReviewRepository(selection model.ReviewSelection) (string, error) {
	if selection.Subject.Kind == model.SubjectCapturedChange {
		return selection.Repository, nil
	}
	return subject.ResolveRepositoryRoot(selection.Repository)
}

func (conductor *Conductor) Profiles(ctx context.Context) ([]model.ProfileSummary, error) {
	return conductor.profilesAt(ctx, "")
}

func (conductor *Conductor) ProfilesForRepository(ctx context.Context, repository string) ([]model.ProfileSummary, error) {
	root, err := subject.ResolveRepositoryRoot(repository)
	if err != nil {
		return nil, err
	}
	return conductor.profilesAt(ctx, root)
}

func (conductor *Conductor) profilesAt(ctx context.Context, repository string) ([]model.ProfileSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return conductor.profileSummaries(repository)
}

func (conductor *Conductor) Explain(ctx context.Context, selection model.ProfileSelection) (model.ProfileExplanation, error) {
	return conductor.explainAt(ctx, selection, "")
}

func (conductor *Conductor) ExplainForRepository(ctx context.Context, selection model.ProfileSelection, repository string) (model.ProfileExplanation, error) {
	root, err := subject.ResolveRepositoryRoot(repository)
	if err != nil {
		return model.ProfileExplanation{}, err
	}
	return conductor.explainAt(ctx, selection, root)
}

func (conductor *Conductor) explainAt(ctx context.Context, selection model.ProfileSelection, repository string) (model.ProfileExplanation, error) {
	if err := ctx.Err(); err != nil {
		return model.ProfileExplanation{}, err
	}
	if profileSelectionHasExecutionOverrides(selection) {
		return model.ProfileExplanation{}, errors.New("Profile explanation does not accept Reviewer, model, or effort overrides")
	}
	resolved, err := conductor.profiles.resolve(profileRequest{repository: repository, name: selection.Profile})
	if err != nil {
		return model.ProfileExplanation{}, err
	}
	profile, err := conductor.compileResolvedProfile(resolved)
	if err != nil {
		return model.ProfileExplanation{}, err
	}
	return model.ProfileExplanation{
		ProfileRevision:    profile.revision,
		ReviewerWasDefault: profile.reviewerWasDefault,
		Instructions:       profile.snapshot.Instructions,
	}, nil
}

func (conductor *Conductor) Inspect(_ context.Context, id model.ReviewID) (model.ReviewRecord, error) {
	if !validReviewID(id) {
		return model.ReviewRecord{}, fmt.Errorf("invalid review id %q", id)
	}
	record, err := conductor.store.Load(id)
	if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
		return model.ReviewRecord{}, InitializationRequiredError{Repository: "."}
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
// getRunner provides the single Review seam; runner is eagerly constructed
// and immutable after New, so concurrent Party/Eval callers share it without
// racy per-call writes. The trailing lazy branch supports literal
// &Conductor{artifacts: ...} in tests.
func (conductor *Conductor) getRunner() *reviewRunner {
	if conductor.runner == nil {
		conductor.runner = newReviewRunner(conductor.store, func() time.Time { return conductor.now() }, func() model.RuntimeProvenance { return conductor.buildProvenance() }, newArtifactPublisher(conductor.artifacts))
	}
	return conductor.runner
}

func (conductor *Conductor) VerifyArtifacts(record model.ReviewRecord) error {
	return conductor.getRunner().VerifyArtifacts(record)
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
