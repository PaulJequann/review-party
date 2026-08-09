package reviewparty

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestNonzeroHarnessExitCannotCompleteValidPayload(t *testing.T) {
	run := commandRun{WaitErr: errors.New("exit status 1")}
	execution := finalizeHarnessRun(run, cleanReview, "authentication failed", "model", "high", "reviewer")
	if execution.Outcome != AttemptReviewerUnavailable || execution.AssistantText != cleanReview {
		t.Fatalf("execution = %#v, want unavailable with preserved output", execution)
	}
}

func TestProcessGroupCanBeTerminated(t *testing.T) {
	command := exec.Command("sh", "-c", "sleep 30")
	configureProcessGroup(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	terminateProcessGroup(command)

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		killProcessGroup(command)
		t.Fatal("process did not terminate")
	}
}

func TestCommandRunnerReturnsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	run := runCommand(ctx, exec.Command("sh", "-c", "sleep 30"))
	if !errors.Is(run.ContextErr, context.DeadlineExceeded) {
		t.Fatalf("context error = %v, want deadline exceeded", run.ContextErr)
	}
}

func TestBoundedBufferReportsOverflowWithoutGrowing(t *testing.T) {
	buffer := newBoundedBuffer(4)
	written, err := buffer.Write([]byte("123456"))
	if err != nil {
		t.Fatal(err)
	}
	if written != 6 {
		t.Fatalf("written = %d", written)
	}
	if !buffer.overflow {
		t.Fatal("buffer did not report overflow")
	}
	if string(buffer.Bytes()) != "1234" {
		t.Fatalf("buffer = %q", buffer.Bytes())
	}
}
