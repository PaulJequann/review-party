//go:build linux

package hostrun

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const sweepBudget = 2 * time.Second

const sweepPoll = 20 * time.Millisecond

type platformTree struct{}

// The thread stays locked for the sentinel's life: Pdeathsig fires when the
// forking thread dies.
func newPlatformTree() (platformTree, TreeMode, error) {
	runtime.LockOSThread()
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return platformTree{}, TreeGroup, nil
	}
	return platformTree{}, TreeSubreaper, nil
}

func (t *tree) attach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

func (t *tree) kill() {
	_ = syscall.Kill(-t.group, syscall.SIGKILL) //nolint:errcheck // A process that is already gone is the goal.
	sentinelGroup := syscall.Getpgrp()
	for _, child := range adoptedChildren() {
		_ = syscall.Kill(child.pid, syscall.SIGKILL) //nolint:errcheck // A process that is already gone is the goal.
		if child.pgrp != sentinelGroup {
			_ = syscall.Kill(-child.pgrp, syscall.SIGKILL) //nolint:errcheck // A process that is already gone is the goal.
		}
	}
}

func (t *tree) sweep() {
	deadline := time.Now().Add(sweepBudget)
	for {
		t.kill()
		if reapedAll() {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(sweepPoll)
	}
}

func reapedAll() bool {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err == syscall.ECHILD {
			return true
		}
		if err != nil || pid <= 0 {
			return false
		}
	}
}

type adopted struct {
	pid  int
	pgrp int
}

// A /proc scan needs no kernel option, unlike /proc/<pid>/task/<tid>/children.
func adoptedChildren() []adopted {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var children []adopted
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		stat, err := readProcStat(pid)
		if err == nil && stat.ppid == self {
			children = append(children, adopted{pid: pid, pgrp: stat.pgrp})
		}
	}
	return children
}
