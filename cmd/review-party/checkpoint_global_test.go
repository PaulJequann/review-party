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

func TestRunHintsAtTheMissingGitHookOnceAndOnlyWhenItIsMissing(t *testing.T) {
	fixture := newCheckpointFixture(t)
	if result := fixture.runWith("", false, "config", "checkpoint", "set", "pre-push", "--integration", "git", "--target", "global", "--yes"); result.exit != 0 {
		t.Fatalf("declare = %+v", result)
	}
	run := func() commandRun {
		return fixture.runWith("", false, "run", "--profile", "missing", "--repo", fixture.repository)
	}
	hint := "warning: the pre-push Checkpoint has no git hook in this clone; install it with review-party init --repo " + shellWord(fixture.repository) + "\n"

	result := run()
	if result.exit == 0 || strings.Count(result.stderr, hint) != 1 {
		t.Fatalf("run without a hook = %+v", result)
	}

	fixture.installIntegration("git")
	result = run()
	if result.exit == 0 || strings.Contains(result.stderr, "no git hook") {
		t.Fatalf("run with a hook = %+v", result)
	}
}

func TestConfigCheckpointNoneAndInheritChooseWhetherGlobalCheckpointsApply(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declareGlobal("set", "pre-push")
	refs := pushLine(fixture.commit("one.go", "package app\n\nconst one = 1\n"), fixture.base)
	config := func() string {
		payload, err := os.ReadFile(filepath.Join(fixture.repository, ".reviewparty", "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		return string(payload)
	}
	checkpoint := func(verb string) commandRun {
		return fixture.runWith("", false, "config", "checkpoint", verb, "--repo", fixture.repository, "--yes")
	}

	assertRunContains(t, checkpoint("none"), commandRun{stdout: "warning: Repository Configuration declares no Checkpoints, so the Global pre-push Checkpoint no longer applies here.\npublished\n"})
	if !strings.Contains(config(), `"checkpoints": {}`) {
		t.Fatalf("Repository Configuration after none = %s", config())
	}
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{})
	assertRunContains(t, checkpoint("none"), commandRun{exit: 1, stderr: "Repository Configuration already declares no Checkpoints"})

	assertRunContains(t, checkpoint("inherit"), commandRun{stdout: "warning: Repository Configuration no longer declares Checkpoints of its own, so the Global pre-push Checkpoint now applies here.\npublished\n"})
	if strings.Contains(config(), `"checkpoints"`) {
		t.Fatalf("Repository Configuration after inherit = %s", config())
	}
	if result := fixture.hook(refs, "pre-push", "origin", "url"); result.exit != 1 {
		t.Fatalf("hook with the Global Checkpoint in force = %+v", result)
	}
	assertRunContains(t, checkpoint("inherit"), commandRun{exit: 1, stderr: "Repository Configuration does not replace the Global Checkpoints; there is nothing to inherit"})
}

func TestConfigCheckpointNoneAndInheritOnlyEditRepositoryConfiguration(t *testing.T) {
	fixture := newCheckpointFixture(t)
	for _, verb := range []string{"none", "inherit"} {
		if result := fixture.runWith("", false, "config", "checkpoint", verb, "--target", "global", "--yes"); result.exit == 0 || !strings.Contains(result.stderr, "unknown flag: --target") {
			t.Fatalf("%s --target global = %+v", verb, result)
		}
	}
}
