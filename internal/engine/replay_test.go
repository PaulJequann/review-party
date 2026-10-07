package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"reviewparty/internal/model"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/store"
)

func TestReplayUsesFrozenProfileAndCreatesIndependentLineage(t *testing.T) {
	repository, base, head := committedReviewFixture(t)
	executor := successfulExecutor(cleanReview)
	conductor := testConductor(t, executor, time.Second)
	original, err := conductor.Review(testContext(t), model.RunSelection{Repository: repository, Subject: model.CommittedRange(base, head), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	installChangedBugsProfile(t, repository)
	replay, err := conductor.Replay(testContext(t), model.ReplaySelection{SourceReviewID: original.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertFrozenReplay(t, original, replay)
	if executor.attemptCount() != 2 {
		t.Fatalf("attempts = %d", executor.attemptCount())
	}
	if originalPatch, replayPatch := promptPatch(t, executor.attempts[0].Prompt), promptPatch(t, executor.attempts[1].Prompt); originalPatch == "" || replayPatch != originalPatch {
		t.Fatalf("replay reviewed patch %q, want the original %q regenerated from the repository", replayPatch, originalPatch)
	}
	page, err := conductor.History(context.Background(), store.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertReplayLineage(t, page.Entries[0].ReplaysReviewID, original.ID)
}

func assertFrozenReplay(t *testing.T, original, replay model.ReviewRecord) {
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

func assertReplayLineage(t *testing.T, source *model.ReviewID, expected model.ReviewID) {
	t.Helper()
	if source == nil {
		t.Fatal("replay lineage is nil")
	}
	if *source != expected {
		t.Fatalf("lineage = %s, want %s", *source, expected)
	}
}

func TestReplayRejectsExecutionOverrides(t *testing.T) {
	repository, base, head := committedReviewFixture(t)
	conductor := testConductor(t, successfulExecutor(cleanReview), time.Second)
	original, err := conductor.Review(testContext(t), model.RunSelection{Repository: repository, Subject: model.CommittedRange(base, head), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = conductor.Replay(testContext(t), model.ReplaySelection{SourceReviewID: original.ID, Reviewer: "opencode"})
	if err == nil || !strings.Contains(err.Error(), "does not accept") {
		t.Fatalf("error = %v", err)
	}
}

func TestReplayRejectsWorkingChangesBeforeLaunch(t *testing.T) {
	repository := changedTestRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor := testConductor(t, executor, time.Second)
	original, err := conductor.Review(testContext(t), testSelection(repository))
	if err != nil {
		t.Fatal(err)
	}
	_, err = conductor.Replay(testContext(t), model.ReplaySelection{SourceReviewID: original.ID})
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
	original, err := conductor.Review(testContext(t), model.RunSelection{Repository: repository, Subject: model.CommittedRange(base, head), Profile: "bugs"})
	if err != nil {
		t.Fatal(err)
	}
	original.Subject.HeadObject = strings.Repeat("0", 40)
	if err := conductor.store.Save(original); err != nil {
		t.Fatal(err)
	}
	_, err = conductor.Replay(testContext(t), model.ReplaySelection{SourceReviewID: original.ID})
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
	writeExecutableProfile(t, repository, "bugs")
	directory := filepath.Join(repository, ".reviewparty", "profiles", "bugs")
	if err := os.WriteFile(filepath.Join(directory, "instructions.md"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}
