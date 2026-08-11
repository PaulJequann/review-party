package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/store"
)

func TestReplayUsesFrozenProfileAndCreatesIndependentLineage(t *testing.T) {
	repository, base, head := committedReviewFixture(t)
	executor := successfulExecutor(cleanReview)
	conductor := testConductor(t, executor, time.Second)
	original, err := conductor.Review(context.Background(), ReviewSelection{Repository: repository, Subject: CommittedRange(base, head), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	installChangedBugsProfile(t, repository)
	replay, err := conductor.Replay(context.Background(), ReplaySelection{SourceReviewID: original.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertFrozenReplay(t, original, replay)
	if executor.attemptCount() != 2 {
		t.Fatalf("attempts = %d", executor.attemptCount())
	}
	page, err := conductor.History(context.Background(), store.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertReplayLineage(t, page.Entries[0].ReplaysReviewID, original.ID)
}

func assertFrozenReplay(t *testing.T, original, replay ReviewRecord) {
	t.Helper()
	if !reflect.DeepEqual(replay.ProfileRevision, original.ProfileRevision) {
		t.Fatalf("replay revision changed\noriginal: %#v\nreplay: %#v", original.ProfileRevision, replay.ProfileRevision)
	}
	if !reflect.DeepEqual(replay.ProfileSnapshot, original.ProfileSnapshot) {
		t.Fatal("replay snapshot changed")
	}
	if replay.ID == original.ID {
		t.Fatal("replay reused source Review ID")
	}
	assertReplayLineage(t, replay.ReplaysReviewID, original.ID)
	if replay.Subject.Identity != original.Subject.Identity {
		t.Fatalf("Subject identity = %s", replay.Subject.Identity)
	}
}

func assertReplayLineage(t *testing.T, source *ReviewID, expected ReviewID) {
	t.Helper()
	if source == nil {
		t.Fatal("replay lineage is nil")
	}
	if *source != expected {
		t.Fatalf("lineage = %s, want %s", *source, expected)
	}
}

func TestReplayRecordsExplicitReviewerOverride(t *testing.T) {
	repository, base, head := committedReviewFixture(t)
	grok := successfulExecutor(cleanReview)
	opencode := successfulExecutor(cleanReview)
	conductor := testConductorWithExecutors(t, map[string]attemptExecutor{"grok": grok, "opencode": opencode}, time.Second)
	original, err := conductor.Review(context.Background(), ReviewSelection{Repository: repository, Subject: CommittedRange(base, head), Profile: "bugs", Reviewer: "grok"})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := conductor.Replay(context.Background(), ReplaySelection{SourceReviewID: original.ID, Reviewer: "opencode", Model: "meta/muse-spark-1.2-contributor", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if replay.ProfileRevision.ReviewerID != "opencode" {
		t.Fatalf("reviewer = %s", replay.ProfileRevision.ReviewerID)
	}
	if replay.ProfileRevision.Model != "meta/muse-spark-1.2-contributor" {
		t.Fatalf("model = %s", replay.ProfileRevision.Model)
	}
	if replay.ProfileRevision.Effort != "high" {
		t.Fatalf("effort = %s", replay.ProfileRevision.Effort)
	}
	if opencode.attemptCount() != 1 {
		t.Fatalf("opencode attempts = %d", opencode.attemptCount())
	}
}

func TestReplayOriginalReviewerUnavailabilityRemainsIncomplete(t *testing.T) {
	repository, base, head := committedReviewFixture(t)
	executor := successfulExecutor(cleanReview)
	conductor := testConductor(t, executor, time.Second)
	original, err := conductor.Review(context.Background(), ReviewSelection{Repository: repository, Subject: CommittedRange(base, head), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	executor.availability = availability{Available: false, Diagnostic: "recorded Reviewer unavailable"}
	replay, err := conductor.Replay(context.Background(), ReplaySelection{SourceReviewID: original.ID})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Lifecycle != LifecycleIncomplete || replay.ProfileRevision.ReviewerID != original.ProfileRevision.ReviewerID {
		t.Fatalf("replay = %#v", replay)
	}
	if executor.attemptCount() != 1 {
		t.Fatalf("attempts = %d, want only original", executor.attemptCount())
	}
}

func TestReplayRejectsWorkingChangesBeforeLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testConductor(t, executor, time.Second)
	original, err := conductor.Review(context.Background(), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	_, err = conductor.Replay(context.Background(), ReplaySelection{SourceReviewID: original.ID})
	if !errors.Is(err, ErrWorkingChangesReplayUnsupported) {
		t.Fatalf("error = %v", err)
	}
	if executor.attemptCount() != 1 {
		t.Fatalf("attempts = %d", executor.attemptCount())
	}
}

func TestReplayRejectsMissingCommitBeforeLaunch(t *testing.T) {
	repository, base, head := committedReviewFixture(t)
	executor := successfulExecutor(cleanReview)
	conductor := testConductor(t, executor, time.Second)
	original, err := conductor.Review(context.Background(), ReviewSelection{Repository: repository, Subject: CommittedRange(base, head), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	original.Subject.HeadObject = strings.Repeat("0", 40)
	if err := conductor.store.Save(original); err != nil {
		t.Fatal(err)
	}
	_, err = conductor.Replay(context.Background(), ReplaySelection{SourceReviewID: original.ID})
	if err == nil {
		t.Fatal("expected missing commit error")
	}
	if !strings.Contains(err.Error(), "reconstruct replay Subject") {
		t.Fatalf("error = %v", err)
	}
	if executor.attemptCount() != 1 {
		t.Fatalf("attempts = %d", executor.attemptCount())
	}
}

func installChangedBugsProfile(t *testing.T, repository string) {
	t.Helper()
	payload, err := packagedProfileFiles.ReadFile("profiles/bugs.md")
	if err != nil {
		t.Fatal(err)
	}
	payload = []byte(strings.Replace(string(payload), "Find material defects", "CHANGED PROFILE MUST NOT BE REPLAYED", 1))
	directory := filepath.Join(repository, ".reviewparty", "profiles")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bugs.md"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}
