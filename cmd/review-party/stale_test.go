package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

const (
	staleBundleID        = "rb_1725192000000_00000000000000b6"
	staleRunningReviewID = "rp_1725192000000_00000000000000c8"
	stalePendingReviewID = "rp_1725192000000_00000000000000c9"
	wantStaleDiagnosis   = "no progress for 30m0s; the run may have died"
)

// writeStaleBundle records a running bundle whose process died 30 minutes ago,
// well past its members' 8m execution deadline plus slack.
func (fixture statusLedger) writeStaleBundle() {
	last := time.Now().UTC().Add(-30 * time.Minute)
	records := []model.ReviewRecord{
		fixture.record(staleRunningReviewID, model.LifecycleRunning, 0),
		fixture.record(stalePendingReviewID, model.LifecyclePending, 0),
	}
	for index := range records {
		records[index].CreatedAt, records[index].UpdatedAt = last, last
	}
	bundle := fixture.bundle(staleBundleID, model.LifecycleRunning, 0, records)
	bundle.CreatedAt, bundle.UpdatedAt = last, last
	fixture.write(func(ledger *store.LedgerRecordStore) error { return ledger.CreateReviewBundle(bundle, records) })
}

// runStale runs the CLI and requires it to fail on id's stale diagnosis.
func runStale(t *testing.T, id string, arguments ...string) string {
	t.Helper()
	exit, stdout, stderr := runCLI(arguments...)
	if want := "review-party: " + id + ": " + wantStaleDiagnosis + "\n"; exit != 1 || stderr != want {
		t.Fatalf("%v exit = %d, stderr = %q; want 1 and %q; stdout =\n%s", arguments, exit, stderr, want, stdout)
	}
	return stdout
}

// runClean runs the CLI and requires it to exit 0 with nothing on stderr.
func runClean(t *testing.T, arguments ...string) string {
	t.Helper()
	exit, stdout, stderr := runCLI(arguments...)
	if exit != 0 || stderr != "" {
		t.Fatalf("%v exit = %d, stderr = %q; want 0 and no stderr; stdout =\n%s", arguments, exit, stderr, stdout)
	}
	return stdout
}

func TestStatusMarksARunWithNoProgressPastItsDeadlineStaleAndFails(t *testing.T) {
	newStatusLedger(t).writeStaleBundle()
	stdout := runStale(t, staleBundleID, "status", staleBundleID)
	if !strings.HasPrefix(stdout, "bundle "+staleBundleID+" · running · 0/2 review(s) finished\n") {
		t.Fatalf("status header =\n%s", stdout)
	}
	if want := "stale: " + wantStaleDiagnosis + "\ninspect: review-party inspect " + staleBundleID + "\n"; !strings.HasSuffix(stdout, want) {
		t.Fatalf("status output =\n%s\nwant it to end with\n%s", stdout, want)
	}
	var status model.ReviewStatus
	if err := json.Unmarshal([]byte(runStale(t, stalePendingReviewID, "status", stalePendingReviewID, "--format", "json")), &status); err != nil {
		t.Fatal(err)
	}
	if stale := status.Stale; stale == nil || stale.LimitMS != (10*time.Minute).Milliseconds() {
		t.Fatalf("queued member stale = %#v, want its bundle's signal over a 10m limit", stale)
	}
}

// The in-flight listing marks a stale run but still exits 0: nothing retires
// a dead run, so failing here would fail every later listing too.
func TestStatusWithoutIDListsAStaleRunAndExitsZero(t *testing.T) {
	fixture := newStatusLedger(t)
	fixture.writeStaleBundle()
	if stdout := runClean(t, "status", "--repo", fixture.repository); !strings.Contains(stdout, "stale: "+wantStaleDiagnosis+"\n") {
		t.Fatalf("in-flight output =\n%s\nwant the stale line", stdout)
	}
	var report inFlightReport
	if err := json.Unmarshal([]byte(runClean(t, "status", "--repo", fixture.repository, "--format", "json")), &report); err != nil {
		t.Fatal(err)
	}
	stale := map[string]bool{}
	for _, entry := range report.InFlight {
		stale[entry.ID] = entry.Stale != nil
	}
	if !stale[staleBundleID] {
		t.Fatalf("in-flight staleness by id = %v, want %s marked stale", stale, staleBundleID)
	}
}

// A queued member of a sequential bundle waits on the members ahead of it, so
// its own old row must not read stale while its siblings keep writing.
func TestStatusJudgesAQueuedMemberThroughItsBundleProgress(t *testing.T) {
	fixture := newStatusLedger(t)
	recent, old := time.Now().UTC().Add(-3*time.Minute), time.Now().UTC().Add(-30*time.Minute)
	records := []model.ReviewRecord{
		fixture.record("rp_1725192000000_00000000000000d1", model.LifecycleCompleted, 0),
		fixture.record("rp_1725192000000_00000000000000d2", model.LifecycleRunning, 0),
		fixture.record("rp_1725192000000_00000000000000d3", model.LifecyclePending, 0),
	}
	for index := range records {
		records[index].CreatedAt, records[index].UpdatedAt = old, recent
	}
	queued := &records[2]
	queued.UpdatedAt = old
	bundle := fixture.bundle("rb_1725192000000_00000000000000d0", model.LifecycleRunning, 0, records)
	bundle.CreatedAt, bundle.UpdatedAt = old, recent
	fixture.write(func(ledger *store.LedgerRecordStore) error { return ledger.CreateReviewBundle(bundle, records) })

	var status model.ReviewStatus
	if err := json.Unmarshal([]byte(runClean(t, "status", string(queued.ID), "--format", "json")), &status); err != nil {
		t.Fatal(err)
	}
	if status.Stale != nil {
		t.Fatalf("queued member stale = %#v, want no stale signal", status.Stale)
	}
}

func TestWaitExitsNonZeroOnAStaleRunInsteadOfBlocking(t *testing.T) {
	newStatusLedger(t).writeStaleBundle()
	for _, id := range []string{staleBundleID, staleRunningReviewID, stalePendingReviewID} {
		if stdout := runStale(t, id, "wait", id, "--timeout", "5s"); stdout != "" {
			t.Fatalf("wait %s stdout = %q, want nothing", id, stdout)
		}
	}
}
