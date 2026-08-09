package reviewparty

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	RecordDirectory       string
	AttemptDeadline       time.Duration
	UserConfigurationPath string
}

type Conductor struct {
	store           recordStore
	reviewers       reviewerCatalog
	attemptDeadline time.Duration
	now             func() time.Time
}

func New(config Config) (*Conductor, error) {
	if config.RecordDirectory == "" {
		config.RecordDirectory = defaultRecordDirectory()
	}
	if config.AttemptDeadline <= 0 {
		config.AttemptDeadline = 10 * time.Minute
	}
	store, err := newFileRecordStore(config.RecordDirectory)
	if err != nil {
		return nil, err
	}
	userConfiguration, err := loadUserConfiguration(config.UserConfigurationPath)
	if err != nil {
		return nil, err
	}
	reviewers, err := configureReviewerCatalog(defaultReviewerCatalog(), userConfiguration, config.UserConfigurationPath)
	if err != nil {
		return nil, err
	}
	return newConductorWithCatalog(store, reviewers, config.AttemptDeadline), nil
}

func newConductor(store recordStore, executors map[string]attemptExecutor, deadline time.Duration) *Conductor {
	return newConductorWithCatalog(store, catalogWithExecutors(executors), deadline)
}

func newConductorWithCatalog(store recordStore, reviewers reviewerCatalog, deadline time.Duration) *Conductor {
	return &Conductor{
		store:           store,
		reviewers:       reviewers,
		attemptDeadline: deadline,
		now:             time.Now,
	}
}

func (conductor *Conductor) Review(ctx context.Context, selection ReviewSelection) (ReviewRecord, error) {
	if err := ctx.Err(); err != nil {
		return ReviewRecord{}, err
	}
	profile, err := compileProfile(conductor.reviewers, selection.profileSelection(), conductor.attemptDeadline)
	if err != nil {
		return ReviewRecord{}, err
	}
	subject, err := resolveSubject(selection.Repository, selection.Subject)
	if err != nil {
		return ReviewRecord{}, err
	}
	record, err := conductor.pendingRecord(subject, profile)
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

	executor := profile.reviewer.executor
	check := executor.Check(ctx, profile.reviewer.candidate)
	if !check.Available {
		return conductor.finishIncomplete(record, check.Diagnostic)
	}
	return conductor.executePass(ctx, record, profile, executor)
}

func (conductor *Conductor) Profiles(ctx context.Context) ([]ProfileSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return profileSummaries(conductor.reviewers)
}

func (conductor *Conductor) Explain(ctx context.Context, selection ProfileSelection) (ProfileExplanation, error) {
	if err := ctx.Err(); err != nil {
		return ProfileExplanation{}, err
	}
	profile, err := compileProfile(conductor.reviewers, selection, conductor.attemptDeadline)
	if err != nil {
		return ProfileExplanation{}, err
	}
	return ProfileExplanation{
		ProfileRevision:    profile.revision,
		ReviewerWasDefault: profile.reviewerWasDefault,
	}, nil
}

func (conductor *Conductor) Inspect(_ context.Context, id ReviewID) (ReviewRecord, error) {
	if !validReviewID(id) {
		return ReviewRecord{}, fmt.Errorf("invalid review id %q", id)
	}
	return conductor.store.Load(id)
}

func (conductor *Conductor) pendingRecord(subject ReviewSubject, profile compiledProfile) (ReviewRecord, error) {
	id, err := newReviewID(conductor.now())
	if err != nil {
		return ReviewRecord{}, err
	}
	now := conductor.now().UTC()
	passes := make([]PassRecord, 0, len(profile.revision.Passes))
	for _, planned := range profile.revision.Passes {
		passes = append(passes, PassRecord{Name: planned.Name, Required: planned.Required, Attempts: []AttemptRecord{}})
	}
	return ReviewRecord{
		ID:              id,
		Lifecycle:       LifecyclePending,
		Subject:         subject,
		ProfileRevision: profile.revision,
		Passes:          passes,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func (conductor *Conductor) executePass(ctx context.Context, record ReviewRecord, profile compiledProfile, executor attemptExecutor) (ReviewRecord, error) {
	started := conductor.now().UTC()
	attemptContext, cancel := context.WithTimeout(ctx, conductor.attemptDeadline)
	defer cancel()
	execution := executor.Execute(attemptContext, attemptSpec{
		Repository: record.Subject.Repository,
		Prompt:     profile.prompt(record.Subject),
		Candidate:  profile.reviewer.candidate,
	})

	result, parseErr := canonicalReviewResultContract.parse(execution.AssistantText)
	outcome := execution.Outcome
	if outcome == AttemptCompleted && parseErr == nil {
		record.Result = &result
		record.Lifecycle = LifecycleCompleted
	} else {
		if outcome == AttemptCompleted {
			outcome = AttemptInvalidResult
		} else if outcome == "" {
			outcome = AttemptUnknownFailure
		}
		record.Lifecycle = LifecycleIncomplete
		record.IncompleteCause = incompleteCause(outcome, execution.Diagnostic, parseErr)
	}
	record.Passes[0].Attempts = append(record.Passes[0].Attempts, AttemptRecord{
		Number:      1,
		Outcome:     outcome,
		Provenance:  resolvedProvenance(profile.reviewer.candidate, execution),
		Diagnostic:  execution.Diagnostic,
		RawOutput:   boundedAttemptOutput(execution.AssistantText),
		StartedAt:   started,
		CompletedAt: conductor.now().UTC(),
	})
	record.UpdatedAt = conductor.now().UTC()
	if err := conductor.store.Save(record); err != nil {
		return record, err
	}
	return record, nil
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

func (conductor *Conductor) finishIncomplete(record ReviewRecord, cause string) (ReviewRecord, error) {
	record.Lifecycle = LifecycleIncomplete
	record.IncompleteCause = cause
	record.UpdatedAt = conductor.now().UTC()
	if err := conductor.store.Save(record); err != nil {
		return record, err
	}
	return record, nil
}

func incompleteCause(outcome AttemptOutcome, diagnostic string, parseErr error) string {
	if outcome == AttemptInvalidResult && parseErr != nil {
		if diagnostic == "" {
			return parseErr.Error()
		}
		return parseErr.Error() + "; adapter diagnostic: " + diagnostic
	}
	if diagnostic != "" {
		return diagnostic
	}
	if parseErr != nil {
		return parseErr.Error()
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
		if character != '_' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func defaultRecordDirectory() string {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "review-party", "records")
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".local", "state", "review-party", "records")
	}
	return filepath.Join(os.TempDir(), "review-party-records")
}
