//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package configurationhub

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEditorProcessGroupForegroundsInteractiveTerminal(t *testing.T) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("controlling terminal unavailable: %v", err)
	}
	defer func() { _ = terminal.Close() }() //nolint:errcheck // Test terminal cleanup is best-effort.

	command := exec.Command("true")
	command.Stdin = terminal
	restore, err := configureEditorProcessGroup(command)
	if err != nil {
		t.Fatalf("configure editor process group: %v", err)
	}
	if !command.SysProcAttr.Foreground {
		t.Fatal("interactive editor was not configured for the terminal foreground")
	}
	if command.SysProcAttr.Ctty != int(terminal.Fd()) {
		t.Fatalf("editor controlling terminal = %d, want %d", command.SysProcAttr.Ctty, terminal.Fd())
	}
	if err := restore(); err != nil {
		t.Fatalf("restore terminal foreground process group: %v", err)
	}
}

func TestInstructionEditorCancellationTerminatesDescendants(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("shell unavailable: %v", err)
	}
	pidFile := filepath.Join(t.TempDir(), "editor-pids")
	t.Setenv("EDITOR", "sh -c \"sleep 30 & child=$!; echo $$:$child > $1; trap : TERM; wait $child\" editor "+pidFile)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := editInstructions(ctx, "", strings.NewReader(""), io.Discard)
		result <- err
	}()
	shellPID, childPID := readEditorPIDs(t, pidFile)
	t.Cleanup(func() { _ = syscall.Kill(-shellPID, syscall.SIGKILL) }) //nolint:errcheck // Test cleanup is best-effort.
	cancel()
	if err := waitForEditorResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("editor error = %v, want context cancellation", err)
	}
	waitForEditorExit(t, childPID)
}

func readEditorPIDs(t *testing.T, path string) (int, int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if shellPID, childPID, found := parseEditorPIDs(path); found {
			return shellPID, childPID
		}
		if time.Now().After(deadline) {
			t.Fatal("editor did not publish process IDs")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func parseEditorPIDs(path string) (int, int, bool) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimSpace(string(payload)), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	shellPID, shellErr := strconv.Atoi(parts[0])
	childPID, childErr := strconv.Atoi(parts[1])
	if shellErr != nil || childErr != nil {
		return 0, 0, false
	}
	return shellPID, childPID, true
}

func waitForEditorResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("editor did not return after cancellation")
		return nil
	}
}

func waitForEditorExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("editor descendant %d is still running", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
