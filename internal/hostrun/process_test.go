package hostrun

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type killedTree struct {
	runDir   string
	sentinel int
	pids     []int
}

func killOwner(t *testing.T, root string) killedTree {
	t.Helper()
	scratch := t.TempDir()
	pids := filepath.Join(scratch, "pids")
	ready := filepath.Join(scratch, "ready")
	owner := helperCommand("owner", rootVar+"="+root, pidsVar+"="+pids, readyVar+"="+ready)
	owner.Stderr = os.Stderr
	startChild(t, owner)
	var lines []string
	waitFor(t, 10*time.Second, "the owner to announce its tree", func() bool {
		data, err := os.ReadFile(ready)
		lines = strings.Split(strings.TrimSpace(string(data)), "\n")
		return err == nil && len(lines) == 2
	})
	sentinel, err := strconv.Atoi(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait() //nolint:errcheck // The owner was killed; its exit status is not the subject.
	return killedTree{runDir: lines[0], sentinel: sentinel, pids: readPIDs(t, pids)}
}

func TestKilledOwnerTreeDiesAndNextOpenRemovesRun(t *testing.T) {
	root := testRoot(t)
	tree := killOwner(t, root)
	killLater(t, append(slices.Clone(tree.pids), tree.sentinel))
	if len(tree.pids) < 2 {
		t.Fatalf("owner recorded %v; want the tree and at least one child", tree.pids)
	}
	everything := append(slices.Clone(tree.pids), tree.sentinel)
	waitFor(t, 2*time.Second, "the Reviewer tree and sentinel to die", func() bool {
		return len(survivors(everything)) == 0
	})
	if !fileExists(tree.runDir) {
		t.Fatalf("run directory %s vanished before any Open reaped it", tree.runDir)
	}

	var warnings []string
	next := openRun(t, root, &warnings)
	rep := next.Reap()
	if fileExists(tree.runDir) {
		t.Fatalf("Open did not remove the dead run %s", tree.runDir)
	}
	if !slices.Contains(rep.Removed, tree.runDir) && len(warnings) != 0 {
		t.Fatalf("removal reported %+v with warnings %q", rep, warnings)
	}
	if names := rootNames(t, root); !slices.Equal(names, []string{rootLockName}) {
		t.Fatalf("root holds %v; want only root.lock", names)
	}
}

func TestStopEscalatesPastATerminateIgnoringReviewer(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	ready := filepath.Join(t.TempDir(), "ready")
	p := startHelper(t, r, helperCommand("stubborn", readyVar+"="+ready))
	waitFor(t, 5*time.Second, "the Reviewer to ignore terminate", func() bool { return fileExists(ready) })

	const grace = 300 * time.Millisecond
	began := time.Now()
	if err := p.Stop(grace); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(began); elapsed < grace {
		t.Fatalf("Stop returned after %s, before the terminate grace of %s elapsed", elapsed, grace)
	}
	if code := exitCode(t, p.Wait()); code != killedExitCode {
		t.Fatalf("stubborn Reviewer exited %d; want %d from the kill", code, killedExitCode)
	}
	select {
	case <-p.Done():
	default:
		t.Fatal("Done is open after Wait returned")
	}
}

func TestNormalExitSweepsBackgroundGrandchildBeforeWaitReturns(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	pids := filepath.Join(t.TempDir(), "pids")
	p := startHelper(t, r, helperCommand("tree-exit", pidsVar+"="+pids))
	if err := p.Wait(); err != nil {
		t.Fatalf("tree-exit: %v", err)
	}
	tree := readPIDs(t, pids)
	killLater(t, tree)
	if len(tree) < 2 {
		t.Fatalf("tree helper recorded %v; want itself and at least one child", tree)
	}
	if alive := survivors(tree); len(alive) != 0 {
		t.Fatalf("grandchildren %v survived Wait", alive)
	}
}

func TestWaitPassesTheReviewerExitCodeThrough(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	if err := startHelper(t, r, helperCommand("exit", codeVar+"=0")).Wait(); err != nil {
		t.Fatalf("exit 0: %v", err)
	}
	if code := exitCode(t, startHelper(t, r, helperCommand("exit", codeVar+"=7")).Wait()); code != 7 {
		t.Fatalf("exit 7 surfaced as %d", code)
	}
}

func TestStartPassesStartErrorsThroughAndLeavesNoRecord(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	if _, err := r.Start(exec.Command("review-party-no-such-program")); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("unknown program: %v; want exec.ErrNotFound", err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := r.Start(exec.Command(missing)); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing path: %v; want an error naming it", err)
	}
	if records := readProcessRecords(r.dir); len(records) != 0 {
		t.Fatalf("start failures left records %+v", records)
	}
	if err := startHelper(t, r, helperCommand("exit", codeVar+"=0")).Wait(); err != nil {
		t.Fatalf("the run is unusable after a start error: %v", err)
	}
}

func TestSentinelKilledWhileOwnerLivesKillsTheRecordedGroup(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	pids := filepath.Join(t.TempDir(), "pids")
	p := startHelper(t, r, helperCommand("tree", pidsVar+"="+pids))
	waitFor(t, 5*time.Second, "the tree to record its pids", func() bool {
		data, err := os.ReadFile(pids)
		return err == nil && strings.HasSuffix(string(data), "\n")
	})
	tree := readPIDs(t, pids)
	killLater(t, tree)
	if err := p.sentinel.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err == nil {
		t.Fatal("Wait returned nil after the sentinel was killed")
	}
	grouped := tree[:2]
	waitFor(t, 2*time.Second, "the Reviewer and its grouped child to die", func() bool {
		return len(survivors(grouped)) == 0
	})
	if records := readProcessRecords(r.dir); len(records) != 0 {
		t.Fatalf("record %+v survived the sentinel", records)
	}
}

func TestStartFailsWhenTheSentinelDoesNotReportInTime(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	saved := statusTimeout
	statusTimeout = time.Nanosecond
	t.Cleanup(func() { statusTimeout = saved })
	began := time.Now()
	_, err := r.Start(helperCommand("sleep"))
	if !errors.Is(err, errSentinelTimeout) {
		t.Fatalf("Start = %v; want %v", err, errSentinelTimeout)
	}
	if elapsed := time.Since(began); elapsed > 5*time.Second {
		t.Fatalf("Start took %s to give up on the sentinel", elapsed)
	}
	if records := readProcessRecords(r.dir); len(records) != 0 {
		t.Fatalf("timed-out start left records %+v", records)
	}
	statusTimeout = saved
	if err := startHelper(t, r, helperCommand("exit", codeVar+"=0")).Wait(); err != nil {
		t.Fatalf("the run is unusable after a sentinel timeout: %v", err)
	}
}
