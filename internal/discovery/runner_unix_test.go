//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package discovery

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunnerForceKillsAProcessGroupAfterGracePeriod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	environment := []string{"DISCOVERY_RUNNER_HELPER=spawn", "DISCOVERY_RUNNER_PID_FILE=" + pidFile}
	started := time.Now()
	run := NewDefaultRunner().Run(ctx, Command{
		Args:        []string{os.Args[0], "-test.run=TestDiscoveryRunnerHelper"},
		Environment: environment,
	})
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("runner waited %s for a SIGTERM-resistant process group", elapsed)
	}
	if !run.TimedOut || run.Err == nil {
		t.Fatalf("run = %#v", run)
	}
	contents, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read child PID: %v", err)
	}
	pid, err := strconv.Atoi(string(contents))
	if err != nil {
		t.Fatalf("parse child PID %q: %v", contents, err)
	}
	waitForProcessExit(t, pid)
}

func TestRunnerReportsAlreadyCancelledProcessAsIncomplete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run := NewDefaultRunner().Run(ctx, Command{Args: []string{"true"}, Environment: environmentFor("true")})
	if !run.Canceled || run.Err == nil {
		t.Fatalf("run = %#v", run)
	}
}

func TestRunnerUsesExplicitAllowlistedEnvironment(t *testing.T) {
	t.Setenv("DISCOVERY_RUNNER_UNLISTED_SECRET", "should-not-leak")
	run := NewDefaultRunner().Run(context.Background(), Command{
		Args:        []string{"sh", "-c", "printf '%s' \"$DISCOVERY_RUNNER_UNLISTED_SECRET\""},
		Environment: environmentFor("codex"),
	})
	if run.Err != nil || string(run.Stdout) != "" {
		t.Fatalf("run = %#v", run)
	}
}

func TestEnvironmentIncludesUserProfileRuntimeVariables(t *testing.T) {
	variables := map[string]string{
		"USERPROFILE":  "/users/review-party",
		"APPDATA":      "/users/review-party/appdata",
		"LOCALAPPDATA": "/users/review-party/localappdata",
		"SystemRoot":   "/windows",
		"TEMP":         "/tmp",
		"TMP":          "/tmp",
		"PATHEXT":      ".COM;.EXE",
		"ComSpec":      "/windows/system32/cmd.exe",
	}
	for name, value := range variables {
		t.Setenv(name, value)
	}
	entries := environmentFor("codex")
	for name, value := range variables {
		if !containsEnvironmentEntry(entries, name+"="+value) {
			t.Fatalf("environment omitted %s", name)
		}
	}
}

func containsEnvironmentEntry(entries []string, wanted string) bool {
	for _, entry := range entries {
		if entry == wanted {
			return true
		}
	}
	return false
}

func TestDiscoveryRunnerHelper(t *testing.T) {
	switch os.Getenv("DISCOVERY_RUNNER_HELPER") {
	case "spawn":
		runDiscoverySpawnHelper(t)
	case "hold":
		runDiscoveryHoldHelper()
	}
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			t.Fatalf("check child PID %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("child PID %d is still alive after process-group cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runDiscoverySpawnHelper(t *testing.T) {
	signal.Ignore(syscall.SIGTERM)
	command := exec.Command(os.Args[0], "-test.run=TestDiscoveryRunnerHelper")
	command.Env = helperEnvironment("hold")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryHelperPID(t, os.Getenv("DISCOVERY_RUNNER_PID_FILE"), command.Process.Pid)
	for {
		time.Sleep(time.Hour)
	}
}

func writeDiscoveryHelperPID(t *testing.T, path string, pid int) {
	t.Helper()
	if path == "" {
		return
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runDiscoveryHoldHelper() {
	signal.Ignore(syscall.SIGTERM)
	for {
		time.Sleep(time.Hour)
	}
}

func helperEnvironment(value string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "DISCOVERY_RUNNER_HELPER=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "DISCOVERY_RUNNER_HELPER="+value)
}
