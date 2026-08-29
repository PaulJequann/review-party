//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package configurationhub

import (
	"os/exec"
	"syscall"
)

func configureEditorProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateEditorProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		syscall.Kill(-command.Process.Pid, syscall.SIGTERM) //nolint:errcheck // Process-group signaling is best-effort.
	}
}

func killEditorProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL) //nolint:errcheck // Process-group signaling is best-effort.
	}
}
