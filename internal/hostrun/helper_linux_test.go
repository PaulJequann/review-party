//go:build linux

package hostrun

import (
	"os/exec"
	"syscall"
)

func escapeeCommand(mode string) *exec.Cmd {
	cmd := helperCommand(mode)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}
