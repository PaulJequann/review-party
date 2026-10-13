package store

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"reviewparty/internal/model"
)

const (
	concurrentWriterDirectoryEnv = "REVIEW_PARTY_TEST_CONCURRENT_WRITER_DIRECTORY"
	concurrentWriterIndexEnv     = "REVIEW_PARTY_TEST_CONCURRENT_WRITER_INDEX"
	concurrentWriterProcesses    = 6
	concurrentWriterReviews      = 12
)

// TestConcurrentProcessesWriteTheLedgerWithoutLockErrors runs several
// operating-system processes against one ledger, the way parallel agents each
// launch their own review-party, and requires every write to land.
func TestConcurrentProcessesWriteTheLedgerWithoutLockErrors(t *testing.T) {
	directory := t.TempDir()
	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	failures := make(chan string, concurrentWriterProcesses)
	for index := range concurrentWriterProcesses {
		wait.Add(1)
		go func() {
			defer wait.Done()
			command := exec.Command(os.Args[0], "-test.run=^TestConcurrentLedgerWriterProcess$")
			command.Env = append(os.Environ(), concurrentWriterDirectoryEnv+"="+directory, concurrentWriterIndexEnv+"="+strconv.Itoa(index))
			if output, err := command.CombinedOutput(); err != nil {
				failures <- fmt.Sprintf("writer %d: %v\n%s", index, err, output)
			}
		}()
	}
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	requireConcurrentWrites(t, directory)
}

// TestConcurrentLedgerWriterProcess is the child body of the concurrent
// writer test; it does nothing when run directly.
func TestConcurrentLedgerWriterProcess(t *testing.T) {
	directory := os.Getenv(concurrentWriterDirectoryEnv)
	if directory == "" {
		t.Skip("runs only as a child of TestConcurrentProcessesWriteTheLedgerWithoutLockErrors")
	}
	index, err := strconv.Atoi(os.Getenv(concurrentWriterIndexEnv))
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := NewDeferredLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	for review := range concurrentWriterReviews {
		writeConcurrentReview(t, ledger, index, review)
	}
}

func writeConcurrentReview(t *testing.T, ledger *DeferredLedgerRecordStore, writer, review int) {
	t.Helper()
	record := ledgerFixture(model.LifecycleCompleted)
	record.ID = model.ReviewID(fmt.Sprintf("rp_%013d_%016x", writer, review))
	record.Lifecycle = model.LifecycleRunning
	record.Result = nil
	if err := ledger.Save(record); err != nil {
		t.Fatalf("save running review: %v", err)
	}
	completed := ledgerFixture(model.LifecycleCompleted)
	completed.ID = record.ID
	completed.Passes[0].Attempts = append(completed.Passes[0].Attempts, completed.Passes[0].Attempts[0])
	completed.Passes[0].Attempts[1].Number = 2
	completed.Passes[0].Attempts[1].Artifacts = nil
	if err := ledger.Save(completed); err != nil {
		t.Fatalf("save completed review: %v", err)
	}
	judgments := []model.Judgment{judgment(t, 1, model.VerdictAccepted, "real")}
	if _, err := ledger.RecordVerdicts(VerdictBatch{ReviewID: record.ID, Judgments: judgments, RecordedBy: "test", RecordedAt: time.Now()}); err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	waiver := waiverFixture(model.WaiverID(fmt.Sprintf("w_%d_%d", writer, review)), model.WaiverKey{Checkpoint: "c", ContentDigest: "d"}, review)
	if err := ledger.RecordCheckpointWaiver(waiver); err != nil {
		t.Fatalf("record waiver: %v", err)
	}
}

func requireConcurrentWrites(t *testing.T, directory string) {
	t.Helper()
	ledger := newTestLedger(t, directory)
	defer closeTestResource(t, ledger.Close)
	want := concurrentWriterProcesses * concurrentWriterReviews
	for table, expected := range map[string]int{"reviews": want, "attempts": 2 * want, "finding_verdicts": want, "checkpoint_waivers": want} {
		var count int
		if err := ledger.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != expected {
			t.Errorf("%s rows = %d, want %d", table, count, expected)
		}
	}
}
