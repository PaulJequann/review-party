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
	return conductor.executeEvalCases(ctx, suite, ledger, run)
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
	suiteID, err := newDomainID("esr", started)
	if err != nil {
		suite.cleanup()
		return loadedEvalSuite{}, nil, model.EvalSuiteRun{}, err
	}
	run := model.EvalSuiteRun{ID: model.EvalSuiteRunID(suiteID), Suite: suite.name, SuiteRevision: suite.revision, SuiteDigest: suite.digest, Experiment: selection.Experiment, EvalRunIDs: []model.EvalRunID{}, StartedAt: started}
	if err := ledger.SaveEvalSuiteRun(run); err != nil {
		suite.cleanup()
		return loadedEvalSuite{}, nil, model.EvalSuiteRun{}, err
	}
	return suite, ledger, run, nil
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
	_, err := conductor.compileFilesystemProfile(ProfileSelection{Profile: experiment.Profile, Reviewer: experiment.Reviewer, Model: experiment.Model, Effort: experiment.Effort}, "")
	return err
}

func (conductor *Conductor) executeEvalCases(ctx context.Context, suite loadedEvalSuite, ledger store.EvalRunStore, run model.EvalSuiteRun) (model.EvalSuiteRun, error) {
	for _, evalCase := range suite.cases {
		if err := conductor.executeEvalCase(ctx, ledger, &run, evalCase); err != nil {
			return run, err
		}
	}
	run.CompletedAt = conductor.now().UTC()
	if err := ledger.SaveEvalSuiteRun(run); err != nil {
		return run, err
	}
	return run, nil
}

func (conductor *Conductor) executeEvalCase(ctx context.Context, ledger store.EvalRunStore, suiteRun *model.EvalSuiteRun, evalCase preparedEvalCase) error {
	experiment := suiteRun.Experiment
	review, err := conductor.Review(ctx, model.ReviewSelection{Subject: model.CapturedChange(evalCase.base, evalCase.head), Profile: experiment.Profile, Reviewer: experiment.Reviewer, Model: experiment.Model, Effort: experiment.Effort})
	if err != nil {
		return err
	}
	evalRun, err := newEvalRun(suiteRun.ID, evalCase.revision, review, conductor.now().UTC())
	if err != nil {
		return err
	}
	if err := ledger.SaveEvalRun(evalRun); err != nil {
		return err
	}
	suiteRun.EvalRunIDs = append(suiteRun.EvalRunIDs, evalRun.ID)
	addEvalExecutionCount(suiteRun, evalRun.ExecutionState)
	return ledger.SaveEvalSuiteRun(*suiteRun)
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
	if experiment.Profile == "" {
		return errors.New("eval experiment requires an explicit profile")
	}
	if experiment.Reviewer == "" {
		return errors.New("eval experiment requires an explicit reviewer")
	}
	if experiment.Model == "" {
		return errors.New("eval experiment requires an explicit model")
	}
	parsed, err := time.ParseDuration(experiment.Deadline)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("eval experiment has invalid deadline %q", experiment.Deadline)
	}
	if parsed != deadline {
		return fmt.Errorf("eval experiment deadline %s does not match Conductor deadline %s", parsed, deadline)
	}
	return nil
}

func newEvalRun(suiteRunID model.EvalSuiteRunID, revision model.EvalCaseRevision, review ReviewRecord, created time.Time) (model.EvalRun, error) {
	id, err := newDomainID("er", created)
	if err != nil {
		return model.EvalRun{}, err
	}
	return model.EvalRun{ID: model.EvalRunID(id), SuiteRunID: suiteRunID, Case: revision, ReviewID: review.ID, ExecutionState: evalExecutionState(review), AdjudicationState: "awaiting_adjudication", CreatedAt: created}, nil
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
