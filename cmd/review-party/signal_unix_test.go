//go:build unix

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestConfigurationHubContextCancelsOnHangup(t *testing.T) {
	ctx, stop := configurationHubContext(context.Background())
	defer stop()

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context was not cancelled by SIGHUP")
	}
}

func TestSecondSignalTerminatesDuringCleanup(t *testing.T) {
	child, lines := startSignalHelper(t, "default")
	if err := child.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	awaitHelperLine(t, lines, "cleaning after "+syscall.SIGHUP.String()+" signal received")

	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	assertKilledBy(t, signalUntilExit(t, child.Process, exited, syscall.SIGTERM), syscall.SIGTERM)
}

func TestHangupIgnoredAtStartStaysIgnored(t *testing.T) {
	child, lines := startSignalHelper(t, "nohup")
	if err := child.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	awaitHelperLine(t, lines, "cleaning after "+syscall.SIGTERM.String()+" signal received")
}

func startSignalHelper(t *testing.T, mode string) (*exec.Cmd, *bufio.Scanner) {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^TestSignalContextHelperProcess$")
	child.Env = append(os.Environ(), "REVIEW_PARTY_SIGNAL_HELPER="+mode)
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := child.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Error(err)
		}
		_ = child.Wait() //nolint:errcheck // The helper was killed; only reaping it matters.
	})
	lines := bufio.NewScanner(output)
	awaitHelperLine(t, lines, "ready")
	return child, lines
}

func signalUntilExit(t *testing.T, process *os.Process, exited <-chan error, signal syscall.Signal) error {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if err := process.Signal(signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Fatal(err)
		}
		select {
		case err := <-exited:
			return err
		case <-deadline:
			t.Fatal("second signal did not terminate the process while cleanup was running")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestSignalContextHelperProcess(t *testing.T) {
	mode := os.Getenv("REVIEW_PARTY_SIGNAL_HELPER")
	if mode == "" {
		t.Skip("helper process")
	}
	if mode == "nohup" {
		signal.Ignore(syscall.SIGHUP)
	}
	ctx, stop := signalContext(context.Background())
	defer stop()
	fmt.Println("ready")
	<-ctx.Done()
	fmt.Println("cleaning after", context.Cause(ctx))
	time.Sleep(time.Minute)
	os.Exit(0)
}

func awaitHelperLine(t *testing.T, lines *bufio.Scanner, want string) {
	t.Helper()
	if !lines.Scan() {
		t.Fatalf("helper exited before %q: %v", want, lines.Err())
	}
	if got := lines.Text(); got != want {
		t.Fatalf("helper line = %q, want %q", got, want)
	}
}

func assertKilledBy(t *testing.T, err error, want syscall.Signal) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("helper exit = %v, want death by %v", err, want)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("helper exit status has type %T", exitErr.Sys())
	}
	if status.Signal() != want {
		t.Fatalf("helper exit = %v, want death by %v", err, want)
	}
}
