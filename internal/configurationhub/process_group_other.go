//go:build plan9 || js || wasip1

package configurationhub

import "os/exec"

func configureEditorProcessGroup(_ *exec.Cmd) {}

func terminateEditorProcessGroup(command *exec.Cmd) {
	killEditorProcessGroup(command)
}

func killEditorProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
