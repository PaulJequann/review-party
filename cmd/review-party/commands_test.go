package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootHelpSucceedsAndPointsToConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), nil, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "review-party config") {
		t.Fatalf("help does not point to configuration:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUsageErrorIsConciseAndReturnsEstablishedExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{"inspect"}, &stdout, &stderr)
	if exit != usageExitCode {
		t.Fatalf("exit = %d, want %d", exit, usageExitCode)
	}
	if strings.Count(stderr.String(), "review-party:") != 1 {
		t.Fatalf("diagnostic = %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("usage was printed for a concise argument error:\n%s", stderr.String())
	}
}

func TestFlagAccessRejectsUnregisteredName(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	defer func() {
		if recover() == nil {
			t.Fatal("stringFlag accepted an unregistered flag")
		}
	}()
	stringFlag(cmd, "misspelled")
}

func TestRunRejectsConflictingExplicitSelection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{"run", "--profile", "a", "--party", "b"}, &stdout, &stderr)
	if exit != usageExitCode {
		t.Fatalf("exit = %d, want %d", exit, usageExitCode)
	}
	if !strings.Contains(stderr.String(), "profile") || !strings.Contains(stderr.String(), "party") {
		t.Fatalf("diagnostic does not name the conflict: %q", stderr.String())
	}
}

func TestUnknownCommandSuggestsNearestTask(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{"histor"}, &stdout, &stderr)
	if exit != usageExitCode {
		t.Fatalf("exit = %d, want %d", exit, usageExitCode)
	}
	if !strings.Contains(stderr.String(), "history") {
		t.Fatalf("diagnostic has no useful suggestion: %q", stderr.String())
	}
}
