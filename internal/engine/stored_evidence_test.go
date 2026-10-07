package engine

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestReviewKeepsThePatchOutOfTheLedger(t *testing.T) {
	repository := sentinelRepository(t)
	executor := successfulExecutor(cleanReview)
	conductor, state := hostStateConductor(t, executor)

	if _, err := conductor.Review(testContext(t), testSelection(repository)); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(promptPatch(t, executor.attempts[0].Prompt), patchSentinel) {
		t.Fatal("the reviewer prompt lost the patch")
	}
	for _, path := range filesHolding(t, state, patchSentinel) {
		if store.IsLedgerFile(filepath.Base(path)) {
			t.Fatalf("ledger file %s holds the patch", path)
		}
	}
}
