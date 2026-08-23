package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func (conductor *Conductor) RunEvalSuite(ctx context.Context, selection model.EvalSuiteSelection) (model.EvalSuiteRun, error) {
	suite, ledger, run, err := conductor.prepareEvalSuiteRun(selection)
	if err != nil {
		return model.EvalSuiteRun{}, err
	}
	defer suite.cleanup()
	deadline, _ := time.ParseDuration(run.Experiment.Deadline)
	executionContext, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	return conductor.executeEvalCases(executionContext, suite, ledger, run)
}

func (conductor *Conductor) prepareEvalSuiteRun(selection model.EvalSuiteSelection) (loadedEvalSuite, store.EvalRunStore, model.EvalSuiteRun, error) {
	if err := conductor.validateEvalSelection(selection.Experiment); err != nil {
		return loadedEvalSuite{}, nil, model.EvalSuiteRun{}, err
	}
	if err := validateEvalSuiteProfile(selection.Suite, selection.Experiment.Profile); err != nil {
		return loadedEvalSuite{}, nil, model.EvalSuiteRun{}, err
	}
	suite, err := loadEvalSuite(selection.Suite)
	if err != nil {
		return loadedEvalSuite{}, nil, model.EvalSuiteRun{}, err
	}
	ledger, ok := conductor.store.(store.EvalRunStore)
	if !ok {
		suite.cleanup()
		return loadedEvalSuite{}, nil, model.EvalSuiteRun{}, errors.New("eval execution requires the SQLite ledger")
	}
	started := conductor.now().UTC()
	run, evalRuns, err := conductor.newEvalSuiteRun(suite, selection.Experiment, started)
	if err != nil {
		suite.cleanup()
		return loadedEvalSuite{}, nil, model.EvalSuiteRun{}, err
	}
	if err := ledger.CreateEvalSuiteRun(run, evalRuns); err != nil {
		suite.cleanup()
		return loadedEvalSuite{}, nil, model.EvalSuiteRun{}, err
	}
	return suite, ledger, run, nil
}

func (conductor *Conductor) newEvalSuiteRun(suite loadedEvalSuite, experiment model.ExperimentConfiguration, started time.Time) (model.EvalSuiteRun, []model.EvalRun, error) {
	suiteID, err := newDomainID("esr", started)
	if err != nil {
		return model.EvalSuiteRun{}, nil, err
	}
	run := model.EvalSuiteRun{ID: model.EvalSuiteRunID(suiteID), Suite: suite.name, SuiteRevision: suite.revision, SuiteDigest: suite.digest, Experiment: experiment, Lifecycle: model.LifecyclePending, EvalRunIDs: make([]model.EvalRunID, 0, len(suite.cases)), StartedAt: started}
	evalRuns := make([]model.EvalRun, 0, len(suite.cases))
	for _, evalCase := range suite.cases {
		evalRun, err := conductor.newPendingEvalRun(run.ID, evalCase.revision)
		if err != nil {
			return model.EvalSuiteRun{}, nil, err
		}
		run.EvalRunIDs = append(run.EvalRunIDs, evalRun.ID)
		evalRuns = append(evalRuns, evalRun)
	}
	return run, evalRuns, nil
}

func validateEvalSuiteProfile(suite, profile string) error {
	if suite == "global:code-quality" && profile != "code-quality" {
		return fmt.Errorf("eval suite %q requires the code-quality Profile, got %q", suite, profile)
	}
	return nil
}

func (conductor *Conductor) validateEvalSelection(experiment model.ExperimentConfiguration) error {
	if err := validateExperiment(experiment, conductor.attemptDeadline); err != nil {
		return err
	}
	selection := ProfileSelection{Profile: experiment.Profile, Reviewer: experiment.Reviewer, Model: experiment.Model, Effort: experiment.Effort}
	_, err := conductor.compileFilesystemProfile(selection, "")
	return err
}

func (conductor *Conductor) executeEvalCases(ctx context.Context, suite loadedEvalSuite, ledger store.EvalRunStore, run model.EvalSuiteRun) (model.EvalSuiteRun, error) {
	if run.Experiment.ConcurrencyLimit > 1 {
		return conductor.executeEvalCasesConcurrent(ctx, suite, ledger, run)
	}
	for index, evalCase := range suite.cases {
		if err := ctx.Err(); err != nil {
			return conductor.stopEvalSuite(ledger, &run, evalFailureCategory(err), err)
		}
		evalRun, err := ledger.LoadEvalRun(run.EvalRunIDs[index])
		if err != nil {
			return conductor.stopEvalSuite(ledger, &run, model.TerminationUnknownFailure, err)
		}
		if err := validatePreparedEvalCase(evalRun, evalCase); err != nil {
			return conductor.stopEvalSuite(ledger, &run, model.TerminationUnknownFailure, err)
		}
		evalRun.ExecutionState = model.EvalRunning
		evalRun.UpdatedAt = conductor.now().UTC()
		run.Lifecycle = model.LifecycleRunning
		if err := ledger.CheckpointEvalRun(run, evalRun); err != nil {
			return conductor.stopEvalSuite(ledger, &run, model.TerminationUnknownFailure, err)
		}
		request := evalCaseExecution{ledger: ledger, suiteRun: run, evalRun: evalRun, evalCase: evalCase}
		next, err := conductor.executeEvalCase(ctx, request)
		run = next
		if err != nil {
			return run, err
		}
	}
	return run, nil
}

func validatePreparedEvalCase(evalRun model.EvalRun, evalCase preparedEvalCase) error {
	if evalRun.Case.ID != evalCase.revision.ID || evalRun.Case.Digest != evalCase.revision.Digest {
		return fmt.Errorf("Eval Run %q case revision does not match prepared case %q", evalRun.ID, evalCase.revision.ID)
	}
	return nil
}

type evalCaseExecution struct {
	ledger   store.EvalRunStore
	suiteRun model.EvalSuiteRun
	evalRun  model.EvalRun
	evalCase preparedEvalCase
}

func (conductor *Conductor) executeEvalCase(ctx context.Context, execution evalCaseExecution) (model.EvalSuiteRun, error) {
	review, err := conductor.executeEvalReview(ctx, execution.suiteRun.Experiment, execution.evalCase)
	if err != nil {
		if review.ID != "" {
			execution.evalRun.ReviewID = review.ID
		}
		return conductor.failEvalCase(execution, evalFailureCategory(err), err)
	}
	transition := evalTransition{suiteRun: execution.suiteRun, evalRun: execution.evalRun, completed: conductor.now().UTC()}
	nextSuite, nextRun := completedEvalCase(transition, review)
	if err := execution.ledger.CheckpointEvalRun(nextSuite, nextRun); err != nil {
		execution.evalRun = nextRun
		return conductor.failEvalCase(execution, model.TerminationUnknownFailure, err)
	}
	return nextSuite, nil
}

func (conductor *Conductor) executeEvalReview(ctx context.Context, experiment model.ExperimentConfiguration, evalCase preparedEvalCase) (ReviewRecord, error) {
	return conductor.reviewEvalCase(ctx, model.ReviewSelection{Subject: model.CapturedChange(evalCase.base, evalCase.head), Profile: experiment.Profile, Reviewer: experiment.Reviewer, Model: experiment.Model, Effort: experiment.Effort}, experiment.RetryPolicy)
}

type evalTransition struct {
	suiteRun  model.EvalSuiteRun
	evalRun   model.EvalRun
	completed time.Time
}

func completedEvalCase(transition evalTransition, review ReviewRecord) (model.EvalSuiteRun, model.EvalRun) {
	transition.evalRun.ReviewID = review.ID
	transition.evalRun.ExecutionState = evalExecutionState(review)
	transition.evalRun.AdjudicationState = model.EvalAwaitingAdjudication
	transition.evalRun.UpdatedAt = transition.completed
	addEvalExecutionCount(&transition.suiteRun, transition.evalRun.ExecutionState)
	completed := transition.suiteRun.CompletedCleanCount + transition.suiteRun.CompletedFindingCount + transition.suiteRun.IncompleteCount
	if completed == len(transition.suiteRun.EvalRunIDs) {
		transition.suiteRun.Lifecycle = model.LifecycleCompleted
		transition.suiteRun.CompletedAt = transition.completed
	}
	return transition.suiteRun, transition.evalRun
}

func (conductor *Conductor) failEvalCase(execution evalCaseExecution, category model.TerminationCategory, cause error) (model.EvalSuiteRun, error) {
	transition := evalTransition{suiteRun: execution.suiteRun, evalRun: execution.evalRun, completed: conductor.now().UTC()}
	suiteRun, evalRun := failedEvalCase(transition, category, cause)
	if err := execution.ledger.CheckpointEvalRun(suiteRun, evalRun); err != nil {
		return suiteRun, errors.Join(cause, err)
	}
	return suiteRun, cause
}

func failedEvalCase(transition evalTransition, category model.TerminationCategory, cause error) (model.EvalSuiteRun, model.EvalRun) {
	transition.evalRun.ExecutionState = model.EvalIncomplete
	transition.evalRun.AdjudicationState = model.EvalAdjudicationNotReady
	if transition.evalRun.ReviewID != "" {
		transition.evalRun.AdjudicationState = model.EvalAwaitingAdjudication
	}
	transition.evalRun.UpdatedAt = transition.completed
	addEvalExecutionCount(&transition.suiteRun, model.EvalIncomplete)
	transition.suiteRun.Lifecycle = model.LifecycleIncomplete
	transition.suiteRun.Termination = &model.EvalSuiteTermination{Category: category, Message: cause.Error()}
	transition.suiteRun.CompletedAt = transition.completed
	return transition.suiteRun, transition.evalRun
}

func evalFailureCategory(err error) model.TerminationCategory {
	if errors.Is(err, context.Canceled) {
		return model.TerminationCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return model.TerminationDeadlineExceeded
	}
	return model.TerminationUnknownFailure
}

func (conductor *Conductor) newPendingEvalRun(suiteRunID model.EvalSuiteRunID, revision model.EvalCaseRevision) (model.EvalRun, error) {
	created := conductor.now().UTC()
	id, err := newDomainID("er", created)
	if err != nil {
		return model.EvalRun{}, err
	}
	return model.EvalRun{ID: model.EvalRunID(id), SuiteRunID: suiteRunID, Case: revision, ExecutionState: model.EvalPending, AdjudicationState: model.EvalAdjudicationNotReady, CreatedAt: created, UpdatedAt: created}, nil
}

func (conductor *Conductor) stopEvalSuite(ledger store.EvalRunStore, run *model.EvalSuiteRun, category model.TerminationCategory, cause error) (model.EvalSuiteRun, error) {
	run.Lifecycle = model.LifecycleIncomplete
	run.Termination = &model.EvalSuiteTermination{Category: category, Message: cause.Error()}
	run.CompletedAt = conductor.now().UTC()
	if err := ledger.TerminateEvalSuiteRun(*run); err != nil {
		return *run, err
	}
	return *run, cause
}

func (conductor *Conductor) InspectEvalSuiteRun(_ context.Context, id model.EvalSuiteRunID) (model.EvalSuiteRun, error) {
	ledger, ok := conductor.store.(store.EvalRunStore)
	if !ok {
		return model.EvalSuiteRun{}, errors.New("eval inspection requires the SQLite ledger")
	}
	return ledger.LoadEvalSuiteRun(id)
}

func (conductor *Conductor) InspectEvalRun(_ context.Context, id model.EvalRunID) (model.EvalRun, error) {
	ledger, ok := conductor.store.(store.EvalRunStore)
	if !ok {
		return model.EvalRun{}, errors.New("eval inspection requires the SQLite ledger")
	}
	return ledger.LoadEvalRun(id)
}

func validateExperiment(experiment model.ExperimentConfiguration, deadline time.Duration) error {
	if err := validateExperimentSelection(experiment); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(experiment.Deadline)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("eval experiment has invalid deadline %q", experiment.Deadline)
	}
	if parsed != deadline {
		return fmt.Errorf("eval experiment deadline %s does not match Conductor deadline %s", parsed, deadline)
	}
	if err := validateRetryPolicy(experiment.RetryPolicy); err != nil {
		return err
	}
	if experiment.ConcurrencyLimit < 1 {
		return errors.New("eval experiment concurrency limit must be positive")
	}
	return nil
}

func validateExperimentSelection(experiment model.ExperimentConfiguration) error {
	if experiment.Profile == "" {
		return errors.New("eval experiment requires an explicit profile")
	}
	if experiment.Reviewer == "" {
		return errors.New("eval experiment requires an explicit reviewer")
	}
	if experiment.Model == "" {
		return errors.New("eval experiment requires an explicit model")
	}
	return nil
}

func validateRetryPolicy(policy model.RetryPolicy) error {
	if policy.MaxAttempts < 1 {
		return errors.New("eval experiment retry policy requires at least one attempt")
	}
	initial, initialErr := time.ParseDuration(policy.InitialBackoff)
	maximum, maximumErr := time.ParseDuration(policy.MaxBackoff)
	if initialErr != nil || maximumErr != nil {
		return errors.New("eval experiment retry policy requires valid duration backoff")
	}
	if initial <= 0 || maximum < initial {
		return errors.New("eval experiment retry policy requires positive backoff with max_backoff at least initial_backoff")
	}
	return nil
}

func evalExecutionState(review ReviewRecord) model.EvalExecutionState {
	if review.Lifecycle == LifecycleIncomplete {
		return model.EvalIncomplete
	}
	if review.Result != nil && review.Result.Status == ResultFindings {
		return model.EvalCompletedFindings
	}
	return model.EvalCompletedClean
}

func addEvalExecutionCount(run *model.EvalSuiteRun, state model.EvalExecutionState) {
	switch state {
	case model.EvalCompletedClean:
		run.CompletedCleanCount++
	case model.EvalCompletedFindings:
		run.CompletedFindingCount++
	case model.EvalIncomplete:
		run.IncompleteCount++
	}
}

func newDomainID(prefix string, now time.Time) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%d_%s", prefix, now.UTC().UnixMilli(), hex.EncodeToString(random)), nil
}
