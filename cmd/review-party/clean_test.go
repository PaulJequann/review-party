package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func assertLabels(t *testing.T, name string, items []footprintItem, want ...string) {
	t.Helper()
	if got := labels(items); !slices.Equal(got, want) {
		t.Fatalf("%s =\n%v\nwant\n%v", name, got, want)
	}
}

var history = []string{"artifacts artifacts", "backups backups", "ledger ledger.sqlite", "ledger ledger.sqlite-wal"}

func TestCleanReapsDeadRunsKeepsLiveOnesAndOnlyPreviewsTheRest(t *testing.T) {
	fixture := newHostFixture(t)
	live := fixture.liveRun()
	dead := fixture.seed()

	result := decodeInto[cleanResult](t, fixture.run("clean", "--format", "json"), 0)

	assertLabels(t, "removed", result.Removed, "orphaned-run 1-dead")
	assertLabels(t, "left behind", result.LeftBehind)
	assertLabels(t, "kept", result.Kept, "live-run "+filepath.Base(live), "unrecognized notes.txt")
	assertLabels(t, "pending", result.Pending, "artifacts artifacts", "backups backups", "cache model-discovery", "ledger ledger.sqlite", "ledger ledger.sqlite-wal",
		"legacy review-party-delta-1", "legacy review-party-worktrees", "partial-artifact .artifact-1.tmp")
	fixture.assertOnDisk(append(fixture.inTemp("review-party-worktrees"), live), []string{dead})
}

func TestCleanYesRemovesLeftoversAndCacheButNotHistory(t *testing.T) {
	fixture := newHostFixture(t)
	fixture.seed()

	run := fixture.run("clean", "--yes")
	result := decodeInto[cleanResult](t, fixture.run("clean", "--yes", "--format", "json"), 0)

	note := "note: repositories reviewed by an earlier release may still list worktrees under " + fixture.inTemp("review-party-worktrees")[0] + "; run `git worktree prune` in each to clear them\n"
	assertExitAndOutput(t, run, 0, note)
	assertLabels(t, "pending after a second pass", result.Pending, history...)
	assertLabels(t, "removed by a second pass", result.Removed)
	kept := append(fixture.inState("ledger.sqlite", "ledger.sqlite-wal", "backups", "artifacts/ab/published", "notes.txt"), fixture.inTemp("review-party-runtime-9999", "unrelated")...)
	removed := append(fixture.inState("artifacts/.artifact-1.tmp"), fixture.inTemp("review-party-delta-1", "review-party-worktrees")...)
	fixture.assertOnDisk(kept, append(removed, fixture.host.cache))
}

// assertExitAndOutput fails unless the command exited with exit and its
// stdout contains want.
func assertExitAndOutput(t *testing.T, run commandRun, exit int, want string) {
	t.Helper()
	if run.exit != exit {
		t.Fatalf("exited %d, want %d: %+v", run.exit, exit, run)
	}
	if !strings.Contains(run.stdout, want) {
		t.Fatalf("stdout lacks %q: %+v", want, run)
	}
}

func TestCleanYesWithASelectorRemovesOnlyThatHistory(t *testing.T) {
	fixture := newHostFixture(t)
	fixture.seed()

	result := decodeInto[cleanResult](t, fixture.run("clean", "--yes", "--ledger", "--format", "json"), 0)

	assertLabels(t, "pending", result.Pending, history[:2]...)
	fixture.assertOnDisk(fixture.inState("backups", "artifacts/ab/published"), fixture.inState("ledger.sqlite", "ledger.sqlite-wal"))
}

func TestCleanLeavesHistoryBehindWhileARunIsLive(t *testing.T) {
	fixture := newHostFixture(t)
	fixture.liveRun()
	fixture.seed()

	run := fixture.run("clean", "--yes", "--ledger")

	assertExitAndOutput(t, run, 1, "left behind ledger "+fixture.inState("ledger.sqlite")[0]+": a Review Party run is live\n")
	result := decodeInto[cleanResult](t, fixture.run("clean", "--yes", "--ledger", "--format", "json"), 1)
	assertLabels(t, "left behind", result.LeftBehind, "ledger ledger.sqlite", "ledger ledger.sqlite-wal", "partial-artifact .artifact-1.tmp")
	fixture.assertOnDisk(fixture.inState("ledger.sqlite", "ledger.sqlite-wal"), nil)
}

func TestCleanExitsOneAndNamesWhatItCouldNotRemove(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory whose entries this user cannot unlink")
	}
	fixture := newHostFixture(t)
	locked := filepath.Join(fixture.host.temp, "review-party-eval-1", "sealed")
	fixture.write(filepath.Join(locked, "file"), "stuck")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) }) //nolint:errcheck // Restoring the mode only lets TempDir cleanup succeed.

	result := decodeInto[cleanResult](t, fixture.run("clean", "--yes", "--format", "json"), 1)

	assertLabels(t, "left behind", result.LeftBehind, "legacy review-party-eval-1")
	if left := result.LeftBehind[0]; !strings.Contains(left.Error, "permission denied") {
		t.Fatalf("left behind = %+v, want a permission error", left)
	}
}
