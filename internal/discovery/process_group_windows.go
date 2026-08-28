//go:build windows

package discovery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func configureProcessGroup(_ *exec.Cmd) {}

func processTreeCleanupAvailable() bool { return true }

func terminateProcessGroup(command *exec.Cmd) {
	terminateProcessTree(command, false)
}

func killProcessGroup(command *exec.Cmd) {
	terminateProcessTree(command, true)
}

func terminateProcessTree(command *exec.Cmd, force bool) {
	if command.Process == nil {
		return
	}
	arguments := []string{"/PID", strconv.Itoa(command.Process.Pid), "/T"}
	if force {
		arguments = append(arguments, "/F")
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), processCleanupGrace)
	defer cancel()
	_ = exec.CommandContext(cleanupContext, taskkillExecutable(), arguments...).Run()
}

func taskkillExecutable() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "taskkill.exe")
	}
	return "taskkill"
}
