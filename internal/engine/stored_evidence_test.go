package engine

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/artifact"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

const patchSentinel = "PATCH-SENTINEL-4f1c"

// hostStateConductor wires the ledger and the artifact store into one state
// directory, as New does, so a test can inspect everything a Review leaves.
func hostStateConductor(t *testing.T, executor attemptExecutor) (*Conductor, string) {
	t.Helper()
	state := t.TempDir()
	ledger, err := store.NewLedgerRecordStore(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	manager := newTestConfigurationManagerWithDeadline(t, time.Second)
	conductor, err := newConductorWithManager(ledger, catalogWithExecutors(map[string]attemptExecutor{defaultReviewer: executor}), manager, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conductor.artifacts = mustNewArtifactStore(t, state)
	conductor.getRunner().publisher = newArtifactPublisher(conductor.artifacts)
	return conductor, state
}

func sentinelRepository(t *testing.T) string {
	t.Helper()
	repository := testRepository(t)
	writeTestFile(t, filepath.Join(repository, "review.go"), "package demo\n\nconst state = \""+patchSentinel+"\"\n")
	return repository
}

// filesHolding lists the files under root whose bytes contain needle.
func filesHolding(t *testing.T, root, needle string) []string {
	t.Helper()
	var holding []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(contents, []byte(needle)) {
			holding = append(holding, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return holding
}

// The patch and the prompt that carries it are rebuilt from the repository on
// demand, so neither the ledger nor the artifact store may hold them.
func TestReviewLeavesNoPatchInHostState(t *testing.T) {
	repository := sentinelRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor, state := hostStateConductor(t, executor)

	if _, err := conductor.Review(testContext(t), testSelection(repository)); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(promptPatch(t, executor.attempts[0].Prompt), patchSentinel) {
		t.Fatal("the reviewer prompt lost the patch")
	}
	if holding := filesHolding(t, state, patchSentinel); len(holding) != 0 {
		t.Fatalf("host state holds the patch: %v", holding)
	}
}

func storedAttempt(t *testing.T, conductor *Conductor, id model.ReviewID) model.AttemptRecord {
	t.Helper()
	record, err := conductor.store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return record.Passes[0].Attempts[0]
}

func TestCompletedReviewKeepsNoRawOutput(t *testing.T) {
	executor := &scriptedExecutor{availability: availability{Available: true}, execute: func(context.Context, attemptSpec) attemptExecution {
		return attemptExecution{AssistantText: cleanReview, ReviewerNoise: codexWebsocketNoise, Outcome: model.AttemptCompleted}
	}}
	conductor, state := hostStateConductor(t, executor)

	record, err := conductor.Review(testContext(t), testSelection(changedTestRepository(t)))
	if err != nil {
		t.Fatal(err)
	}

	if record.Lifecycle != model.LifecycleCompleted {
		t.Fatalf("lifecycle = %s, want completed", record.Lifecycle)
	}
	if artifacts := storedAttempt(t, conductor, record.ID).Artifacts; len(artifacts) != 0 {
		t.Fatalf("completed attempt kept evidence %v", artifacts)
	}
	if holding := filesHolding(t, state, "405 Method Not Allowed"); len(holding) != 0 {
		t.Fatalf("host state holds the reviewer noise: %v", holding)
	}
}

func TestIncompleteReviewKeepsItsRawOutput(t *testing.T) {
	conductor, _ := hostStateConductor(t, successfulExecutor("not a result contract"))

	record, err := conductor.Review(testContext(t), testSelection(changedTestRepository(t)))
	if err != nil {
		t.Fatal(err)
	}

	if record.Lifecycle != model.LifecycleIncomplete {
		t.Fatalf("lifecycle = %s, want incomplete", record.Lifecycle)
	}
	artifacts := storedAttempt(t, conductor, record.ID).Artifacts
	if len(artifacts) != 1 || artifacts[0].Kind != artifact.AssistantText {
		t.Fatalf("incomplete attempt evidence = %v, want its assistant text", artifacts)
	}
	contents, err := conductor.artifacts.Read(artifacts[0])
	if err != nil || string(contents) != "not a result contract" {
		t.Fatalf("assistant text = %q, %v", contents, err)
	}
}

// steppingClock moves a minute per reading so every attempt completes at a
// distinct, ordered time.
func steppingClock() func() time.Time {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		current = current.Add(time.Minute)
		return current
	}
}

func TestFailureEvidenceKeepsOnlyTheNewestAttempts(t *testing.T) {
	conductor, state := hostStateConductor(t, successfulExecutor("not a result contract"))
	conductor.now = steppingClock()
	repository := changedTestRepository(t)
	var reviews []model.ReviewID
	for range retainedFailureEvidence + 2 {
		record, err := conductor.Review(testContext(t), testSelection(repository))
		if err != nil {
			t.Fatal(err)
		}
		reviews = append(reviews, record.ID)
	}

	kept := make([]bool, 0, len(reviews))
	for _, id := range reviews {
		kept = append(kept, len(storedAttempt(t, conductor, id).Artifacts) > 0)
	}
	if want := append([]bool{false, false}, slices.Repeat([]bool{true}, retainedFailureEvidence)...); !slices.Equal(kept, want) {
		t.Fatalf("kept evidence by age = %v, want %v", kept, want)
	}
	entries, err := os.ReadDir(filepath.Join(state, artifact.Directory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != retainedFailureEvidence {
		t.Fatalf("artifact directories = %d, want %d", len(entries), retainedFailureEvidence)
	}
}
