//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package discovery

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processTreeCleanupAvailable() bool { return true }

func terminateProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		syscall.Kill(-command.Process.Pid, syscall.SIGTERM) //nolint:errcheck // Process-group signaling is best-effort.
	}
}

func killProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL) //nolint:errcheck // Process-group signaling is best-effort.
	}
}
