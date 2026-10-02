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
