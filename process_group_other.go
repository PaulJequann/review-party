//go:build windows || plan9 || js || wasip1

package reviewparty

import "os/exec"

func configureProcessGroup(_ *exec.Cmd) {}

func terminateProcessGroup(command *exec.Cmd) {
	killProcessGroup(command)
}

func killProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
