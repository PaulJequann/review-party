//go:build windows

package hostrun

import (
	"os/exec"
	"syscall"
)

func escapeeCommand(mode string) *exec.Cmd {
	cmd := helperCommand(mode)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	return cmd
}
