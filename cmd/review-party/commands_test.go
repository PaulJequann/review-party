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

func TestPartyRunAcceptsConfiguredDefaultWithoutName(t *testing.T) {
	root := newRootCommand(productionCommandIO(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}))
	cmd, _, err := root.Find([]string{"party", "run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Fatalf("party run rejected configured default selection: %v", err)
	}
	if !strings.Contains(cmd.Use, "[PARTY]") {
		t.Fatalf("use = %q, want optional Party", cmd.Use)
	}
}

func TestCobraRejectsInvalidTypedFlagBeforeExecution(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{"party", "run", "standard", "--concurrency", "many"}, &stdout, &stderr)
	if exit != usageExitCode {
		t.Fatalf("exit = %d, want %d", exit, usageExitCode)
	}
	if !strings.Contains(stderr.String(), `invalid argument "many" for "--concurrency"`) {
		t.Fatalf("diagnostic = %q", stderr.String())
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

func TestCompletionListsPackagedProfilesAndReviewers(t *testing.T) {
	cases := map[string]struct {
		arguments []string
		expected  string
	}{
		"profile":  {arguments: []string{"__complete", "review", "b"}, expected: "bugs"},
		"party":    {arguments: []string{"__complete", "party", "run", "sta"}, expected: "standard"},
		"reviewer": {arguments: []string{"__complete", "review", "--reviewer", "open"}, expected: "opencode"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if exit := run(context.Background(), testCase.arguments, &stdout, &stderr); exit != 0 {
				t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
			}
			if !strings.Contains(stdout.String(), testCase.expected) {
				t.Fatalf("completion = %q, want %q", stdout.String(), testCase.expected)
			}
		})
	}
}
