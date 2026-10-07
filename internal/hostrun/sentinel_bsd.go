//go:build unix && !linux

package hostrun

import (
	"os/exec"
	"syscall"
)

type platformTree struct{}

func newPlatformTree() (platformTree, TreeMode, error) {
	return platformTree{}, TreeGroup, nil
}

func (t *tree) attach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (t *tree) kill() {
	_ = syscall.Kill(-t.group, syscall.SIGKILL) //nolint:errcheck // A process that is already gone is the goal.
}

func (t *tree) sweep() {
	t.kill()
}
