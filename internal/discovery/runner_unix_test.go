//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package discovery

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunnerForceKillsAProcessGroupAfterGracePeriod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	environment := []string{"DISCOVERY_RUNNER_HELPER=spawn", "DISCOVERY_RUNNER_PID_FILE=" + pidFile}
	started := time.Now()
	runDone := make(chan RunResult, 1)
	go func() {
		runDone <- NewDefaultRunner().Run(ctx, Command{
			Args:        []string{os.Args[0], "-test.run=TestDiscoveryRunnerHelper"},
			Environment: environment,
		})
	}()
	waitForFile(t, pidFile)
	cancel()
	run := <-runDone
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("runner waited %s for a SIGTERM-resistant process group", elapsed)
	}
	if !run.Canceled || run.Err == nil {
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

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("check readiness file: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("readiness file %q was not created", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
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

func TestTrustedExecutableIgnoresUntrustedPathEntries(t *testing.T) {
	temp := t.TempDir()
	if err := os.WriteFile(filepath.Join(temp, "sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", temp)
	resolved, err := trustedExecutable("sh")
	if err != nil {
		t.Fatal(err)
	}
	if resolved == filepath.Join(temp, "sh") || pathWithinDirectory(temp, resolved) {
		t.Fatalf("trusted executable resolved to repository-local PATH entry: %q", resolved)
	}
}

func TestTrustedExecutableIncludesKnownUserInstallationPaths(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	wanted := filepath.Join(bin, "reviewer-harness")
	if err := os.WriteFile(wanted, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	resolved, err := trustedExecutable("reviewer-harness")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wanted {
		t.Fatalf("trusted executable = %q, want %q", resolved, wanted)
	}
}

func TestEnvironmentExcludesUntrustedPathEntries(t *testing.T) {
	untrusted := t.TempDir()
	t.Setenv("PATH", untrusted)
	for _, entry := range environmentFor("codex") {
		if strings.HasPrefix(entry, "PATH=") {
			if strings.Contains(entry, untrusted) {
				t.Fatalf("environment retained untrusted PATH: %q", entry)
			}
			return
		}
	}
	t.Fatal("environment omitted trusted PATH")
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
		if errors.Is(err, syscall.ESRCH) || processIsZombie(pid) {
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

func processIsZombie(pid int) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	contents, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	closingCommand := strings.LastIndex(string(contents), ") ")
	if closingCommand < 0 {
		return false
	}
	state := strings.Fields(string(contents[closingCommand+2:]))
	return len(state) > 0 && state[0] == "Z"
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
