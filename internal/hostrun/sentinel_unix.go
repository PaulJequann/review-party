//go:build unix

package hostrun

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	controlFD = 3
	statusFD  = 4
)

// The pipes travel as fds 3 and 4, so the sentinel needs no extra argv.
func sentinelArgs(*os.File, *os.File) []string { return nil }

// attachPipes hands the sentinel its two pipes and its own process group, so
// a terminal signal aimed at the owner's group never reaches it.
func attachPipes(sentinel *exec.Cmd, controlRead, statusWrite *os.File) {
	sentinel.ExtraFiles = []*os.File{controlRead, statusWrite}
	sentinel.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// ExtraFiles arrive without close-on-exec, so the sentinel sets it before the
// Reviewer exists; otherwise the Reviewer tree would inherit both pipes.
func sentinelPipes([]string) (control, status *os.File, err error) {
	syscall.CloseOnExec(controlFD)
	syscall.CloseOnExec(statusFD)
	return os.NewFile(controlFD, "control"), os.NewFile(statusFD, "status"), nil
}

func releaseOwnerPipes() {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer null.Close() //nolint:errcheck // The duplicates keep /dev/null open; the original fd is spent.
	for fd := 0; fd <= 2; fd++ {
		_ = unix.Dup2(int(null.Fd()), fd) //nolint:errcheck // A descriptor that will not take /dev/null keeps the inherited stream; the sentinel still works.
	}
}

// The Reviewer leads its own process group; signals to -group reach every
// descendant that did not call setsid.
func (t *tree) started(pid int) {
	t.pid = pid
	t.group = pid
}

func (t *tree) terminate() {
	_ = syscall.Kill(-t.group, syscall.SIGTERM) //nolint:errcheck // A process that is already gone is the goal.
}

func exitStatus(state *os.ProcessState) int {
	status, ok := state.Sys().(syscall.WaitStatus)
	if ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return state.ExitCode()
}
