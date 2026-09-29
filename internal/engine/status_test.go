package engine

import (
	"context"
	"errors"
	"testing"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

func TestStatusFollowsABundleFromInFlightToCompleted(t *testing.T) {
	repository := changedTestRepository(t)
	root, err := subject.ResolveRepositoryRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	var conductor *Conductor
	var during []model.ReviewStatus
	executor := &scriptedExecutor{availability: availability{Available: true}}
	executor.execute = func(ctx context.Context, _ attemptSpec) attemptExecution {
		if during == nil {
			var inFlightErr error
			if during, inFlightErr = conductor.InFlight(ctx, root); inFlightErr != nil {
				t.Error(inFlightErr)
			}
		}
		return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
	}
	conductor = testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	twoMemberSequentialSelection(t, repository)

	bundle := runRun(t, conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	if len(during) != 1 || during[0].ID != string(bundle.ID) || during[0].Kind != model.ReviewStatusBundle || during[0].Lifecycle != model.LifecycleRunning {
		t.Fatalf("in flight during the first attempt = %#v, want only the running bundle %s", during, bundle.ID)
	}
	requireStatusMemberLifecycles(t, during[0], model.LifecycleRunning, model.LifecyclePending)
	finished, err := conductor.Status(context.Background(), string(bundle.ID))
	if err != nil {
		t.Fatal(err)
	}
	if finished.Lifecycle != model.LifecycleCompleted || finished.Repository != root {
		t.Fatalf("finished status = %#v, want the completed bundle for %s", finished, root)
	}
	requireStatusMemberLifecycles(t, finished, model.LifecycleCompleted, model.LifecycleCompleted)
	for index, member := range finished.Reviews {
		if member.ReviewID != bundle.Members[index].ReviewID || member.Scope != "global" || member.Reviewer != defaultReviewer ||
			member.Attempts != 1 || member.Status != string(model.ResultClean) || member.UpdatedAt.IsZero() {
			t.Fatalf("member %d = %#v, want the completed clean Review %s", index, member, bundle.Members[index].ReviewID)
		}
	}
	after, err := conductor.InFlight(context.Background(), root)
	if err != nil || after == nil || len(after) != 0 {
		t.Fatalf("in flight after the run = %#v, %v; want an empty list", after, err)
	}
}

func TestStatusListsAStandaloneReviewInFlight(t *testing.T) {
	repository := changedTestRepository(t)
	root, err := subject.ResolveRepositoryRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	var conductor *Conductor
	var during []model.ReviewStatus
	executor := &scriptedExecutor{availability: availability{Available: true}}
	executor.execute = func(ctx context.Context, _ attemptSpec) attemptExecution {
		var inFlightErr error
		if during, inFlightErr = conductor.InFlight(ctx, root); inFlightErr != nil {
			t.Error(inFlightErr)
		}
		return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
	}
	conductor = testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor})
	writeExecutableProfile(t, repository, "local-docs")

	record, err := conductor.ReviewExplicitProfile(context.Background(), model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Profile: "repository:local-docs"})
	if err != nil {
		t.Fatal(err)
	}

	if len(during) != 1 || during[0].ID != string(record.ID) || during[0].Kind != model.ReviewStatusReview {
		t.Fatalf("in flight = %#v, want the running Review %s", during, record.ID)
	}
	requireStatusMemberLifecycles(t, during[0], model.LifecycleRunning)
	if during[0].Reviews[0].Profile != "local-docs" {
		t.Fatalf("status member = %#v, want Profile local-docs", during[0].Reviews[0])
	}
}

func TestStatusRejectsUnknownAndUnsupportedIDs(t *testing.T) {
	conductor := testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: successfulExecutor(cleanReview)})
	if _, err := conductor.Status(context.Background(), "ev_1725192000000_0123456789abcdef"); !errors.Is(err, ErrUnsupportedStatusID) {
		t.Fatalf("unsupported prefix error = %v, want ErrUnsupportedStatusID", err)
	}
	for _, id := range []string{"rb_1725192000000_0123456789abcdef", "rp_1725192000000_0123456789abcdef"} {
		if _, err := conductor.Status(context.Background(), id); !errors.Is(err, store.ErrReviewNotFound) {
			t.Fatalf("status %s error = %v, want ErrReviewNotFound", id, err)
		}
	}
}

func requireStatusMemberLifecycles(t *testing.T, status model.ReviewStatus, want ...model.Lifecycle) {
	t.Helper()
	if len(status.Reviews) != len(want) {
		t.Fatalf("status reviews = %#v, want %d", status.Reviews, len(want))
	}
	for index, member := range status.Reviews {
		if member.Lifecycle != want[index] || member.ReviewID == "" {
			t.Fatalf("member %d = %#v, want lifecycle %s with a Review ID", index, member, want[index])
		}
	}
}
