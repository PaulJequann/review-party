package engine

import (
	"context"
	"errors"
	"testing"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func twoMemberSequentialSelection(t *testing.T, repository string) {
	t.Helper()
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}, {Profile: "code-quality"}},
		Repository:       []configuration.SelectionItem{},
	})
}

func TestRunRecordsEveryMemberBeforeTheFirstLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	var conductor *Conductor
	var observed []model.Lifecycle
	executor := &scriptedExecutor{availability: availability{Available: true}}
	executor.execute = func(ctx context.Context, _ attemptSpec) attemptExecution {
		if observed == nil {
			page, err := conductor.History(ctx, store.HistoryQuery{Limit: 10})
			if err != nil {
				t.Error(err)
			}
			for _, entry := range page.Entries {
				observed = append(observed, entry.Lifecycle)
			}
		}
		return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
	}
	conductor = testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	twoMemberSequentialSelection(t, repository)

	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	requireBundleOutcome(t, bundle, 2, model.LifecycleCompleted)
	if len(observed) != 2 || !containsLifecycle(observed, model.LifecycleRunning) || !containsLifecycle(observed, model.LifecyclePending) {
		t.Fatalf("ledger during the first attempt = %v, want one running and one pending Review", observed)
	}
}

func TestStoppedBundleFinalizesMembersThatNeverStarted(t *testing.T) {
	repository := changedTestRepository(t)
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	executor := &scriptedExecutor{availability: availability{Available: true}}
	executor.execute = func(context.Context, attemptSpec) attemptExecution {
		cancel()
		return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
	}
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	recorder := &runProgressRecorder{}
	conductor.progress = recorder.record
	twoMemberSequentialSelection(t, repository)

	bundle, err := conductor.Run(ctx, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if bundle.Lifecycle != model.LifecycleIncomplete || bundle.Termination == nil || bundle.Termination.Category != model.TerminationCancelled {
		t.Fatalf("bundle = %#v, want an incomplete bundle stopped by cancellation", bundle)
	}
	skipped := bundle.Members[1]
	if skipped.ReviewID == "" || skipped.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("unstarted member = %#v, want an incomplete Review Record", skipped)
	}
	record, err := conductor.Inspect(context.Background(), skipped.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	assertTermination(t, record, model.TerminationCancelled, model.PhaseAvailabilityCheck)
	if record.AttemptCount() != 0 {
		t.Fatalf("unstarted member attempts = %d, want 0", record.AttemptCount())
	}
	requireAttemptCount(t, executor, 1)
	requireStoppedMemberProgress(t, recorder.collected(), skipped.ReviewID)
	persisted := inspectBundleRecord(t, conductor, bundle.ID)
	if persisted.Members[1].Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("persisted unstarted member = %#v, want incomplete", persisted.Members[1])
	}
}

func requireStoppedMemberProgress(t *testing.T, events []model.RunProgressEvent, id model.ReviewID) {
	t.Helper()
	var kinds []model.RunProgressKind
	var finished model.RunProgressEvent
	for _, event := range events {
		if event.ReviewID != id {
			continue
		}
		kinds = append(kinds, event.Kind)
		finished = event
	}
	if len(kinds) != 2 || kinds[0] != model.RunProgressPending || kinds[1] != model.RunProgressFinished {
		t.Fatalf("unstarted member progress = %v, want pending then finished", kinds)
	}
	if finished.Lifecycle != model.LifecycleIncomplete || finished.Category != model.TerminationCancelled {
		t.Fatalf("unstarted member finish = %#v, want incomplete cancelled", finished)
	}
}

func containsLifecycle(values []model.Lifecycle, want model.Lifecycle) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
