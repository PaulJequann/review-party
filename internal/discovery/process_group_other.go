//go:build plan9 || js || wasip1

package discovery

import "os/exec"

func configureProcessGroup(_ *exec.Cmd) {}

func processTreeCleanupAvailable() bool { return false }

func terminateProcessGroup(command *exec.Cmd) {
	killProcessGroup(command)
}

func killProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
