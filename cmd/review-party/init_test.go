package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"reviewparty/internal/configuration"
)

type initFixture struct {
	manager    *configuration.Manager
	repository string
}

func newInitFixture(t *testing.T) initFixture {
	t.Helper()
	isolateProfileCommandEnvironment(t)
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runProfileTestCommand(t, exec.Command("git", "-C", repository, "init", "--quiet"))
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	return initFixture{manager: manager, repository: repository}
}

func (fixture initFixture) profile(t *testing.T, scope configuration.Scope, name string) {
	t.Helper()
	plan, err := fixture.manager.PlanProfileCreation(configuration.Repository(fixture.repository), configuration.ProfileDraft{
		Target: scope, Name: name, Reviewer: "codex", Model: "luna",
		ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func (fixture initFixture) run(t *testing.T, streams commandIO, arguments ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	streams.output, streams.errors = &stdout, &stderr
	if streams.input == nil {
		streams.input = strings.NewReader("")
	}
	streams.configurationManager = func(string) *configuration.Manager { return fixture.manager }
	exit := execute(context.Background(), append([]string{"init", "--repo", fixture.repository}, arguments...), streams)
	return exit, stdout.String(), stderr.String()
}

func (fixture initFixture) declareCheckpoints(t *testing.T, names ...configuration.CheckpointName) {
	t.Helper()
	intents := make([]configuration.Intent, 0, len(names))
	for _, name := range names {
		checkpoint := configuration.NewCheckpoint()
		checkpoint.Integrations = []configuration.IntegrationName{configuration.IntegrationGit}
		intents = append(intents, configuration.SetCheckpoint{Name: name, Checkpoint: checkpoint})
	}
	plan, err := fixture.manager.Plan(configuration.Repository(fixture.repository), intents)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func (fixture initFixture) installHooks(t *testing.T, integration configuration.IntegrationName) {
	t.Helper()
	var output bytes.Buffer
	streams := commandIO{input: strings.NewReader(""), output: &output, errors: &output, configurationManager: func(string) *configuration.Manager { return fixture.manager }}
	if exit := execute(context.Background(), []string{"checkpoint", "install", string(integration), "--repo", fixture.repository, "--yes"}, streams); exit != 0 {
		t.Fatalf("install exit = %d, output = %q", exit, output.String())
	}
}

func (fixture initFixture) selection(t *testing.T) (configuration.ReviewSelection, bool) {
	t.Helper()
	selection, value, err := fixture.manager.EffectiveReviewSelection(configuration.Repository(fixture.repository))
	if err != nil {
		t.Fatal(err)
	}
	return selection, value.Authored
}

func (fixture initFixture) repositoryConfiguration(t *testing.T) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(fixture.repository, ".reviewparty", "config.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return content
}

func TestInitSetupFlagsAddEachNameToItsResolvedScopeAndRepeatAsANoOp(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "bugs")
	fixture.profile(t, configuration.ScopeGlobal, "docs")
	fixture.profile(t, configuration.ScopeRepository, "docs")

	exit, _, stderr := fixture.run(t, commandIO{}, "--profile", "bugs", "--profile", "docs", "--yes")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	selection, _ := fixture.selection(t)
	if !slices.Equal(selection.Global, []configuration.SelectionItem{{Profile: "bugs"}}) ||
		!slices.Equal(selection.Repository, []configuration.SelectionItem{{Profile: "docs"}}) {
		t.Fatalf("selection = %#v, want Global bugs and Repository docs", selection)
	}

	written := fixture.repositoryConfiguration(t)
	exit, stdout, stderr := fixture.run(t, commandIO{}, "--profile", "bugs", "--profile", "docs")
	if exit != 0 {
		t.Fatalf("rerun exit = %d, stderr = %q", exit, stderr)
	}
	if stdout != "The Review selection already includes every name given; nothing was written.\n" {
		t.Fatalf("rerun stdout = %q", stdout)
	}
	if !bytes.Equal(fixture.repositoryConfiguration(t), written) {
		t.Fatal("rerun rewrote Repository Configuration")
	}
}

func TestInitSetupFlagsRequireYesWithoutATerminal(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "bugs")

	exit, _, stderr := fixture.run(t, commandIO{}, "--profile", "bugs")
	if exit == 0 || !strings.Contains(stderr, "requires --yes") {
		t.Fatalf("exit = %d, stderr = %q, want a --yes refusal", exit, stderr)
	}
	if _, authored := fixture.selection(t); authored {
		t.Fatal("refused setup wrote a Review selection")
	}
}

func TestInitWithoutATerminalPrintsOnlyTheCommandsThatFillEachGap(t *testing.T) {
	fixture := newInitFixture(t)
	add := "Missing Review selection: review-party init --repo " + fixture.repository + " --profile <name>"
	exit, stdout, stderr := fixture.run(t, commandIO{})
	want := "Missing Review Profile: review-party config profile create <name> --scope global --template <template> --reviewer <reviewer> --model <model> --effort <effort> --deadline <deadline>\n" + add + "\n"
	if exit != 0 || stdout != want {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q, want %q", exit, stdout, stderr, want)
	}

	fixture.profile(t, configuration.ScopeGlobal, "bugs")
	_, stdout, _ = fixture.run(t, commandIO{})
	if want := add + " (Profiles: bugs)\n"; stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	if written := fixture.repositoryConfiguration(t); written != nil {
		t.Fatalf("report-only init wrote Repository Configuration: %s", written)
	}
}

func TestInitWithoutATerminalNamesEachDeclaredDefinitionThisCallerLacks(t *testing.T) {
	author := newInitFixture(t)
	author.profile(t, configuration.ScopeGlobal, "docs")
	if exit, _, stderr := author.run(t, commandIO{}, "--profile", "docs", "--yes"); exit != 0 {
		t.Fatalf("author setup exit = %d, stderr = %q", exit, stderr)
	}
	teammate := initFixture{repository: author.repository, manager: configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}, ValidateName: func(string) error { return nil },
	})}

	_, stdout, _ := teammate.run(t, commandIO{})
	want := "Missing Global Profile docs: review-party config profile create docs --scope global --template <template> --reviewer <reviewer> --model <model> --effort <effort> --deadline <deadline>\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	_, stdout, _ = author.run(t, commandIO{})
	if want := "Repository is ready: review-party run --repo " + author.repository + "\n"; stdout != want {
		t.Fatalf("author stdout = %q, want %q", stdout, want)
	}
}

func TestInitInATerminalRunsTheFirstUseJourney(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "bugs")
	streams := commandIO{
		input:    iotest.OneByteReader(strings.NewReader("1\ny\n")),
		terminal: func(any) bool { return true },
	}

	exit, stdout, stderr := fixture.run(t, streams, "--accessible")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q\n%s", exit, stderr, stdout)
	}
	selection, _ := fixture.selection(t)
	if !slices.Equal(selection.Global, []configuration.SelectionItem{{Profile: "bugs"}}) {
		t.Fatalf("selection = %#v, want the chosen Global Profile", selection)
	}
	if !strings.HasSuffix(stdout, "Repository is ready: review-party run --repo "+fixture.repository+"\n") {
		t.Fatalf("journey did not end with the remaining report:\n%s", stdout)
	}
}

func TestInitReportLeavesAHookOutsideTheTeamFloorToTheCaller(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "docs")
	if exit, _, stderr := fixture.run(t, commandIO{}, "--profile", "docs", "--yes"); exit != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", exit, stderr)
	}
	plan, err := fixture.manager.Plan(configuration.Repository(fixture.repository), []configuration.Intent{
		configuration.SetCheckpoint{Name: configuration.CheckpointPrePush, Checkpoint: configuration.NewCheckpoint()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ := fixture.run(t, commandIO{})
	if want := "Repository is ready: review-party run --repo " + fixture.repository + "\n"; stdout != want {
		t.Fatalf("stdout = %q, want only %q", stdout, want)
	}
}

func TestInitWithoutATerminalNamesEachCheckpointWithoutItsGitHook(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "docs")
	if exit, _, stderr := fixture.run(t, commandIO{}, "--profile", "docs", "--yes"); exit != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", exit, stderr)
	}
	fixture.declareCheckpoints(t, configuration.CheckpointPrePush, configuration.CheckpointPreCommit)
	ready := "Repository is ready: review-party run --repo " + fixture.repository + "\n"
	install := "review-party checkpoint install git --repo " + fixture.repository + "\n"

	_, stdout, _ := fixture.run(t, commandIO{})
	if want := ready + "Checkpoint pre-push has no git hook: " + install + "Checkpoint pre-commit has no git hook: " + install; stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}

	fixture.installHooks(t, configuration.IntegrationGit)
	if _, stdout, _ = fixture.run(t, commandIO{}); stdout != ready {
		t.Fatalf("installed stdout = %q, want %q", stdout, ready)
	}

	hook := filepath.Join(fixture.repository, ".git", "hooks", "pre-push")
	if err := os.Chmod(hook, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stdout, _ = fixture.run(t, commandIO{}); stdout != ready+"Checkpoint pre-push git hook "+hook+" is not executable, so git skips it: "+install {
		t.Fatalf("non-executable stdout = %q", stdout)
	}

	lefthook := filepath.Join(fixture.repository, "lefthook.yml")
	if err := os.WriteFile(lefthook, []byte("pre-push:\n  commands: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ = fixture.run(t, commandIO{})
	want := ready + "Checkpoint pre-push has no git hook; add to " + lefthook + " (lefthook) by hand:\n" +
		"  " + strings.ReplaceAll(strings.TrimSuffix(lefthookPrePushEntry(), "\n"), "\n", "\n  ") + "\n" +
		"Checkpoint pre-push git hook does not run until lefthook is active in this clone: lefthook install\n" +
		"Checkpoint pre-commit has no git hook; add to " + lefthook + " (lefthook) by hand:\n" +
		"  pre-commit:\n    commands:\n      review-party-checkpoint:\n        run: '" + hookCommand{configuration.CheckpointPreCommit, "review-party checkpoint hook git pre-commit"}.guarded() + "'\n" +
		"Checkpoint pre-commit git hook does not run until lefthook is active in this clone: lefthook install\n"
	if stdout != want {
		t.Fatalf("lefthook stdout = %q, want %q", stdout, want)
	}
}

func TestInitWithoutATerminalNamesTheHuskyActivationStep(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "docs")
	if exit, _, stderr := fixture.run(t, commandIO{}, "--profile", "docs", "--yes"); exit != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", exit, stderr)
	}
	fixture.declareCheckpoints(t, configuration.CheckpointPrePush)
	wrapper := filepath.Join(fixture.repository, ".husky", "_", "pre-push")
	if err := os.MkdirAll(filepath.Dir(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.installHooks(t, configuration.IntegrationGit)
	ready := "Repository is ready: review-party run --repo " + fixture.repository + "\n"
	if _, stdout, _ := fixture.run(t, commandIO{}); stdout != ready+"Checkpoint pre-push git hook does not run until husky is active in this clone: npx husky\n" {
		t.Fatalf("inactive husky stdout = %q", stdout)
	}
	if output, err := exec.Command("git", "-C", fixture.repository, "config", "core.hooksPath", ".husky/_").CombinedOutput(); err != nil {
		t.Fatalf("activate husky: %v: %s", err, output)
	}
	if _, stdout, _ := fixture.run(t, commandIO{}); stdout != ready+"Checkpoint pre-push git hook does not run until husky is active in this clone: npx husky\n" {
		t.Fatalf("husky without its wrapper stdout = %q", stdout)
	}
	if err := os.WriteFile(wrapper, []byte("#!/usr/bin/env sh\n. \"${0%/*}/h\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, stdout, _ := fixture.run(t, commandIO{}); stdout != ready {
		t.Fatalf("active husky stdout = %q, want %q", stdout, ready)
	}
}

func TestInitReportHintsCarryAnAbsoluteConfiguration(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "docs")
	if exit, _, stderr := fixture.run(t, commandIO{}, "--profile", "docs", "--yes"); exit != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", exit, stderr)
	}
	absolute, err := filepath.Abs("relative.json")
	if err != nil {
		t.Fatal(err)
	}
	want := "Repository is ready: review-party run --repo " + fixture.repository + configurationArgument(absolute) + "\n"
	if _, stdout, _ := fixture.run(t, commandIO{}, "--config", "relative.json"); stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestInitWithoutATerminalNamesMissingAgentFloorHooks(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "docs")
	if exit, _, stderr := fixture.run(t, commandIO{}, "--profile", "docs", "--yes"); exit != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", exit, stderr)
	}
	agents, git := configuration.NewCheckpoint(), configuration.NewCheckpoint()
	agents.Integrations = []configuration.IntegrationName{configuration.IntegrationClaudeCode, configuration.IntegrationCodex}
	git.Integrations = []configuration.IntegrationName{configuration.IntegrationGit}
	plan, err := fixture.manager.Plan(configuration.Repository(fixture.repository), []configuration.Intent{
		configuration.SetCheckpoint{Name: configuration.CheckpointPrePush, Checkpoint: agents},
		configuration.SetCheckpoint{Name: configuration.CheckpointPreCommit, Checkpoint: git},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	ready := "Repository is ready: review-party run --repo " + fixture.repository + "\n"
	gitGap := "Checkpoint pre-commit has no git hook: review-party checkpoint install git --repo " + fixture.repository + "\n"

	_, stdout, _ := fixture.run(t, commandIO{})
	want := ready + gitGap +
		"Checkpoint pre-push has no claude-code hook: review-party checkpoint install claude-code --repo " + fixture.repository + "\n" +
		"Checkpoint pre-push has no codex hook: review-party checkpoint install codex --repo " + fixture.repository + "\n" +
		codexApprovalStep + "\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}

	fixture.installHooks(t, configuration.IntegrationClaudeCode)
	fixture.installHooks(t, configuration.IntegrationCodex)
	if _, stdout, _ = fixture.run(t, commandIO{}); stdout != ready+gitGap+codexApprovalStep+"\n" {
		t.Fatalf("installed stdout = %q", stdout)
	}
}

func TestInitInATerminalDeclaresACheckpointAndInstallsItsHook(t *testing.T) {
	fixture := newInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "bugs")
	pathWithOnlyGit(t)
	streams := commandIO{
		// bugs, publish, pre-push, no exemptions, 0 lines, human waivers,
		// the git floor, publish, install, no personal agent hooks
		input:    iotest.OneByteReader(strings.NewReader("1\ny\n1\n\n0\n1\n0\ny\ny\n0\n")),
		terminal: func(any) bool { return true },
	}

	exit, stdout, stderr := fixture.run(t, streams, "--accessible")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q\n%s", exit, stderr, stdout)
	}
	hook, err := os.ReadFile(filepath.Join(fixture.repository, ".git", "hooks", "pre-push"))
	if err != nil || !strings.Contains(string(hook), "review-party checkpoint hook git pre-push") {
		t.Fatalf("pre-push hook = %q, %v\n%s", hook, err, stdout)
	}
	if !strings.HasSuffix(stdout, "Repository is ready: review-party run --repo "+fixture.repository+"\n") {
		t.Fatalf("an installed hook still reported missing:\n%s", stdout)
	}
}

// pathWithOnlyGit sets PATH to a directory holding git alone, so init finds
// no Caller Agent to preselect.
func pathWithOnlyGit(t *testing.T) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Symlink(git, filepath.Join(directory, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
}
