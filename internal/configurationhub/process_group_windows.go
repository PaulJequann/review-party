//go:build windows

package configurationhub

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func configureEditorProcessGroup(_ *exec.Cmd) {}

func terminateEditorProcessGroup(command *exec.Cmd) {
	terminateEditorProcessTree(command, false)
}

func killEditorProcessGroup(command *exec.Cmd) {
	terminateEditorProcessTree(command, true)
}

func terminateEditorProcessTree(command *exec.Cmd, force bool) {
	if command.Process == nil {
		return
	}
	arguments := []string{"/PID", strconv.Itoa(command.Process.Pid), "/T"}
	if force {
		arguments = append(arguments, "/F")
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), editorProcessCleanupGrace)
	defer cancel()
	_ = exec.CommandContext(cleanupContext, editorTaskkillExecutable(), arguments...).Run()
}

func editorTaskkillExecutable() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "taskkill.exe")
	}
	return "taskkill"
}
