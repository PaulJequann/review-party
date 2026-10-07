//go:build unix

package hostrun

import (
	"syscall"
	"testing"
	"time"
)

func TestReapSparesAGroupWhoseLeaderStartStampDiffers(t *testing.T) {
	root := testRoot(t)
	cmd := helperCommand("sleep")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	leader := startChild(t, cmd)
	leaderID, err := identify(leader.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	stale := leaderID
	stale.Start++
	fakeDeadRun(t, root, "1-dead",
		ProcessRecord{Schema: recordSchema, Sentinel: stale, Reviewer: stale, Group: leaderID.PID, Started: fakeTime, Program: "stale", Mode: TreeGroup})

	rep := openRun(t, root, nil).Reap()
	if len(rep.Leftovers) != 0 {
		t.Fatalf("reap left %+v", rep.Leftovers)
	}
	time.Sleep(200 * time.Millisecond)
	if !running(leader.Process.Pid) {
		t.Fatal("a group whose leader has a different start stamp was signalled")
	}
}
