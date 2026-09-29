package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

// runProgressRecorder collects progress events with a count of concurrent
// emissions to prove the sink receives every event.
type runProgressRecorder struct {
	mutex  sync.Mutex
	events []model.RunProgressEvent
}

func (recorder *runProgressRecorder) record(event model.RunProgressEvent) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.events = append(recorder.events, event)
}

func (recorder *runProgressRecorder) collected() []model.RunProgressEvent {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return append([]model.RunProgressEvent(nil), recorder.events...)
}

func progressTestConductor(t *testing.T, repository string, limit int, recorder *runProgressRecorder) *Conductor {
	t.Helper()
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(cleanReview)})
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: limit,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}, {Profile: "code-quality"}},
		Repository:       []configuration.SelectionItem{},
	})
	conductor.progress = recorder.record
	return conductor
}

func sleepingProgressConductor(t *testing.T, repository string, limit int, recorder *runProgressRecorder) *Conductor {
	t.Helper()
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(ctx context.Context, _ attemptSpec) attemptExecution {
			select {
			case <-ctx.Done():
				return contextExecution(ctx.Err())
			case <-time.After(10 * time.Millisecond):
				return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
			}
		},
	}
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: limit,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}, {Profile: "code-quality"}},
		Repository:       []configuration.SelectionItem{},
	})
	conductor.progress = recorder.record
	return conductor
}

func TestRunReportsEveryMemberTransition(t *testing.T) {
	for _, limit := range []int{1, 2} {
		repository := changedTestRepository(t)
		recorder := &runProgressRecorder{}
		conductor := sleepingProgressConductor(t, repository, limit, recorder)

		bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

		events := recorder.collected()
		requireBundleOutcome(t, bundle, 2, model.LifecycleCompleted)
		requirePendingBeforeAnyStart(t, events, bundle)
		for _, member := range bundle.Members {
			requireMemberTransitions(t, events, member)
		}
	}
}

// requirePendingBeforeAnyStart proves a caller learns every member ID, in
// member order, before the first member starts.
func requirePendingBeforeAnyStart(t *testing.T, events []model.RunProgressEvent, bundle model.ReviewBundle) {
	t.Helper()
	pending := 0
	for index, event := range events {
		if event.BundleID != bundle.ID || event.ReviewID == "" || event.Total != len(bundle.Members) {
			t.Fatalf("event %d = %#v, want bundle %q, a Review ID, and total %d", index, event, bundle.ID, len(bundle.Members))
		}
		if event.Kind != model.RunProgressPending {
			continue
		}
		if index != pending || event.Index != pending || event.ReviewID != bundle.Members[pending].ReviewID {
			t.Fatalf("event %d = %#v, want pending member %d before any other event", index, event, pending)
		}
		pending++
	}
	if pending != len(bundle.Members) {
		t.Fatalf("pending events = %d, want %d", pending, len(bundle.Members))
	}
}

func requireMemberTransitions(t *testing.T, events []model.RunProgressEvent, member model.BundleMember) {
	t.Helper()
	var observed []model.RunProgressEvent
	for _, event := range events {
		if event.ReviewID == member.ReviewID {
			observed = append(observed, event)
		}
	}
	want := []model.RunProgressKind{model.RunProgressPending, model.RunProgressStarted, model.RunProgressAttempt, model.RunProgressFinished}
	if len(observed) != len(want) {
		t.Fatalf("member %s events = %#v, want kinds %v", member.ReviewID, observed, want)
	}
	for index, event := range observed {
		if event.Kind != want[index] {
			t.Fatalf("member %s event %d = %s, want %s", member.ReviewID, index, event.Kind, want[index])
		}
	}
	if observed[2].Attempt != 1 {
		t.Fatalf("attempt event = %#v, want attempt 1", observed[2])
	}
	finished := observed[3]
	if finished.Lifecycle != member.Lifecycle || finished.Status != string(model.ResultClean) || finished.Category != "" {
		t.Fatalf("finish = %#v, want clean %s completion", finished, member.Lifecycle)
	}
}

func TestRunWithoutProgressSinkStaysSilent(t *testing.T) {
	repository := changedTestRepository(t)
	recorder := &runProgressRecorder{}
	conductor := progressTestConductor(t, repository, 1, recorder)
	conductor.progress = nil

	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	if events := recorder.collected(); len(events) != 0 {
		t.Fatalf("events = %#v, want none without a sink", events)
	}
	requireBundleOutcome(t, bundle, 2, model.LifecycleCompleted)
}

func TestRunProgressReportsIncompleteMembers(t *testing.T) {
	repository := changedTestRepository(t)
	recorder := &runProgressRecorder{}
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: &scriptedExecutor{
		availability: availability{Available: false, Diagnostic: "reviewer is offline"},
	}})
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}},
		Repository:       []configuration.SelectionItem{},
	})
	conductor.progress = recorder.record

	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	events := recorder.collected()
	if len(events) != 3 {
		t.Fatalf("events = %#v, want pending, started, and finished for one unavailable member", events)
	}
	finished := events[2]
	if finished.Kind != model.RunProgressFinished || finished.Lifecycle != model.LifecycleIncomplete || finished.Category != model.TerminationReviewerUnavailable {
		t.Fatalf("finish event = %#v, want an incomplete reviewer_unavailable lifecycle", finished)
	}
	if finished.Message != "reviewer is offline" {
		t.Fatalf("finish message = %q, want the availability diagnostic", finished.Message)
	}
	if bundle.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("bundle lifecycle = %q, want incomplete", bundle.Lifecycle)
	}
}

func TestReviewExplicitProfileEmitsProgressEvents(t *testing.T) {
	repository := changedTestRepository(t)
	recorder := &runProgressRecorder{}
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(cleanReview)})
	conductor.progress = recorder.record

	selection := model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Profile: "repository:local-docs"}
	writeExecutableProfile(t, repository, "local-docs")
	record, err := conductor.ReviewExplicitProfile(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}

	events := recorder.collected()
	requireMemberTransitions(t, events, model.BundleMember{ReviewID: record.ID, Lifecycle: record.Lifecycle})
	if len(events) != 4 || events[0].BundleID != "" {
		t.Fatalf("events = %#v, want four transitions outside any bundle", events)
	}
}

// TestRunProgressStartedWaitsForConcurrencyGate pins the gate contract: with
// three members at concurrency limit two, no third running line may appear
// while two members execute, and every start pairs with a finish — so a
// running line never includes launch-queue time.
func TestRunProgressStartedWaitsForConcurrencyGate(t *testing.T) {
	repository := changedTestRepository(t)
	recorder := &runProgressRecorder{}
	const limit = 2
	executor := &scriptedExecutor{
		availability: availability{Available: true},
		execute: func(ctx context.Context, spec attemptSpec) attemptExecution {
			select {
			case <-ctx.Done():
				return contextExecution(ctx.Err())
			case <-time.After(20 * time.Millisecond):
				return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
			}
		},
	}
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeRepositorySelection(t, repository, configuration.ReviewSelection{
		ConcurrencyLimit: limit,
		Global:           []configuration.SelectionItem{{Profile: "bugs"}, {Profile: "code-quality"}, {Profile: "documentation"}},
		Repository:       []configuration.SelectionItem{},
	})
	conductor.progress = recorder.record

	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	events := recorder.collected()
	if len(events) != len(bundle.Members)*4 {
		t.Fatalf("events = %d, want %d (four transitions per member)", len(events), len(bundle.Members)*4)
	}
	running := 0
	for index, event := range events {
		switch event.Kind {
		case model.RunProgressStarted:
			running++
			if running > limit {
				t.Fatalf("event %d reports %d running members with concurrency limit %d: %#v", index, running, limit, event)
			}
		case model.RunProgressFinished:
			running--
		}
	}
	if running != 0 {
		t.Fatalf("events leave %d members running without a finish", running)
	}
}

// compile-time guard: the recorder satisfies the sink signature used by New.
var _ func(model.RunProgressEvent) = (*runProgressRecorder)(nil).record

// ensure time import stays meaningful for future timing assertions in this file.
var _ = time.Now
