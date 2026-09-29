package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
)

// artifactFailureConductor returns a conductor whose Attempt artifacts cannot
// be published, a hard error raised after the Review is saved running.
func artifactFailureConductor(t *testing.T, recorder *runProgressRecorder) *Conductor {
	t.Helper()
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(cleanReview)})
	artifactRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifactRoot, "artifacts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	conductor.getRunner().publisher = newArtifactPublisher(mustNewArtifactStore(t, artifactRoot))
	conductor.progress = recorder.record
	return conductor
}

// requireFinishedMatchesSaved asserts the Review reported finished exactly
// once, and that the heartbeat's lifecycle and termination are the saved ones.
func requireFinishedMatchesSaved(t *testing.T, conductor *Conductor, events []model.RunProgressEvent, id model.ReviewID) model.ReviewRecord {
	t.Helper()
	stored, err := conductor.store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	var finished []model.RunProgressEvent
	for _, event := range events {
		if event.Kind == model.RunProgressFinished && event.ReviewID == id {
			finished = append(finished, event)
		}
	}
	if len(finished) != 1 {
		t.Fatalf("finished events for %s = %#v, want exactly one", id, finished)
	}
	if stored.Termination == nil {
		t.Fatalf("stored record %q has no termination", stored.Lifecycle)
	}
	want := model.RunProgressEvent{Lifecycle: stored.Lifecycle, Category: stored.Termination.Category, Message: stored.Termination.Message}
	got := model.RunProgressEvent{Lifecycle: finished[0].Lifecycle, Category: finished[0].Category, Message: finished[0].Message}
	if got != want {
		t.Fatalf("finished event = %#v, want the saved record %#v", got, want)
	}
	return stored
}

// A hard error after the explicit Review started must not leave its record
// running: status and wait would report a live run that no process owns.
func TestExplicitReviewHardErrorPersistsATerminalRecord(t *testing.T) {
	repository := changedTestRepository(t)
	writeExecutableProfile(t, repository, "local-docs")
	recorder := &runProgressRecorder{}
	conductor := artifactFailureConductor(t, recorder)

	returned, err := conductor.ReviewExplicitProfile(context.Background(), model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Profile: "repository:local-docs"})
	if err == nil {
		t.Fatal("explicit review succeeded, want the artifact publish failure")
	}
	stored := requireFinishedMatchesSaved(t, conductor, recorder.collected(), returned.ID)
	if stored.Lifecycle != model.LifecycleIncomplete || returned.Lifecycle != stored.Lifecycle {
		t.Fatalf("stored = %q, returned = %q; want both incomplete", stored.Lifecycle, returned.Lifecycle)
	}
}

// Every member of a Bundle stopped by a hard error reports one finished line,
// and it says what the ledger says.
func TestStoppedBundleHeartbeatReportsTheSavedRecords(t *testing.T) {
	repository := changedTestRepository(t)
	twoMemberSequentialSelection(t, repository)
	recorder := &runProgressRecorder{}
	conductor := artifactFailureConductor(t, recorder)

	bundle, err := conductor.Run(context.Background(), model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})
	if err == nil {
		t.Fatal("run succeeded, want the artifact publish failure")
	}
	for _, member := range bundle.Members {
		stored := requireFinishedMatchesSaved(t, conductor, recorder.collected(), member.ReviewID)
		if stored.Lifecycle != model.LifecycleIncomplete {
			t.Fatalf("member %s stored %q, want incomplete", member.ReviewID, stored.Lifecycle)
		}
	}
}

// blockCheckoutCleanup makes the checkout's owner metadata a non-empty
// directory, so removing the checkout fails after the Reviewer exits.
func blockCheckoutCleanup(t *testing.T, checkout string) {
	t.Helper()
	metadata := checkout + ".owner.json"
	if err := os.Remove(metadata); err != nil {
		t.Error(err)
	}
	if err := os.MkdirAll(filepath.Join(metadata, "held"), 0o700); err != nil {
		t.Error(err)
	}
}

// A checkout that cannot be cleaned after the Review was saved completed is an
// operational error of that process: the saved result and its findings stand.
func TestCheckoutCleanupFailureKeepsTheCompletedRecord(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	repository, base, head := committedReviewFixture(t)
	executor := &scriptedExecutor{availability: availability{Available: true}, execute: func(_ context.Context, spec attemptSpec) attemptExecution {
		blockCheckoutCleanup(t, spec.Repository)
		return attemptExecution{AssistantText: findingsReview, Outcome: model.AttemptCompleted}
	}}
	conductor := testConductor(t, executor, time.Second)

	returned, err := conductor.Review(context.Background(), model.RunSelection{Repository: repository, Subject: model.CommittedRange(base, head), Profile: "bugs"})
	if err == nil || !strings.Contains(err.Error(), "could not be cleaned") {
		t.Fatalf("review error = %v, want the checkout cleanup failure", err)
	}
	stored, loadErr := conductor.store.Load(returned.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	type verdict struct {
		lifecycle model.Lifecycle
		findings  int
	}
	for name, record := range map[string]model.ReviewRecord{"stored": stored, "returned": returned} {
		got := verdict{lifecycle: record.Lifecycle}
		if record.Result != nil {
			got.findings = record.Result.FindingCount()
		}
		if got.lifecycle != model.LifecycleCompleted || got.findings == 0 {
			t.Fatalf("%s record = %+v, want completed with its findings", name, got)
		}
	}
}
