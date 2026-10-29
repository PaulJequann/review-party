package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (fixture checkpointFixture) declareGlobal(verb, name string) {
	fixture.t.Helper()
	if result := fixture.runWith("", false, "config", "checkpoint", verb, name, "--target", "global", "--yes"); result.exit != 0 {
		fixture.t.Fatalf("%s global %s = %+v", verb, name, result)
	}
}

func TestCheckpointHookEnforcesAGlobalCheckpointInAnEnrolledRepository(t *testing.T) {
	fixture := newCheckpointFixture(t)
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(root, "review-party", "config.json")
	fixture.declareGlobal("set", "pre-push")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	refs := pushLine(head, fixture.base)

	refusal := "review-party: pre-push Checkpoint (declared in " + global + "): 3 unreviewed lines in " + fixture.base[:12] + ".." + head[:12] +
		"; next: review-party run --unreviewed --base " + fixture.base + " --head " + head + "\n"
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{exit: 1, stderr: refusal})

	exit, stdout, stderr := fixture.check("pre-push", "--base", fixture.base)
	if exit != 1 || !strings.Contains(stdout, "declared in: global "+global+"\n") {
		t.Fatalf("check = %d %q %q", exit, stdout, stderr)
	}

	if err := os.Remove(filepath.Join(fixture.repository, ".reviewparty", "config.json")); err != nil {
		t.Fatal(err)
	}
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{})
}

func TestRepositoryCheckpointsReplaceGlobalOnesAsAWhole(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declareGlobal("set", "pre-push")
	fixture.declare("pre-commit")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")

	assertRun(t, fixture.hook(pushLine(head, fixture.base), "pre-push", "origin", "url"), commandRun{})
	fixture.declareGlobal("remove", "pre-push")
}

func TestCheckpointInstallGitInstallsForAGlobalCheckpoint(t *testing.T) {
	fixture := newHookInstallFixture(t)
	fixture.declareGlobal("set", "pre-push")
	if result := fixture.install("--yes"); result.exit != 0 {
		t.Fatalf("install = %+v", result)
	}
	if !strings.Contains(fixture.read(filepath.Join(".git", "hooks", "pre-push")), "review-party checkpoint hook git pre-push") {
		t.Fatal("the pre-push hook does not run the Checkpoint hook")
	}
}
