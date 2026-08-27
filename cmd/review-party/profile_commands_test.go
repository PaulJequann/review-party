package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInitPreparesStateWithoutCreatingProfiles(t *testing.T) {
	isolateProfileCommandEnvironment(t)
	repository := t.TempDir()
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"init", "--repo", repository}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(repository, ".reviewparty", "profiles")); !os.IsNotExist(err) {
		t.Fatalf("init created Profiles: %v", err)
	}
}

func TestRetiredProfileMutationCommandsAreAbsent(t *testing.T) {
	for _, arguments := range [][]string{{"profile", "create"}, {"profile", "install-defaults"}} {
		var stdout, stderr bytes.Buffer
		if exit := run(context.Background(), arguments, &stdout, &stderr); exit != usageExitCode {
			t.Fatalf("run(%v) exit = %d, stderr = %q", arguments, exit, stderr.String())
		}
	}
}

func TestOrdinaryRunRejectsExecutionOverrides(t *testing.T) {
	for _, flag := range []string{"--reviewer", "--model", "--effort", "--deadline"} {
		var stdout, stderr bytes.Buffer
		arguments := []string{"run", flag, "value"}
		if flag == "--deadline" {
			arguments[len(arguments)-1] = "1m"
		}
		if exit := run(context.Background(), arguments, &stdout, &stderr); exit != usageExitCode {
			t.Fatalf("flag %s exit = %d, stderr = %q", flag, exit, stderr.String())
		}
	}
}

func runProfileTestCommand(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", command.Args, err, output)
	}
}

func isolateProfileCommandEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}
