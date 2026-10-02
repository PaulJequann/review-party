package main

import (
	"errors"
	"fmt"
	"io/fs"
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
	assertRun(t, fixture.hook(refs, "pre-push", "--", "-origin", "url"), commandRun{exit: 1, stderr: refusal})

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
		"Wrote 1 file(s).\n"
	assertRun(t, installed, commandRun{stdout: want})
	content := fixture.read(".git/hooks/pre-push")
	if want := "#!/bin/sh\n" + checkpointHookBlock(configuration.CheckpointPrePush) + "# team hook\ncat > hook-saw\n"; content != want {
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
	edited := strings.Replace(content, "then exit 1; fi", "then exit 0; fi", 1)
	fixture.writeFile(".git/hooks/pre-push", edited)
	assertRun(t, fixture.install(), commandRun{stdout: "pre-push: edited review-party block left unchanged in " + hookPath + " (git hooks)\n"})
	if fixture.read(".git/hooks/pre-push") != edited {
		t.Fatal("an edited block was rewritten")
	}
}

func TestCheckpointInstallMakesAnEditedHookExecutableWithoutRewritingIt(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	assertRunContains(t, fixture.install("--yes"), commandRun{stdout: "Wrote 1 file(s)."})
	hook := filepath.Join(fixture.repository, ".git", "hooks", "pre-push")
	edited := strings.Replace(fixture.read(".git/hooks/pre-push"), "then exit 1; fi", "then exit 0; fi", 1)
	fixture.writeFile(".git/hooks/pre-push", edited)
	if err := os.Chmod(hook, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRun(t, fixture.install("--yes"), commandRun{stdout: "pre-push: make executable " + hook + " (git hooks)\nWrote 1 file(s).\n"})
	if info, err := os.Stat(hook); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("edited hook stat = %v, %v, want mode 0755", info, err)
	}
	if fixture.read(".git/hooks/pre-push") != edited {
		t.Fatal("making an edited hook executable rewrote its block")
	}
}

func TestCheckpointHookShellBlocksOnlyOnARefusal(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPreCommit)
	assertRunContains(t, fixture.install("--yes"), commandRun{stdout: "Wrote 1 file(s)."})
	hook := filepath.Join(fixture.repository, ".git", "hooks", "pre-commit")
	guarded := hookCommand{configuration.CheckpointPreCommit, "review-party checkpoint hook git pre-commit"}.guarded()
	warning := "review-party: warning: review-party exited 2; pre-commit Checkpoint not checked\n"
	for _, status := range []struct {
		code       int
		wantCode   int
		wantStderr string
	}{{0, 0, ""}, {1, 1, ""}, {2, 0, warning}} {
		script := fmt.Sprintf("#!/bin/sh\nexit %d\n", status.code)
		if err := os.WriteFile(filepath.Join(fixture.bin, "review-party"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		for form, command := range map[string]*exec.Cmd{"hook block": exec.Command(hook), "lefthook and pre-commit framework": exec.Command("sh", "-c", guarded)} {
			command.Dir = fixture.repository
			var stderr strings.Builder
			command.Stderr = &stderr
			var exit *exec.ExitError
			if err := command.Run(); err != nil && !errors.As(err, &exit) {
				t.Fatal(err)
			}
			if code := command.ProcessState.ExitCode(); code != status.wantCode || stderr.String() != status.wantStderr {
				t.Errorf("%s with review-party exiting %d = exit %d, stderr %q; want exit %d, stderr %q", form, status.code, code, stderr.String(), status.wantCode, status.wantStderr)
			}
		}
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
		{name: "husky", setup: activateHusky, hook: ".husky/pre-commit", tool: "husky"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHookInstallFixture(t, configuration.CheckpointPreCommit)
			fixture.provideStandIn()
			test.setup(fixture)
			result := fixture.install("--yes")
			want := "pre-commit: create " + filepath.Join(fixture.repository, test.hook) + " (" + test.tool + ")\nWrote 1 file(s).\n"
			assertRun(t, result, commandRun{stdout: want})
			info, err := os.Stat(filepath.Join(fixture.repository, test.hook))
			if err != nil {
				t.Fatal(err)
			}
			if mode := info.Mode().Perm(); mode != 0o755 {
				t.Fatalf("created hook mode = %v", mode)
			}
			if content, want := fixture.read(test.hook), "#!/bin/sh\n"+checkpointHookBlock(configuration.CheckpointPreCommit); content != want {
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

func TestCheckpointInstallPrintsTheLefthookSnippetWithoutEditingTheFile(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	existing := "pre-commit:\n  commands:\n    lint:\n      run: make lint\n"
	fixture.writeFile("lefthook.yml", existing)
	fixture.writeExecutable(".git/hooks/pre-push", "#!/bin/sh\ncall_lefthook run \"pre-push\" \"$@\"\n")
	config := filepath.Join(fixture.repository, "lefthook.yml")
	manual := "pre-push: add by hand to " + config + " (lefthook)\n  " + strings.ReplaceAll(strings.TrimSuffix(lefthookPrePushEntry(), "\n"), "\n", "\n  ") + "\n"

	assertRun(t, fixture.install("--yes"), commandRun{stdout: manual})
	if fixture.read("lefthook.yml") != existing {
		t.Fatalf("lefthook.yml = %q", fixture.read("lefthook.yml"))
	}
	fixture.writeFile("lefthook.yml", existing+"# "+strings.ReplaceAll(lefthookPrePushEntry(), "\n", "\n# "))
	assertRun(t, fixture.install("--yes"), commandRun{stdout: manual})
	fixture.writeFile("lefthook.yml", existing+lefthookPrePushEntry())
	assertRun(t, fixture.install("--yes"), commandRun{stdout: "pre-push: already installed in " + config + " (lefthook)\n"})
}

// lefthookPrePushEntry is the pre-push snippet install prints for lefthook.
func lefthookPrePushEntry() string {
	return "pre-push:\n  commands:\n    review-party-checkpoint:\n      run: |\n" +
		"        review_party_remote=$(cat <<'REVIEW_PARTY_REMOTE'\n        :{1}\n        REVIEW_PARTY_REMOTE\n        )\n" +
		"        " + hookCommand{configuration.CheckpointPrePush, `review-party checkpoint hook git pre-push -- "${review_party_remote#:}"`}.guarded() + "\n" +
		"      use_stdin: true\n"
}

func TestCheckpointLefthookSnippetPassesHookArgumentsLiterally(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.writeFile("lefthook.yml", "")
	snippet := fixture.install("--yes").stdout
	for _, remote := range []string{"it's $HOME; `true`", "REVIEW_PARTY_REMOTE", "/tmp/a b/remote.git"} {
		if got, want := fixture.runManagerCommand(snippet, "run: ", remote, "url"), "checkpoint\nhook\ngit\npre-push\n--\n"+remote+"\n"; got != want {
			t.Fatalf("hook arguments = %q, want %q", got, want)
		}
	}
}

func TestCheckpointInstallPrintsThePreCommitFrameworkSteps(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	fixture.writeFile(".pre-commit-config.yaml", "repos: []\n")
	activate := "  git does not run it in this clone until you run: pre-commit install --hook-type pre-push\n"
	result := fixture.install("--yes")
	for _, line := range []string{"stages: [pre-push]", "language: system", activate, `review-party checkpoint hook git pre-push -- "$PRE_COMMIT_REMOTE_NAME"`} {
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

	snippet := strings.TrimSuffix(strings.SplitN(result.stdout, "\n", 2)[1], activate)
	fixture.writeFile(".pre-commit-config.yaml", "repos:\n"+strings.ReplaceAll(snippet, "\n  ", "\n"))
	installed := "pre-push: already installed in " + filepath.Join(fixture.repository, ".pre-commit-config.yaml") + " (pre-commit framework)\n"
	assertRun(t, fixture.install("--yes"), commandRun{stdout: installed + activate})
	fixture.writeFile(".git/hooks/pre-push", "#!/usr/bin/env bash\n# File generated by pre-commit: https://pre-commit.com\n")
	assertRun(t, fixture.install("--yes"), commandRun{stdout: installed + activate})
	fixture.writeExecutable(".git/hooks/pre-push", "#!/usr/bin/env bash\n# File generated by pre-commit: https://pre-commit.com\n")
	assertRun(t, fixture.install("--yes"), commandRun{stdout: installed})
}

func TestCheckpointInstallNamesTheHuskyActivationStep(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPreCommit)
	fixture.provideStandIn()
	fixture.writeFile(".husky/pre-commit", "#!/bin/sh\n")
	hook := filepath.Join(fixture.repository, ".husky", "pre-commit")
	inserted := "pre-commit: add the review-party block to " + hook + " (husky)\n"
	if got := fixture.install().stdout; got != inserted+"  git does not run it in this clone until you run: npx husky\n" {
		t.Fatalf("inactive husky stdout = %q", got)
	}
	fixture.git("config", "core.hooksPath", ".husky")
	if got := fixture.install().stdout; got != inserted {
		t.Fatalf("husky at .husky stdout = %q", got)
	}
	fixture.git("config", "core.hooksPath", ".husky/_")
	if got := fixture.install().stdout; got != inserted+"  git does not run it in this clone until you run: npx husky\n" {
		t.Fatalf("husky 9 without its wrapper stdout = %q", got)
	}
	activateHusky(fixture)
	if got := fixture.install().stdout; got != inserted {
		t.Fatalf("husky 9 stdout = %q", got)
	}
}

func activateHusky(fixture hookInstallFixture) {
	for _, name := range configuration.CheckpointNames() {
		fixture.writeExecutable(".husky/_/"+string(name), "#!/usr/bin/env sh\n. \"${0%/*}/h\"\n")
	}
	fixture.git("config", "core.hooksPath", ".husky/_")
}

func TestCheckpointInstalledHookForwardsARemoteNamedLikeAnOption(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	assertRunContains(t, fixture.install("--yes"), commandRun{stdout: "Wrote 1 file(s)."})
	hook := shellQuoteArgument(filepath.Join(fixture.repository, ".git", "hooks", "pre-push"))
	if got, want := fixture.recordArguments(hook+" -origin url </dev/null"), "checkpoint\nhook\ngit\npre-push\n--\n-origin\nurl\n"; got != want {
		t.Fatalf("hook arguments = %q, want %q", got, want)
	}
}

func TestCheckpointInstallWarnsThatHooksLoadTheDefaultConfiguration(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPreCommit)
	fixture.provideStandIn()
	config := filepath.Join(t.TempDir(), "my config.json")
	warning := "warning: the hooks load each Caller's default Global Configuration, not --config '" + config + "'\n"
	assertRunContains(t, fixture.install("--yes", "--config", config), commandRun{stdout: warning})
}

// runManagerCommand runs the YAML command after key in text through sh, the
// way lefthook and the pre-commit framework would, and returns the arguments
// review-party received, one per line. The command is single-quoted or a
// literal block, and like lefthook it gets {1}, {2}, and so on replaced by
// gitArguments as they are, unquoted.
func (fixture hookInstallFixture) runManagerCommand(text, key string, gitArguments ...string) string {
	fixture.t.Helper()
	_, value, found := strings.Cut(text, key)
	if !found {
		fixture.t.Fatalf("no %q in %q", key, text)
	}
	quoted, block, _ := strings.Cut(value, "\n")
	command := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(quoted, "'"), "'"), "''", "'")
	if quoted == "|" {
		command = yamlLiteralBlock(block)
	}
	for index, argument := range gitArguments {
		command = strings.ReplaceAll(command, fmt.Sprintf("{%d}", index+1), argument)
	}
	return fixture.recordArguments(command)
}

// yamlLiteralBlock reads the lines of a literal block scalar, which keep the
// indentation of the first line, and returns them with it removed.
func yamlLiteralBlock(text string) string {
	lines := strings.Split(text, "\n")
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], " "))]
	var block strings.Builder
	for _, line := range lines {
		content, found := strings.CutPrefix(line, indent)
		if !found {
			break
		}
		block.WriteString(content + "\n")
	}
	return block.String()
}

func (fixture hookInstallFixture) writeExecutable(path, content string) {
	fixture.t.Helper()
	fixture.writeFile(path, content)
	if err := os.Chmod(filepath.Join(fixture.repository, path), 0o755); err != nil {
		fixture.t.Fatal(err)
	}
}

// recordArguments runs command through sh with a review-party stand-in on
// PATH and returns the arguments the stand-in received, one per line.
func (fixture hookInstallFixture) recordArguments(command string) string {
	fixture.t.Helper()
	arguments := filepath.Join(fixture.t.TempDir(), "arguments")
	standIn := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuoteArgument(arguments) + "\n"
	if err := os.WriteFile(filepath.Join(fixture.bin, "review-party"), []byte(standIn), 0o755); err != nil {
		fixture.t.Fatal(err)
	}
	if output, err := exec.Command("sh", "-c", command).CombinedOutput(); err != nil {
		fixture.t.Fatalf("run %q: %v: %s", command, err, output)
	}
	received, err := os.ReadFile(arguments)
	if err != nil {
		fixture.t.Fatal(err)
	}
	return string(received)
}

func TestCheckpointInstallMakesAnExistingHookExecutable(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	fixture.writeFile(".git/hooks/pre-push", "#!/bin/sh\n# team hook\n")
	hook := filepath.Join(fixture.repository, ".git", "hooks", "pre-push")
	if err := os.Chmod(hook, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRunContains(t, fixture.install("--yes"), commandRun{stdout: "Wrote 1 file(s)."})
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o755 {
		t.Fatalf("hook mode = %v, want 0755", mode)
	}

	installed := fixture.read(".git/hooks/pre-push")
	if err := os.Chmod(hook, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRun(t, fixture.install("--yes"), commandRun{stdout: "pre-push: make executable " + hook + " (git hooks)\nWrote 1 file(s).\n"})
	if info, err = os.Stat(hook); err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o755 {
		t.Fatalf("reinstalled hook mode = %v, want 0755", mode)
	}
	if got := fixture.read(".git/hooks/pre-push"); got != installed {
		t.Fatalf("reinstalled hook content = %q, want %q", got, installed)
	}
}

func TestCheckpointInstallEditsASymlinkedHookAtItsTarget(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.writeFile("scripts/pre-push", "#!/bin/sh\n# team hook\n")
	fixture.installThroughLink()
}

func TestCheckpointInstallWritesADanglingHookSymlinkAtItsTarget(t *testing.T) {
	newHookInstallFixture(t, configuration.CheckpointPrePush).installThroughLink()
}

// installThroughLink installs the pre-push hook through a .git/hooks symlink
// to scripts/pre-push and checks that the link survives and its target, the
// only file in scripts, holds the block.
func (fixture hookInstallFixture) installThroughLink() {
	t := fixture.t
	t.Helper()
	fixture.provideStandIn()
	hook := filepath.Join(fixture.repository, ".git", "hooks", "pre-push")
	if err := os.Symlink(filepath.Join("..", "..", "scripts", "pre-push"), hook); err != nil {
		t.Fatal(err)
	}
	assertRunContains(t, fixture.install("--yes"), commandRun{stdout: "Wrote 1 file(s)."})
	if info, err := os.Lstat(hook); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("hook is no longer a symlink: %v %v", info, err)
	}
	if script := fixture.read("scripts/pre-push"); !strings.Contains(script, checkpointHookBlock(configuration.CheckpointPrePush)) {
		t.Fatalf("linked script was not edited:\n%s", script)
	}
	entries, err := os.ReadDir(filepath.Join(fixture.repository, "scripts"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("scripts directory = %v %v, want only pre-push", entries, err)
	}
}

func TestCheckpointInstallLeavesAHookForAnotherInterpreterToTheCaller(t *testing.T) {
	for _, test := range []struct {
		checkpoint configuration.CheckpointName
		original   string
		edited     bool
	}{
		{configuration.CheckpointPreCommit, "#!/usr/bin/env python3\nrun_checks()\n", false},
		{configuration.CheckpointPreCommit, "#!/usr/bin/ruby -w\nrun_checks()\n", false},
		{configuration.CheckpointPreCommit, "#!/usr/bin/env -S bash -e\nrun_checks()\n", true},
		{configuration.CheckpointPreCommit, "#!/bin/zsh\nrun_checks()\n", true},
		{configuration.CheckpointPrePush, "\x7fELF\x02\x01\x01\x00\x00\x00", false},
	} {
		t.Run(test.original, func(t *testing.T) {
			fixture := newHookInstallFixture(t, test.checkpoint)
			fixture.provideStandIn()
			hook := ".git/hooks/" + string(test.checkpoint)
			fixture.writeFile(hook, test.original)
			result := fixture.install("--yes")
			if edited := fixture.read(hook) != test.original; edited != test.edited {
				t.Fatalf("edited = %v, want %v: %+v", edited, test.edited, result)
			}
			command := map[configuration.CheckpointName]string{
				configuration.CheckpointPreCommit: "review-party checkpoint hook git pre-commit",
				configuration.CheckpointPrePush:   "review-party checkpoint hook git pre-push -- <remote> <url>",
			}[test.checkpoint]
			if !test.edited {
				assertRun(t, result, commandRun{stdout: string(test.checkpoint) + ": add by hand to " + filepath.Join(fixture.repository, hook) + " (git hooks)\n" +
					"  # This hook is not a shell script. Run the command below from it with the\n" +
					"  # hook's arguments in place of any <placeholders>, and its standard input.\n" +
					"  # Stop only when it exits 1; any other status means the Checkpoint was not\n" +
					"  # checked, so warn and continue.\n" +
					"  " + command + "\n"})
			}
		})
	}
}
