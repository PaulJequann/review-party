package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

const zeroObject = "0000000000000000000000000000000000000000"

func (fixture checkpointFixture) hook(input string, arguments ...string) commandRun {
	fixture.t.Helper()
	fixture.t.Chdir(fixture.repository)
	return fixture.runWith(input, false, append([]string{"checkpoint", "hook", "git"}, arguments...)...)
}

func pushLine(local, remote string) string {
	return "refs/heads/main " + local + " refs/heads/main " + remote + "\n"
}

func TestCheckpointHookAllowsSilentlyWhenUndeclared(t *testing.T) {
	fixture := newCheckpointFixture(t)
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	assertRun(t, fixture.hook(pushLine(head, fixture.base), "pre-push", "origin", "url"), commandRun{})
	assertRun(t, fixture.hook("", "pre-commit"), commandRun{})
}

func TestCheckpointHookRefusesAPushUntilReviewed(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	refs := pushLine(head, fixture.base)

	refusal := "review-party: pre-push Checkpoint has no completed Review of " + fixture.base[:12] + ".." + head[:12] +
		"; next: review-party run --base " + fixture.base + " --head " + head + "\n"
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{exit: 1, stderr: refusal})

	deletion := "(delete) " + zeroObject + " refs/heads/old " + head + "\n"
	assertRun(t, fixture.hook(deletion, "pre-push", "origin", "url"), commandRun{})

	changes := fixture.rangeChanges(head).Changes
	fixture.saveReview("rp_1725192000000_00000000000000c1", "bugs", model.LifecycleCompleted, changes)
	fixture.saveReview("rp_1725192000000_00000000000000c2", "docs", model.LifecycleRunning, changes)
	waiting := "review-party: pre-push Checkpoint is waiting on a running Review of " + fixture.base[:12] + ".." + head[:12] +
		"; wait: review-party wait rp_1725192000000_00000000000000c2\n"
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{exit: 1, stderr: waiting})

	fixture.saveReview("rp_1725192000000_00000000000000c2", "docs", model.LifecycleCompleted, changes)
	assertRun(t, fixture.hook(refs, "pre-push", "origin", "url"), commandRun{})
}

func TestCheckpointHookWarnsAndAllowsWithoutADecision(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")

	noBase := fixture.hook(pushLine(head, zeroObject), "pre-push", "origin", "url")
	want := "review-party: warning: pre-push Checkpoint not checked: refs/heads/main: no remote object, remote HEAD, or origin/HEAD to compare with\n"
	assertRun(t, noBase, commandRun{stderr: want})

	garbled := fixture.hook("refs/heads/main "+head+"\n", "pre-push", "origin", "url")
	assertOneWarning(t, garbled)

	fixture.writeFile(".reviewparty/config.json", "{")
	assertOneWarning(t, fixture.hook(pushLine(head, fixture.base), "pre-push", "origin", "url"))
}

// assertOneWarning fails unless the hook allowed with a single warning line.
func assertOneWarning(t *testing.T, result commandRun) {
	t.Helper()
	assertRunContains(t, result, commandRun{stderr: "review-party: warning: pre-push Checkpoint not checked: "})
	if lines := strings.Count(result.stderr, "\n"); lines != 1 {
		t.Fatalf("warning spans %d lines: %q", lines, result.stderr)
	}
}

func TestCheckpointHookPreCommitChecksStagedContentAndOffersAnyoneWaivers(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-commit", "--waivers", "anyone")
	fixture.writeFile("one.go", "package app\n")
	fixture.git("add", "one.go")

	refusal := "review-party: pre-commit Checkpoint has no completed Review of the staged changes; next: review-party run" +
		`; or waive: review-party checkpoint waive pre-commit --reason "<why>"` + "\n"
	assertRun(t, fixture.hook("", "pre-commit"), commandRun{exit: 1, stderr: refusal})
}

// hookInstallFixture is a repository with a declared Checkpoint and a PATH
// whose only review-party, when present, is a stand-in that saves the refs it
// was given to captured.
type hookInstallFixture struct {
	checkpointFixture
	bin      string
	captured string
}

func newHookInstallFixture(t *testing.T, checkpoints ...configuration.CheckpointName) hookInstallFixture {
	t.Helper()
	fixture := hookInstallFixture{checkpointFixture: newCheckpointFixture(t), bin: t.TempDir(), captured: filepath.Join(t.TempDir(), "review-party-stdin")}
	for _, checkpoint := range checkpoints {
		fixture.declare(string(checkpoint))
	}
	t.Setenv("PATH", fixture.bin+":/usr/bin:/bin")
	if path, err := exec.LookPath("review-party"); err == nil {
		t.Skipf("review-party is installed system-wide at %s", path)
	}
	return fixture
}

func (fixture hookInstallFixture) install(arguments ...string) commandRun {
	fixture.t.Helper()
	return fixture.runWith("", false, append([]string{"checkpoint", "install", "git", "--repo", fixture.repository}, arguments...)...)
}

func (fixture hookInstallFixture) read(path string) string {
	fixture.t.Helper()
	content, err := os.ReadFile(filepath.Join(fixture.repository, path))
	if err != nil {
		fixture.t.Fatal(err)
	}
	return string(content)
}

func (fixture hookInstallFixture) provideStandIn() {
	fixture.t.Helper()
	script := "#!/bin/sh\ncat > " + fixture.captured + "\n"
	if err := os.WriteFile(filepath.Join(fixture.bin, "review-party"), []byte(script), 0o755); err != nil {
		fixture.t.Fatal(err)
	}
}

// pushedInput is what a pre-push hook run left behind: its warnings, the refs
// the team's own hook read, and the refs review-party read.
type pushedInput struct {
	stderr      string
	teamHookSaw string
	reviewSaw   string
}

// runPrePush runs the pre-push hook as git would, with the refs on standard input.
func (fixture hookInstallFixture) runPrePush(refs string) pushedInput {
	fixture.t.Helper()
	command := exec.Command(filepath.Join(fixture.repository, ".git", "hooks", "pre-push"), "origin", "url")
	command.Dir = fixture.repository
	command.Stdin = strings.NewReader(refs)
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		fixture.t.Fatalf("pre-push hook: %v\n%s", err, stderr.String())
	}
	result := pushedInput{stderr: stderr.String(), teamHookSaw: fixture.read("hook-saw")}
	if content, err := os.ReadFile(fixture.captured); err == nil {
		result.reviewSaw = string(content)
	}
	return result
}

func TestCheckpointInstallKeepsAnExistingPrePushHookAndItsInput(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	original := "#!/bin/sh\n# team hook\ncat > hook-saw\n"
	fixture.writeFile(".git/hooks/pre-push", original)
	if err := os.Chmod(filepath.Join(fixture.repository, ".git/hooks/pre-push"), 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(fixture.repository, ".git", "hooks", "pre-push")

	installed := fixture.install("--yes")
	want := "pre-push: add the review-party block to " + hookPath + " (git hooks)\n" +
		"warning: review-party is not on PATH; the hooks warn and allow until it is\n" +
		"Wrote 1 hook file(s).\n"
	assertRun(t, installed, commandRun{stdout: want})
	content := fixture.read(".git/hooks/pre-push")
	if want := "#!/bin/sh\n" + checkpointHookBlock(configuration.CheckpointPrePush, "") + "# team hook\ncat > hook-saw\n"; content != want {
		t.Fatalf("hook = %q, want %q", content, want)
	}

	refs := pushLine("1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222") +
		"refs/heads/topic 3333333333333333333333333333333333333333 refs/heads/topic " + zeroObject + "\n"
	missing := pushedInput{stderr: "review-party: warning: review-party is not on PATH; pre-push Checkpoint not checked\n", teamHookSaw: refs}
	if got := fixture.runPrePush(refs); got != missing {
		t.Fatalf("without review-party = %+v, want %+v", got, missing)
	}
	fixture.provideStandIn()
	if got, want := fixture.runPrePush(refs), (pushedInput{teamHookSaw: refs, reviewSaw: refs}); got != want {
		t.Fatalf("with review-party = %+v, want %+v", got, want)
	}

	again := fixture.install()
	assertRun(t, again, commandRun{stdout: "pre-push: already installed in " + hookPath + " (git hooks)\n"})
	edited := strings.Replace(content, "|| exit $?", "|| true", 1)
	fixture.writeFile(".git/hooks/pre-push", edited)
	assertRun(t, fixture.install(), commandRun{stdout: "pre-push: edited review-party block left unchanged in " + hookPath + " (git hooks)\n"})
	if fixture.read(".git/hooks/pre-push") != edited {
		t.Fatal("an edited block was rewritten")
	}
}

func TestCheckpointInstallCreatesMissingHooksWhereTheToolRunsThem(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(hookInstallFixture)
		hook   string
		tool   string
		absent string
	}{
		{name: "plain", setup: func(hookInstallFixture) {}, hook: ".git/hooks/pre-commit", tool: "git hooks"},
		{name: "core.hooksPath", setup: func(fixture hookInstallFixture) { fixture.git("config", "core.hooksPath", ".githooks") }, hook: ".githooks/pre-commit", tool: "core.hooksPath"},
		{name: "husky", setup: func(fixture hookInstallFixture) { fixture.writeFile(".husky/_/h", "") }, hook: ".husky/pre-commit", tool: "husky"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHookInstallFixture(t, configuration.CheckpointPreCommit)
			fixture.provideStandIn()
			test.setup(fixture)
			result := fixture.install("--yes")
			want := "pre-commit: create " + filepath.Join(fixture.repository, test.hook) + " (" + test.tool + ")\nWrote 1 hook file(s).\n"
			assertRun(t, result, commandRun{stdout: want})
			info, err := os.Stat(filepath.Join(fixture.repository, test.hook))
			if err != nil {
				t.Fatal(err)
			}
			if mode := info.Mode().Perm(); mode != 0o755 {
				t.Fatalf("created hook mode = %v", mode)
			}
			if content, want := fixture.read(test.hook), "#!/bin/sh\n"+checkpointHookBlock(configuration.CheckpointPreCommit, ""); content != want {
				t.Fatalf("created hook = %q, want %q", content, want)
			}
		})
	}
}

func TestCheckpointInstallNeedsConfirmationToWrite(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	assertRunContains(t, fixture.install(), commandRun{exit: usageExitCode, stderr: "rerun with --yes"})
	if _, err := os.Stat(filepath.Join(fixture.repository, ".git", "hooks", "pre-push")); err == nil {
		t.Fatal("an unconfirmed install wrote the hook")
	}
	declined := fixture.runWith("n\n", true, "checkpoint", "install", "git", "--repo", fixture.repository)
	assertRunContains(t, declined, commandRun{exit: 1, stderr: "hooks not written"})
	if none := newCheckpointFixture(t); none.runWith("", false, "checkpoint", "install", "git", "--repo", none.repository, "--yes").exit != usageExitCode {
		t.Fatal("install without a declared Checkpoint succeeded")
	}
}

func TestCheckpointInstallExtendsLefthookOnlyWhereTheHookIsFree(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush, configuration.CheckpointPreCommit)
	fixture.provideStandIn()
	existing := "pre-commit:\n  commands:\n    lint:\n      run: make lint\n"
	fixture.writeFile("lefthook.yml", existing)
	config := filepath.Join(fixture.repository, "lefthook.yml")

	result := fixture.install("--yes")
	want := "pre-push: add a review-party-checkpoint command to " + config + " (lefthook)\n" +
		"pre-commit: add by hand to " + config + " (lefthook)\n" +
		"  pre-commit:\n    commands:\n      review-party-checkpoint:\n        run: '" + guardedHookCommand(configuration.CheckpointPreCommit, "review-party checkpoint hook git pre-commit") + "'\n" +
		"Wrote 1 hook file(s).\n"
	assertRun(t, result, commandRun{stdout: want})
	appended := existing + "\npre-push:\n  commands:\n    review-party-checkpoint:\n      run: '" + guardedHookCommand(configuration.CheckpointPrePush, "review-party checkpoint hook git pre-push {1} {2}") + "'\n      use_stdin: true\n"
	if fixture.read("lefthook.yml") != appended {
		t.Fatalf("lefthook.yml = %q", fixture.read("lefthook.yml"))
	}
	if again := fixture.install("--yes"); !strings.HasPrefix(again.stdout, "pre-push: already installed in "+config+" (lefthook)\n") || strings.Contains(again.stdout, "Wrote") {
		t.Fatalf("second install = %+v", again)
	}
}

func TestCheckpointInstallPrintsThePreCommitFrameworkSteps(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	fixture.writeFile(".pre-commit-config.yaml", "repos: []\n")
	result := fixture.install("--yes")
	for _, line := range []string{"stages: [pre-push]", "language: system", "then run: pre-commit install --hook-type pre-push", `review-party checkpoint hook git pre-push "$PRE_COMMIT_REMOTE_NAME"`} {
		if !strings.Contains(result.stdout, line) {
			t.Fatalf("snippet lacks %q: %+v", line, result)
		}
	}
	if result.exit != 0 || strings.Contains(result.stdout, "Wrote") {
		t.Fatalf("pre-commit framework install = %+v", result)
	}
	if config := fixture.read(".pre-commit-config.yaml"); config != "repos: []\n" {
		t.Fatalf(".pre-commit-config.yaml = %q", config)
	}
}

func TestCheckpointInstallCarriesTheCallersConfigurationOnlyIntoPerCloneHooks(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPreCommit)
	fixture.provideStandIn()
	config := filepath.Join(t.TempDir(), "my config.json")
	assertRunContains(t, fixture.install("--yes", "--config", config), commandRun{stdout: "Wrote 1 hook file(s)."})
	if content, want := fixture.read(".git/hooks/pre-commit"), "#!/bin/sh\n"+checkpointHookBlock(configuration.CheckpointPreCommit, " --config '"+config+"'"); content != want {
		t.Fatalf("plain hook = %q, want %q", content, want)
	}

	shared := newHookInstallFixture(t, configuration.CheckpointPreCommit)
	shared.provideStandIn()
	shared.writeFile(".husky/_/h", "")
	result := shared.install("--yes", "--config", config)
	assertRunContains(t, result, commandRun{stdout: "warning: husky hooks are shared with the team, so they load each Caller's default configuration, not --config '" + config + "'\n"})
	if content, want := shared.read(".husky/pre-commit"), "#!/bin/sh\n"+checkpointHookBlock(configuration.CheckpointPreCommit, ""); content != want {
		t.Fatalf("husky hook = %q, want %q", content, want)
	}
}

func TestCheckpointInstallMakesAnExistingHookExecutable(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	fixture.writeFile(".git/hooks/pre-push", "#!/bin/sh\n# team hook\n")
	hook := filepath.Join(fixture.repository, ".git", "hooks", "pre-push")
	if err := os.Chmod(hook, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRunContains(t, fixture.install("--yes"), commandRun{stdout: "Wrote 1 hook file(s)."})
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o755 {
		t.Fatalf("hook mode = %v, want 0755", mode)
	}
}
