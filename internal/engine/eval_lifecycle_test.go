package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reviewparty/internal/artifact"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func checkedCleanup(t *testing.T, description string, cleanup func() error) func() {
	t.Helper()
	return func() {
		if err := cleanup(); err != nil {
			t.Errorf("%s: %v", description, err)
		}
	}
}

func TestEvalPlansEveryRunBeforeFirstHarnessLaunch(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}, {id: "third"}})
	ledger, observed := newObservedEvalStore(t)
	executor := &evalSequenceExecutor{outputs: []string{cleanReview, cleanReview, cleanReview}}
	executor.onExecute = func() {
		assertPlannedEvalRuns(t, ledger, observed.created.ID)
		executor.onExecute = nil
	}
	conductor := newLifecycleTestConductor(t, observed, executor)
	if _, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite)); err != nil {
		t.Fatal(err)
	}
}

func TestEvalCaseIdentityMismatchStopsBeforeReviewerLaunch(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}})
	_, observed := newObservedEvalStore(t)
	records := &mismatchedEvalLoadStore{observingEvalStore: observed}
	executor := successfulExecutor(cleanReview)
	conductor := newLifecycleTestConductor(t, records, executor)
	run, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
	if err == nil || !strings.Contains(err.Error(), "does not match prepared case") {
		t.Fatalf("error = %v", err)
	}
	if run.Lifecycle != model.LifecycleIncomplete || executor.attemptCount() != 0 {
		t.Fatalf("lifecycle=%s attempts=%d", run.Lifecycle, executor.attemptCount())
	}
}

type mismatchedEvalLoadStore struct {
	*observingEvalStore
}

func (records *mismatchedEvalLoadStore) LoadEvalRun(id model.EvalRunID) (model.EvalRun, error) {
	run, err := records.observingEvalStore.LoadEvalRun(id)
	if err == nil {
		run.Case.ID = "mismatched-case"
	}
	return run, err
}

func TestEvalCheckpointFailuresPersistHonestState(t *testing.T) {
	tests := []struct {
		name   string
		failAt int
		assert func(*testing.T, *store.LedgerRecordStore, model.EvalSuiteRunID, int)
	}{
		{name: "before reviewer launch", failAt: 1, assert: assertStoppedBeforeLaunch},
		{name: "after terminal review", failAt: 2, assert: func(t *testing.T, ledger *store.LedgerRecordStore, id model.EvalSuiteRunID, _ int) {
			assertIncompleteTerminalCheckpoint(t, ledger, id)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}})
			ledger, observed := newObservedEvalStore(t)
			failing := &failingEvalCheckpointStore{observingEvalStore: observed, failAt: test.failAt}
			executor := successfulExecutor(cleanReview)
			conductor := newLifecycleTestConductor(t, failing, executor)
			run, err := conductor.RunEvalSuite(testContext(t), evalSelection(suite))
			if err == nil || !strings.Contains(err.Error(), "injected checkpoint failure") {
				t.Fatalf("error = %v", err)
			}
			test.assert(t, ledger, run.ID, executor.attemptCount())
		})
	}
}

func assertIncompleteTerminalCheckpoint(t *testing.T, ledger *store.LedgerRecordStore, id model.EvalSuiteRunID) {
	t.Helper()
	persisted := loadEvalSuiteRun(t, ledger, id)
	first := loadStoredEvalRun(t, ledger, persisted.EvalRunIDs[0])
	if persisted.CompletedCleanCount != 0 {
		t.Fatalf("parent clean=%d", persisted.CompletedCleanCount)
	}
	if persisted.IncompleteCount != 1 {
		t.Fatalf("parent incomplete=%d", persisted.IncompleteCount)
	}
	if persisted.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("parent=%s", persisted.Lifecycle)
	}
	if first.ExecutionState != model.EvalIncomplete {
		t.Fatalf("child=%s", first.ExecutionState)
	}
	if first.ReviewID == "" {
		t.Fatal("review id is empty")
	}
	if first.AdjudicationState != model.EvalAwaitingAdjudication {
		t.Fatalf("adjudication=%s", first.AdjudicationState)
	}
}

func TestStoppedEvalSuiteRejectsPendingCaseAdjudicationExplicitly(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}})
	ctx, cancel := context.WithCancel(testContext(t))
	executor := &evalSequenceExecutor{outputs: []string{cleanReview}, onExecute: cancel}
	conductor := testEvalConductor(t, executor)
	run, runErr := conductor.RunEvalSuite(ctx, evalSelection(suite))
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("run error = %v", runErr)
	}
	_, err := conductor.ExportAdjudication(context.Background(), run.ID)
	if err == nil || !strings.Contains(err.Error(), "cannot be adjudicated before an ordinary Review exists") {
		t.Fatalf("adjudication error = %v", err)
	}
}

func TestEvalCancellationStopsNewCasesAndPersistsHonestState(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}})
	ctx, cancel := context.WithCancel(testContext(t))
	executor := &evalSequenceExecutor{outputs: []string{cleanReview}, onExecute: cancel}
	conductor := testEvalConductor(t, executor)
	run, err := conductor.RunEvalSuite(ctx, evalSelection(suite))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	assertCancelledEvalSuite(t, conductor, run.ID, len(executor.fileCounts))
}

func TestEvalSuiteDeadlineStopsNewCases(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}})
	executor := &deadlineEvalExecutor{}
	conductor := testEvalConductor(t, executor)
	conductor.evalDefaultDeadline = 20 * time.Millisecond
	selection := evalSelection(suite)
	selection.Experiment.Deadline = conductor.evalDefaultDeadline.String()
	run, err := conductor.RunEvalSuite(testContext(t), selection)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	persisted, loadErr := conductor.InspectEvalSuiteRun(context.Background(), run.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	second, loadErr := conductor.InspectEvalRun(context.Background(), persisted.EvalRunIDs[1])
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if persisted.Termination == nil || persisted.Termination.Category != model.TerminationDeadlineExceeded {
		t.Fatalf("termination = %#v", persisted.Termination)
	}
	if second.ExecutionState != model.EvalPending || executor.attempts != 1 {
		t.Fatalf("second=%s attempts=%d", second.ExecutionState, executor.attempts)
	}
}

type deadlineEvalExecutor struct {
	attempts int
}

func (executor *deadlineEvalExecutor) Check(context.Context, reviewerCandidate) availability {
	return availability{Available: true}
}

func (executor *deadlineEvalExecutor) Execute(ctx context.Context, _ attemptSpec) attemptExecution {
	executor.attempts++
	<-ctx.Done()
	return contextExecution(ctx.Err())
}

func TestEvalReviewErrorTerminalizesActiveChildWithParent(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}})
	ctx, cancel := context.WithCancel(testContext(t))
	_, observed := newObservedEvalStore(t)
	records := &cancellingEvalCheckpointStore{observingEvalStore: observed, cancel: cancel}
	conductor := newLifecycleTestConductor(t, records, successfulExecutor(cleanReview))
	run, err := conductor.RunEvalSuite(ctx, evalSelection(suite))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	persisted, loadErr := conductor.InspectEvalSuiteRun(context.Background(), run.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	first, loadErr := conductor.InspectEvalRun(context.Background(), persisted.EvalRunIDs[0])
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if persisted.Lifecycle != model.LifecycleIncomplete || first.ExecutionState != model.EvalIncomplete {
		t.Fatalf("parent=%s child=%s", persisted.Lifecycle, first.ExecutionState)
	}
	if persisted.IncompleteCount != 1 {
		t.Fatalf("incomplete=%d", persisted.IncompleteCount)
	}
	if first.AdjudicationState != model.EvalAdjudicationNotReady {
		t.Fatalf("adjudication=%s", first.AdjudicationState)
	}
}

type cancellingEvalCheckpointStore struct {
	*observingEvalStore
	cancel context.CancelFunc
}

func (records *cancellingEvalCheckpointStore) CheckpointEvalRun(run model.EvalSuiteRun, evalRun model.EvalRun) error {
	if err := records.observingEvalStore.CheckpointEvalRun(run, evalRun); err != nil {
		return err
	}
	if records.cancel != nil {
		records.cancel()
		records.cancel = nil
	}
	return nil
}

type observingEvalStore struct {
	*store.LedgerRecordStore
	created model.EvalSuiteRun
}

func (observed *observingEvalStore) CreateEvalSuiteRun(run model.EvalSuiteRun, evalRuns []model.EvalRun) error {
	observed.created = run
	return observed.LedgerRecordStore.CreateEvalSuiteRun(run, evalRuns)
}

type failingEvalCheckpointStore struct {
	*observingEvalStore
	failAt int
	calls  int
}

func (failing *failingEvalCheckpointStore) CheckpointEvalRun(run model.EvalSuiteRun, evalRun model.EvalRun) error {
	failing.calls++
	if failing.calls == failing.failAt {
		return errors.New("injected checkpoint failure")
	}
	return failing.observingEvalStore.CheckpointEvalRun(run, evalRun)
}

func newObservedEvalStore(t *testing.T) (*store.LedgerRecordStore, *observingEvalStore) {
	t.Helper()
	ledger, err := store.NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(checkedCleanup(t, "close ledger", ledger.Close))
	return ledger, &observingEvalStore{LedgerRecordStore: ledger}
}

func newLifecycleTestConductor(t *testing.T, records store.RecordStore, executor attemptExecutor) *Conductor {
	t.Helper()
	conductor, err := newConductorWithManager(records, catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor}), newTestConfigurationManager(t), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conductor.artifacts, err = artifact.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return conductor
}

func assertPlannedEvalRuns(t *testing.T, ledger *store.LedgerRecordStore, id model.EvalSuiteRunID) {
	t.Helper()
	suite := loadEvalSuiteRun(t, ledger, id)
	if suite.Lifecycle != model.LifecycleRunning || len(suite.EvalRunIDs) != 3 {
		t.Fatalf("first launch saw suite lifecycle=%s runs=%d", suite.Lifecycle, len(suite.EvalRunIDs))
	}
	wants := []model.EvalExecutionState{model.EvalRunning, model.EvalPending, model.EvalPending}
	for index, runID := range suite.EvalRunIDs {
		run := loadStoredEvalRun(t, ledger, runID)
		if run.ExecutionState != wants[index] {
			t.Fatalf("run %d state = %s, want %s", index, run.ExecutionState, wants[index])
		}
	}
}

func assertStoppedBeforeLaunch(t *testing.T, ledger *store.LedgerRecordStore, id model.EvalSuiteRunID, attempts int) {
	t.Helper()
	suite := loadEvalSuiteRun(t, ledger, id)
	first := loadStoredEvalRun(t, ledger, suite.EvalRunIDs[0])
	if suite.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("suite lifecycle = %s", suite.Lifecycle)
	}
	if first.ExecutionState != model.EvalPending || attempts != 0 {
		t.Fatalf("first=%s attempts=%d", first.ExecutionState, attempts)
	}
}

func assertCancelledEvalSuite(t *testing.T, conductor *Conductor, id model.EvalSuiteRunID, attempts int) {
	t.Helper()
	suite, err := conductor.InspectEvalSuiteRun(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	assertCancelledSuiteState(t, suite)
	second, err := conductor.InspectEvalRun(context.Background(), suite.EvalRunIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	assertPendingUnstartedEvalRun(t, second, attempts)
}

func assertCancelledSuiteState(t *testing.T, suite model.EvalSuiteRun) {
	t.Helper()
	if suite.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("suite lifecycle = %s", suite.Lifecycle)
	}
	if suite.Termination == nil || suite.Termination.Category != model.TerminationCancelled {
		t.Fatalf("suite termination = %#v", suite.Termination)
	}
}

func assertPendingUnstartedEvalRun(t *testing.T, second model.EvalRun, attempts int) {
	t.Helper()
	if second.ExecutionState != model.EvalPending || second.ReviewID != "" {
		t.Fatalf("second run = %#v", second)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func loadEvalSuiteRun(t *testing.T, ledger *store.LedgerRecordStore, id model.EvalSuiteRunID) model.EvalSuiteRun {
	t.Helper()
	run, err := ledger.LoadEvalSuiteRun(id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func loadStoredEvalRun(t *testing.T, ledger *store.LedgerRecordStore, id model.EvalRunID) model.EvalRun {
	t.Helper()
	run, err := ledger.LoadEvalRun(id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestConcurrentEvalWaitsForStartedReviewersAfterResultFailure(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}, {id: "third"}})
	_, observed := newObservedEvalStore(t)
	failing := &failingEvalCheckpointStore{observingEvalStore: observed, failAt: 4}
	executor := newLingeringEvalExecutor(3, true)
	conductor := newLifecycleTestConductor(t, failing, executor)

	assertConcurrentEvalDrainsReviewers(t, conductor, suite, executor)
}

func TestConcurrentEvalWaitsForStartedReviewersAfterStartFailure(t *testing.T) {
	suite := writeEvalTestSuite(t, []testEvalCase{{id: "first"}, {id: "second"}})
	_, observed := newObservedEvalStore(t)
	executor := newLingeringEvalExecutor(1, false)
	failing := &failingEvalCheckpointStore{observingEvalStore: observed, failAt: 2}
	records := &startGatedEvalStore{failingEvalCheckpointStore: failing, gate: executor.allIn}
	conductor := newLifecycleTestConductor(t, records, executor)

	assertConcurrentEvalDrainsReviewers(t, conductor, suite, executor)
}

func assertConcurrentEvalDrainsReviewers(t *testing.T, conductor *Conductor, suite string, executor *lingeringEvalExecutor) {
	t.Helper()
	selection := evalSelection(suite)
	selection.Experiment.ConcurrencyLimit = 3

	_, err := conductor.RunEvalSuite(testContext(t), selection)

	if err == nil || !strings.Contains(err.Error(), "injected checkpoint failure") {
		t.Fatalf("error = %v", err)
	}
	if entered, finished := executor.counts(); entered != executor.expected || finished != entered {
		t.Fatalf("reviewers entered=%d finished=%d at return, want %d entered and all finished", entered, finished, executor.expected)
	}
}

type startGatedEvalStore struct {
	*failingEvalCheckpointStore
	gate <-chan struct{}
}

func (records *startGatedEvalStore) CheckpointEvalRun(run model.EvalSuiteRun, evalRun model.EvalRun) error {
	if records.calls == records.failAt-1 {
		<-records.gate
	}
	return records.failingEvalCheckpointStore.CheckpointEvalRun(run, evalRun)
}

type lingeringEvalExecutor struct {
	mu             sync.Mutex
	expected       int
	firstCompletes bool
	entered        int
	finished       int
	allIn          chan struct{}
}

func newLingeringEvalExecutor(expected int, firstCompletes bool) *lingeringEvalExecutor {
	return &lingeringEvalExecutor{expected: expected, firstCompletes: firstCompletes, allIn: make(chan struct{})}
}

func (executor *lingeringEvalExecutor) Check(context.Context, reviewerCandidate) availability {
	return availability{Available: true}
}

func (executor *lingeringEvalExecutor) Execute(ctx context.Context, _ attemptSpec) attemptExecution {
	executor.mu.Lock()
	executor.entered++
	first := executor.entered == 1
	if executor.entered == executor.expected {
		close(executor.allIn)
	}
	executor.mu.Unlock()
	defer executor.finish()
	if first && executor.firstCompletes {
		<-executor.allIn
		return attemptExecution{Outcome: model.AttemptCompleted, AssistantText: cleanReview}
	}
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)
	return contextExecution(ctx.Err())
}

func (executor *lingeringEvalExecutor) finish() {
	executor.mu.Lock()
	executor.finished++
	executor.mu.Unlock()
}

func (executor *lingeringEvalExecutor) counts() (int, int) {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return executor.entered, executor.finished
}
