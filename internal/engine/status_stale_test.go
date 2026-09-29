package engine

import (
	"context"
	"testing"
	"time"

	"reviewparty/internal/model"
)

// staleProbe reads a run's status at a clock set just inside and just past
// the stale limit, while the run it observes is still live in the ledger.
type staleProbe struct {
	conductor *Conductor
	limit     time.Duration
}

func (probe staleProbe) statusAt(t *testing.T, ctx context.Context, id string, offset time.Duration) model.ReviewStatus {
	t.Helper()
	realNow := probe.conductor.now
	at := realNow().Add(offset)
	probe.conductor.now = func() time.Time { return at }
	defer func() { probe.conductor.now = realNow }()
	status, err := probe.conductor.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func (probe staleProbe) onlyInFlight(t *testing.T, ctx context.Context, repository string) model.ReviewStatus {
	t.Helper()
	in, err := probe.conductor.InFlight(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 1 {
		t.Fatalf("in flight = %#v, want exactly the live run", in)
	}
	return in[0]
}

func (probe staleProbe) requireStaleOnlyPastLimit(t *testing.T, ctx context.Context, id string) {
	t.Helper()
	if inside := probe.statusAt(t, ctx, id, probe.limit-5*time.Second); inside.Stale != nil {
		t.Errorf("status %s inside the limit = %#v, want no stale signal", id, inside.Stale)
	}
	past := probe.statusAt(t, ctx, id, probe.limit+5*time.Second).Stale
	if past == nil {
		t.Fatalf("status %s past the limit has no stale signal, want one over %s", id, probe.limit)
	}
	if past.LimitMS != probe.limit.Milliseconds() || past.QuietMS <= past.LimitMS {
		t.Errorf("status %s past the limit = %#v, want a stale signal over %s", id, past, probe.limit)
	}
}

func TestStatusMarksALiveRunStaleOnlyAfterItsDeadlinePlusSlack(t *testing.T) {
	repository := changedTestRepository(t)
	var probe staleProbe
	var bundleID model.ReviewBundleID
	executor := &scriptedExecutor{availability: availability{Available: true}}
	executor.execute = func(ctx context.Context, _ attemptSpec) attemptExecution {
		if bundleID == "" {
			live := probe.onlyInFlight(t, ctx, repository)
			bundleID = model.ReviewBundleID(live.ID)
			probe.requireStaleOnlyPastLimit(t, ctx, live.ID)
			for _, member := range live.Reviews {
				probe.requireStaleOnlyPastLimit(t, ctx, string(member.ReviewID))
			}
		}
		return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
	}
	probe = staleProbe{conductor: testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor}), limit: time.Second + staleSlack}
	twoMemberSequentialSelection(t, repository)

	runRun(t, probe.conductor, model.RunSelection{Repository: repository, Subject: model.WorkingChanges()})

	if finished := probe.statusAt(t, context.Background(), string(bundleID), 24*time.Hour); finished.Lifecycle != model.LifecycleCompleted || finished.Stale != nil {
		t.Fatalf("finished status a day later = %#v, want a completed run that is never stale", finished)
	}
}

func TestStatusMarksALiveStandaloneReviewStale(t *testing.T) {
	repository := changedTestRepository(t)
	var probe staleProbe
	executor := &scriptedExecutor{availability: availability{Available: true}}
	executor.execute = func(ctx context.Context, _ attemptSpec) attemptExecution {
		live := probe.onlyInFlight(t, ctx, repository)
		if live.Kind != model.ReviewStatusReview {
			t.Fatalf("in flight = %#v, want the running Review", live)
		}
		probe.requireStaleOnlyPastLimit(t, ctx, live.ID)
		return attemptExecution{AssistantText: cleanReview, Outcome: model.AttemptCompleted}
	}
	// writeExecutableProfile snapshots a 1m execution deadline.
	probe = staleProbe{conductor: testPartyConductor(t, map[string]attemptExecutor{defaultReviewer: executor}), limit: time.Minute + staleSlack}
	writeExecutableProfile(t, repository, "local-docs")

	if _, err := probe.conductor.ReviewExplicitProfile(context.Background(), model.RunSelection{Repository: repository, Subject: model.WorkingChanges(), Profile: "repository:local-docs"}); err != nil {
		t.Fatal(err)
	}
}
