package engine

import (
	"context"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

type attemptGateContextKey struct{}

func attemptGateFromContext(ctx context.Context) chan struct{} {
	gate, ok := ctx.Value(attemptGateContextKey{}).(chan struct{})
	if !ok {
		return nil
	}
	return gate
}

type concurrentEvalResult struct {
	index   int
	evalRun model.EvalRun
	review  model.ReviewRecord
	err     error
}

func (conductor *Conductor) executeEvalCasesConcurrent(ctx context.Context, suite loadedEvalSuite, ledger store.EvalRunStore, run model.EvalSuiteRun) (model.EvalSuiteRun, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	runContext = context.WithValue(runContext, attemptGateContextKey{}, make(chan struct{}, run.Experiment.ConcurrencyLimit))
	results := make(chan concurrentEvalResult, len(suite.cases))
	started, err := conductor.startConcurrentEvalCases(runContext, suite, ledger, &run, results)
	if err != nil {
		return conductor.stopEvalSuite(ledger, &run, evalFailureCategory(err), err)
	}
	for completed := 0; completed < started; completed++ {
		next, err := conductor.acceptConcurrentEvalResult(ledger, suite, run, <-results)
		if err != nil {
			cancel()
			return next, err
		}
		run = next
	}
	return run, nil
}

func (conductor *Conductor) startConcurrentEvalCases(ctx context.Context, suite loadedEvalSuite, ledger store.EvalRunStore, run *model.EvalSuiteRun, results chan<- concurrentEvalResult) (int, error) {
	started := 0
	for index, evalCase := range suite.cases {
		if err := ctx.Err(); err != nil {
			return started, err
		}
		evalRun, err := ledger.LoadEvalRun(run.EvalRunIDs[index])
		if err != nil {
			return started, err
		}
		if err := validatePreparedEvalCase(evalRun, evalCase); err != nil {
			return started, err
		}
		evalRun.ExecutionState, evalRun.UpdatedAt = model.EvalRunning, conductor.now().UTC()
		run.Lifecycle = model.LifecycleRunning
		if err := ledger.CheckpointEvalRun(*run, evalRun); err != nil {
			return started, err
		}
		started++
		go conductor.runConcurrentEvalCase(ctx, *run, index, evalRun, evalCase, results)
	}
	return started, nil
}

func (conductor *Conductor) runConcurrentEvalCase(ctx context.Context, run model.EvalSuiteRun, index int, evalRun model.EvalRun, evalCase preparedEvalCase, results chan<- concurrentEvalResult) {
	review, err := conductor.executeEvalReview(ctx, run.Experiment, evalCase)
	results <- concurrentEvalResult{index: index, evalRun: evalRun, review: review, err: err}
}

func (conductor *Conductor) acceptConcurrentEvalResult(ledger store.EvalRunStore, suite loadedEvalSuite, run model.EvalSuiteRun, result concurrentEvalResult) (model.EvalSuiteRun, error) {
	execution := evalCaseExecution{ledger: ledger, suiteRun: run, evalRun: result.evalRun, evalCase: suite.cases[result.index]}
	if result.err != nil {
		if result.review.ID != "" {
			execution.evalRun.ReviewID = result.review.ID
		}
		return conductor.failEvalCase(execution, evalFailureCategory(result.err), result.err)
	}
	nextSuite, nextRun := completedEvalCase(evalTransition{suiteRun: run, evalRun: result.evalRun, completed: conductor.now().UTC()}, result.review)
	if err := ledger.CheckpointEvalRun(nextSuite, nextRun); err != nil {
		execution.suiteRun, execution.evalRun = nextSuite, nextRun
		return conductor.failEvalCase(execution, model.TerminationUnknownFailure, err)
	}
	return nextSuite, nil
}
