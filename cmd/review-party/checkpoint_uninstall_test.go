package main

import (
	"bytes"
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"reviewparty/internal/configuration"
)

type uninstallFixture struct {
	hookInstallFixture
	codexHome string
}

func newUninstallFixture(t *testing.T) uninstallFixture {
	t.Helper()
	fixture := uninstallFixture{hookInstallFixture: newHookInstallFixture(t, configuration.CheckpointPrePush, configuration.CheckpointPreCommit), codexHome: t.TempDir()}
	t.Setenv("CODEX_HOME", fixture.codexHome)
	t.Setenv("HOME", t.TempDir())
	return fixture
}

func (fixture uninstallFixture) install(installs ...[]string) {
	fixture.t.Helper()
	for _, install := range installs {
		arguments := append([]string{"checkpoint", "install"}, install...)
		if result := fixture.runWith("", false, append(arguments, "--repo", fixture.repository, "--yes")...); result.exit != 0 {
			fixture.t.Fatalf("install %v = %+v", install, result)
		}
	}
}

func (fixture uninstallFixture) uninstall(input string, terminal bool, arguments ...string) commandRun {
	fixture.t.Helper()
	return fixture.runWith(input, terminal, append([]string{"checkpoint", "uninstall", "--repo", fixture.repository}, arguments...)...)
}

func (fixture uninstallFixture) path(relative string) string {
	return filepath.Join(fixture.repository, relative)
}

func (fixture uninstallFixture) tree() map[string]string {
	fixture.t.Helper()
	git := fixture.path(".git")
	entries := map[string]string{}
	for _, root := range []string{fixture.repository, filepath.Join(git, "hooks"), fixture.codexHome} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case path == git:
				return filepath.SkipDir
			case entry.IsDir():
				entries[path] = "directory"
				return nil
			}
			entries[path], err = fileState(path)
			return err
		})
		if err != nil {
			fixture.t.Fatal(err)
		}
	}
	record := filepath.Join(git, installRecordName)
	if state, err := fileState(record); err == nil {
		entries[record] = state
	}
	return entries
}

func fileState(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	return info.Mode().Perm().String() + "\n" + string(content), err
}

func assertTree(t *testing.T, got, want map[string]string) {
	t.Helper()
	paths := maps.Clone(got)
	maps.Copy(paths, want)
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if got[path] != want[path] {
			t.Errorf("%s =\n%q\nwant\n%q", path, got[path], want[path])
		}
	}
}

const teamSettings = `{
  "permissions": {"allow": ["Bash(go test *)"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Edit", "hooks": [{"type": "command", "command": "./lint.sh"}]}
    ]
  },
  "model": "opus"
}
`

func TestUninstallRestoresEveryFileInstallChanged(t *testing.T) {
	git, agentsMD := []string{"git"}, []string{"agents-md"}
	claude, codex := []string{"claude-code"}, []string{"codex"}
	for _, test := range []struct {
		name      string
		setup     func(uninstallFixture)
		installs  [][]string
		uninstall []string
	}{
		{name: "plain hooks beside the team's own", installs: [][]string{git}, setup: func(fixture uninstallFixture) {
			fixture.writeExecutable(".git/hooks/pre-push", "#!/bin/sh\necho team\n")
		}},
		{name: "a hook that held only a shebang", installs: [][]string{git}, setup: func(fixture uninstallFixture) {
			fixture.writeExecutable(".git/hooks/pre-commit", "#!/bin/sh\n")
		}},
		{name: "husky", installs: [][]string{git}, setup: func(fixture uninstallFixture) {
			fixture.writeExecutable(".husky/pre-push", "#!/usr/bin/env sh\nnpm test\n")
		}},
		{name: "core.hooksPath to a missing directory", installs: [][]string{git}, setup: func(fixture uninstallFixture) {
			fixture.git("config", "core.hooksPath", "tools/git-hooks")
		}},
		{name: "Claude Code team and personal settings", installs: [][]string{claude, {"claude-code", "--personal"}}, setup: func(fixture uninstallFixture) {
			fixture.writeFile(".claude/settings.json", teamSettings)
		}},
		{name: "settings files that held only braces", installs: [][]string{claude, codex}, setup: func(fixture uninstallFixture) {
			fixture.writeFile(".claude/settings.json", "{}\n")
			fixture.writeFile(".codex/hooks.json", "{}")
		}},
		{name: "a new Codex team file and its directory", installs: [][]string{codex}},
		{name: "the shared Codex file", installs: [][]string{{"codex", "--personal"}}, uninstall: []string{"--shared"}, setup: func(fixture uninstallFixture) {
			if err := os.WriteFile(filepath.Join(fixture.codexHome, "hooks.json"), []byte(teamSettings), 0o600); err != nil {
				fixture.t.Fatal(err)
			}
		}},
		{name: "agents-md in AGENTS.md", installs: [][]string{agentsMD}, setup: func(fixture uninstallFixture) {
			fixture.writeFile("AGENTS.md", "# Rules\n\nBe kind.\n")
		}},
		{name: "agents-md in CLAUDE.md", installs: [][]string{agentsMD}, setup: func(fixture uninstallFixture) {
			fixture.writeFile("CLAUDE.md", "# Claude\n")
		}},
		{name: "a new AGENTS.md", installs: [][]string{agentsMD}},
		{name: "every Integration at once", installs: [][]string{git, claude, codex, agentsMD}, setup: func(fixture uninstallFixture) {
			fixture.writeExecutable(".git/hooks/pre-push", "#!/bin/sh\necho team\n")
			fixture.writeFile(".claude/settings.json", teamSettings)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newUninstallFixture(t)
			if test.setup != nil {
				test.setup(fixture)
			}
			before := fixture.tree()
			fixture.install(test.installs...)
			if maps.Equal(fixture.tree(), before) {
				t.Fatal("install changed nothing")
			}

			if result := fixture.uninstall("", false, append(test.uninstall, "--yes")...); result.exit != 0 || result.stderr != "" {
				t.Fatalf("uninstall = %+v", result)
			}
			assertTree(t, fixture.tree(), before)
			assertRun(t, fixture.uninstall("", false, test.uninstall...), commandRun{stdout: "Nothing to remove.\n"})
		})
	}
}

func TestUninstallNamesEachRemovalAndAsksFirst(t *testing.T) {
	fixture := newUninstallFixture(t)
	fixture.writeExecutable(".git/hooks/pre-push", "#!/bin/sh\necho team\n")
	before := fixture.tree()
	fixture.install([]string{"git"})
	installed := fixture.tree()
	plan := "git pre-push: remove the review-party block from " + fixture.path(".git/hooks/pre-push") + "\n" +
		"git pre-commit: delete the file install created at " + fixture.path(".git/hooks/pre-commit") + "\n"

	assertRun(t, fixture.uninstall("", false), commandRun{exit: 2, stdout: plan, stderr: "review-party: rerun with --yes to remove them without a terminal\n"})
	assertRun(t, fixture.uninstall("n\n", true), commandRun{exit: 1, stdout: plan + "Remove these? [y/N] ", stderr: "review-party: nothing removed\n"})
	assertTree(t, fixture.tree(), installed)
	assertRun(t, fixture.uninstall("y\n", true), commandRun{stdout: plan + "Remove these? [y/N] Updated 1 file(s); deleted 1 file(s).\n"})
	assertTree(t, fixture.tree(), before)
}

func TestUninstallLeavesWhatItDidNotWriteAndSaysSo(t *testing.T) {
	fixture := newUninstallFixture(t)
	fixture.writeExecutable(".git/hooks/pre-push", "#!/bin/sh\necho team\n")
	fixture.install([]string{"git"}, []string{"claude-code"})
	hook := strings.Replace(fixture.read(".git/hooks/pre-push"), "review_party_status=0\n", "review_party_status=0 # mine\n", 1)
	fixture.writeExecutable(".git/hooks/pre-push", hook)
	settings := strings.Replace(fixture.read(".claude/settings.json"), "|| true", "|| exit 0", 1)
	fixture.writeFile(".claude/settings.json", settings)
	fixture.writeFile("lefthook.yml", "# review-party checkpoint hook git pre-push\n"+lefthookCommand(configuration.CheckpointPrePush))
	fixture.writeFile("AGENTS.md", "# Rules\n"+agentsMDBegin+"\n"+agentsMDBegin+"\n"+agentsMDEnd+"\n")
	fixture.writeFile(".codex/hooks.json", `{"hooks": "review-party checkpoint hook codex"`)
	fixture.writeFile(".claude/settings.local.json", `{"hooks": `)

	left := "git pre-push: edited review-party block left in " + fixture.path(".git/hooks/pre-push") + "\n" +
		"git pre-push: remove by hand from " + fixture.path("lefthook.yml") + "\n" +
		"  " + hookCommand{configuration.CheckpointPrePush, `review-party checkpoint hook git pre-push -- "${review_party_remote#:}"`}.guarded() + "\n"
	agents := "claude-code: edited review-party entry left in " + fixture.path(".claude/settings.json") + "\n" +
		"codex: remove by hand from " + fixture.path(".codex/hooks.json") + "\n" +
		"  review-party cannot read the file: the document is not a JSON object: unexpected end of JSON input\n" +
		"agents-md: fix the review-party markers by hand in " + fixture.path("AGENTS.md") + "\n" +
		"  " + agentsMDSequence("bbe").problem() + "\n" +
		"  Delete the marker lines and the review-party lines between them, then rerun the uninstall.\n"
	incomplete := "review-party: some review-party hooks or blocks are left; remove them as shown above\n"
	removed := "git pre-commit: delete the file install created at " + fixture.path(".git/hooks/pre-commit") + "\n"
	assertRun(t, fixture.uninstall("", false, "--yes"), commandRun{exit: 1, stdout: left + removed + agents + "Updated 0 file(s); deleted 1 file(s).\n", stderr: incomplete})
	if fixture.read(".git/hooks/pre-push") != hook || fixture.read(".claude/settings.json") != settings {
		t.Fatal("uninstall changed an edited block or entry")
	}
	if _, err := os.Stat(fixture.path(".git/hooks/pre-commit")); err == nil {
		t.Fatal("uninstall kept the hook install created")
	}
	assertRun(t, fixture.uninstall("", false), commandRun{exit: 1, stdout: left + agents, stderr: incomplete})
}

func TestUninstallDeletesOnlyFilesInstallCreatedAndLeftEmpty(t *testing.T) {
	fixture := newUninstallFixture(t)
	created, prePush := fixture.path(".git/hooks/pre-commit"), fixture.path(".git/hooks/pre-push")
	fixture.install([]string{"git"})
	fixture.writeExecutable(".git/hooks/pre-commit", fixture.read(".git/hooks/pre-commit")+"echo mine\n")
	assertRun(t, fixture.uninstall("", false, "--yes"), commandRun{stdout: "git pre-push: delete the file install created at " + prePush + "\n" +
		"git pre-commit: remove the review-party block from " + created + "\nUpdated 1 file(s); deleted 1 file(s).\n"})
	if got := fixture.read(".git/hooks/pre-commit"); got != "#!/bin/sh\necho mine\n" {
		t.Fatalf("pre-commit = %q", got)
	}
	if _, err := os.Stat(fixture.path(".git/" + installRecordName)); err == nil {
		t.Fatal("the install record outlived the files it listed")
	}

	if err := os.Remove(created); err != nil {
		t.Fatal(err)
	}
	fixture.install([]string{"git"})
	fixture.writeExecutable(".git/hooks/pre-commit", "#!/bin/sh\n")
	assertRun(t, fixture.uninstall("", false, "--yes"), commandRun{stdout: "git pre-push: delete the file install created at " + prePush + "\n" +
		"git pre-commit: delete the file install created at " + created + "\nUpdated 0 file(s); deleted 2 file(s).\n"})

	fixture.install([]string{"git"})
	if err := os.Remove(fixture.path(".git/" + installRecordName)); err != nil {
		t.Fatal(err)
	}
	assertRun(t, fixture.uninstall("", false, "--yes"), commandRun{stdout: "git pre-push: remove the review-party block from " + prePush + "\n" +
		"git pre-commit: remove the review-party block from " + created + "\n" +
		"note: " + prePush + " will hold only what install puts in a new file; delete it if nothing else uses it\n" +
		"note: " + created + " will hold only what install puts in a new file; delete it if nothing else uses it\n" +
		"Updated 2 file(s); deleted 0 file(s).\n"})
	if got := fixture.read(".git/hooks/pre-commit"); got != "#!/bin/sh\n" {
		t.Fatalf("an unrecorded pre-commit = %q, want it kept", got)
	}
}

func TestUninstallUndeclaredRemovesOnlyWhatNoDeclaredCheckpointUses(t *testing.T) {
	fixture := newUninstallFixture(t)
	fixture.writeFile(".claude/settings.json", teamSettings)
	before := fixture.tree()
	fixture.install([]string{"git"}, []string{"claude-code"})
	command := "review-party checkpoint uninstall --undeclared --repo " + shellWord(fixture.repository)

	removal := fixture.runWith("", false, "config", "checkpoint", "remove", "pre-commit", "--repo", fixture.repository, "--yes")
	assertRunContains(t, removal, commandRun{stdout: "\nInstalled Checkpoint Integrations serve no declared Checkpoint; remove them with: " + command + "\n"})
	assertRunContains(t, fixture.doctor(), commandRun{stdout: "\ngit pre-commit Integration in " + fixture.path(".git/hooks/pre-commit") + " serves no declared Checkpoint; fix: " + command + "\n"})
	want := []undeclaredIntegration{{Integration: "git", Checkpoint: "pre-commit", Path: fixture.path(".git/hooks/pre-commit"), Fix: command}}
	if got := decodeDoctor(t, fixture.doctor("--format", "json"), 0).Undeclared; !slices.Equal(got, want) {
		t.Fatalf("json doctor undeclared = %+v, want %+v", got, want)
	}
	assertRun(t, fixture.uninstall("", false, "--undeclared", "--yes"), commandRun{stdout: "git pre-commit: delete the file install created at " + fixture.path(".git/hooks/pre-commit") + "\nUpdated 0 file(s); deleted 1 file(s).\n"})
	if !strings.Contains(fixture.read(".git/hooks/pre-push"), checkpointHookBlock(configuration.CheckpointPrePush)) || !strings.Contains(fixture.read(".claude/settings.json"), "review-party checkpoint hook claude-code") {
		t.Fatal("uninstall --undeclared removed what the declared pre-push Checkpoint uses")
	}
	if strings.Contains(fixture.doctor().stdout, "serves no declared Checkpoint") {
		t.Fatal("doctor still flags Integrations after uninstall --undeclared")
	}

	fixture.runWith("", false, "config", "checkpoint", "remove", "pre-push", "--repo", fixture.repository, "--yes")
	assertRun(t, fixture.uninstall("", false, "--undeclared", "--yes"), commandRun{stdout: "git pre-push: delete the file install created at " + fixture.path(".git/hooks/pre-push") + "\n" +
		"claude-code: remove the review-party PreToolUse entry from " + fixture.path(".claude/settings.json") + "\nUpdated 1 file(s); deleted 1 file(s).\n"})
	after, config := fixture.tree(), fixture.path(".reviewparty/config.json")
	delete(after, config)
	delete(before, config)
	assertTree(t, after, before)
}

func TestConfigCheckpointRemoveOffersToUninstallInATerminal(t *testing.T) {
	fixture := newUninstallFixture(t)
	fixture.install([]string{"git"})
	hook := fixture.path(".git/hooks/pre-commit")
	offer := "These installed Checkpoint Integrations serve no declared Checkpoint:\ngit pre-commit: delete the file install created at " + hook + "\nRemove them? [y/N] "

	declined := fixture.runWith("y\nn\n", true, "config", "checkpoint", "remove", "pre-commit", "--repo", fixture.repository)
	assertRunContains(t, declined, commandRun{stdout: offer + "Left in place; remove them later with: review-party checkpoint uninstall --undeclared --repo " + shellWord(fixture.repository) + "\n"})
	if _, err := os.Stat(hook); err != nil {
		t.Fatalf("a declined offer removed the hook: %v", err)
	}

	fixture.declare("pre-commit")
	accepted := fixture.runWith("y\ny\n", true, "config", "checkpoint", "remove", "pre-commit", "--repo", fixture.repository)
	assertRunContains(t, accepted, commandRun{stdout: offer + "Updated 0 file(s); deleted 1 file(s).\n"})
	if _, err := os.Stat(hook); err == nil {
		t.Fatal("an accepted offer left the hook")
	}
}

func TestConfigCheckpointRemoveOnlyNamesTheUninstallWithoutAPrompt(t *testing.T) {
	for _, test := range []struct {
		name           string
		input          string
		outputTerminal bool
		arguments      []string
	}{
		{name: "--yes in a terminal", outputTerminal: true, arguments: []string{"--yes"}},
		{name: "output that is not a terminal", input: "y\ny\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newUninstallFixture(t)
			fixture.install([]string{"git"})
			var stdout, stderr bytes.Buffer
			streams := productionCommandIO(iotest.OneByteReader(strings.NewReader(test.input)), &stdout, &stderr)
			streams.terminal = func(stream any) bool { return test.outputTerminal || stream != &stdout }

			arguments := append([]string{"config", "checkpoint", "remove", "pre-commit", "--repo", fixture.repository}, test.arguments...)
			if exit := execute(context.Background(), arguments, streams); exit != 0 || stderr.String() != "" {
				t.Fatalf("config checkpoint remove = %d, stderr %q", exit, stderr.String())
			}
			hint := "\nInstalled Checkpoint Integrations serve no declared Checkpoint; remove them with: review-party checkpoint uninstall --undeclared --repo " + shellWord(fixture.repository) + "\n"
			if !strings.HasSuffix(stdout.String(), hint) || strings.Contains(stdout.String(), "Remove them?") {
				t.Fatalf("stdout = %q, want only the hint after the removal", stdout.String())
			}
			if _, err := os.Stat(fixture.path(".git/hooks/pre-commit")); err != nil {
				t.Fatalf("the hook was removed without a prompt: %v", err)
			}
		})
	}
}

func TestUninstallLeavesTheSharedCodexFileUnlessAsked(t *testing.T) {
	fixture := newUninstallFixture(t)
	fixture.install([]string{"codex", "--personal"})
	installed := fixture.tree()
	shared := filepath.Join(fixture.codexHome, "hooks.json")

	assertRun(t, fixture.uninstall("", false, "--yes"), commandRun{stdout: "Nothing to remove.\nnote: " + shared + " still runs review-party for every repository on this machine; remove it with --shared\n"})
	assertTree(t, fixture.tree(), installed)
	assertRun(t, fixture.uninstall("", false, "--shared", "--yes"), commandRun{stdout: "codex: delete the file install created at " + shared + "\nUpdated 0 file(s); deleted 1 file(s).\n"})
}

func TestUninstallRemovesEveryCopyOfTheBlock(t *testing.T) {
	fixture := newUninstallFixture(t)
	block := checkpointHookBlock(configuration.CheckpointPrePush)
	fixture.writeExecutable(".git/hooks/pre-push", "#!/bin/sh\n"+block+"echo team\n"+block)

	assertRun(t, fixture.uninstall("", false, "--yes"), commandRun{stdout: "git pre-push: remove the review-party block from " + fixture.path(".git/hooks/pre-push") + "\nUpdated 1 file(s); deleted 0 file(s).\n"})
	if got := fixture.read(".git/hooks/pre-push"); got != "#!/bin/sh\necho team\n" {
		t.Fatalf("pre-push = %q", got)
	}
}
