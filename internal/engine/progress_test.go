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

func TestRunEmitsLiveProgressEvents(t *testing.T) {
	repository := changedTestRepository(t)
	recorder := &runProgressRecorder{}
	conductor := progressTestConductor(t, repository, 1, recorder)

	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	events := recorder.collected()
	if len(events) != 4 {
		t.Fatalf("events = %#v, want 4 (2 members × start+finish)", events)
	}
	total := len(bundle.Members)
	for index, event := range events {
		if event.Total != total {
			t.Fatalf("event %d total = %d, want %d", index, event.Total, total)
		}
	}
	for _, member := range bundle.Members {
		started, finished := progressEventsForMember(events, member)
		if started == nil || finished == nil {
			t.Fatalf("member %s:%s missing start or finish in %#v", member.Scope, member.Profile, events)
		}
		if started.Kind != model.RunProgressStarted || finished.Kind != model.RunProgressFinished {
			t.Fatalf("member %s:%s kinds = %s/%s", member.Scope, member.Profile, started.Kind, finished.Kind)
		}
		if finished.ReviewID != member.ReviewID || finished.Lifecycle != member.Lifecycle {
			t.Fatalf("finish = %#v, want review %q lifecycle %q", finished, member.ReviewID, member.Lifecycle)
		}
		if finished.Status != string(model.ResultClean) || finished.FindingCount != 0 {
			t.Fatalf("finish = %#v, want clean completion", finished)
		}
	}
}

func TestRunEmitsProgressForConcurrentMembers(t *testing.T) {
	repository := changedTestRepository(t)
	recorder := &runProgressRecorder{}
	conductor := progressTestConductor(t, repository, 2, recorder)

	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	events := recorder.collected()
	if len(events) != len(bundle.Members)*2 {
		t.Fatalf("events = %d, want %d (start+finish per member)", len(events), len(bundle.Members)*2)
	}
	for _, member := range bundle.Members {
		started, finished := progressEventsForMember(events, member)
		if started == nil || finished == nil {
			t.Fatalf("concurrent member %s:%s missing start or finish in %#v", member.Scope, member.Profile, events)
		}
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
	if len(events) != 2 {
		t.Fatalf("events = %#v, want start+finish for one unavailable member", events)
	}
	finished := events[1]
	if finished.Kind != model.RunProgressFinished || finished.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("finish event = %#v, want incomplete lifecycle", finished)
	}
	if finished.Message == "" {
		t.Fatalf("finish event = %#v, want the availability diagnostic", finished)
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
	if len(events) != 2 {
		t.Fatalf("events = %#v, want start+finish for the explicit Profile", events)
	}
	if events[0].Kind != model.RunProgressStarted || events[1].Kind != model.RunProgressFinished {
		t.Fatalf("events = %#v, want started then finished", events)
	}
	if events[1].ReviewID != record.ID {
		t.Fatalf("finish review = %q, want %q", events[1].ReviewID, record.ID)
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
	if len(events) != len(bundle.Members)*2 {
		t.Fatalf("events = %d, want %d (start+finish per member)", len(events), len(bundle.Members)*2)
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

func progressEventsForMember(events []model.RunProgressEvent, member model.BundleMember) (*model.RunProgressEvent, *model.RunProgressEvent) {
	var started, finished *model.RunProgressEvent
	for index := range events {
		event := &events[index]
		if event.Profile != member.Profile || event.Scope != member.Scope {
			continue
		}
		switch event.Kind {
		case model.RunProgressStarted:
			started = event
		case model.RunProgressFinished:
			finished = event
		}
	}
	return started, finished
}

// compile-time guard: the recorder satisfies the sink signature used by New.
var _ func(model.RunProgressEvent) = (*runProgressRecorder)(nil).record

// ensure time import stays meaningful for future timing assertions in this file.
var _ = time.Now
