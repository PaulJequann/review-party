package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

const (
	prePushLine   = "Before pushing, review the change with `review-party run --base <upstream> --head HEAD`; the pre-push Checkpoint refuses a push no completed Review covers.\n"
	preCommitLine = "Before committing, stage the change, stash any other changes, and review it with `review-party run`; the pre-commit Checkpoint refuses a commit no completed Review covers.\n"
	helpLine      = "`review-party checkpoint --help` has details.\n"
)

func agentsMDBlock(lines ...string) string {
	return "<!-- review-party checkpoints: begin -->\n" + strings.Join(lines, "") + helpLine + "<!-- review-party checkpoints: end -->\n"
}

func TestAgentsMDInstallAppendsOnceAndKeepsTheRestOfTheFile(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	existing := "# Rules\n\nKeep changes small."
	fixture.writeFile("AGENTS.md", existing)
	path := filepath.Join(fixture.repository, "AGENTS.md")

	assertRun(t, fixture.installAgent("agents-md", "--yes"), commandRun{stdout: "pre-push: add the review-party block to " + path + " (agent instructions)\nWrote 1 file(s).\n"})
	want := existing + "\n\n" + agentsMDBlock(prePushLine)
	if got := fixture.read("AGENTS.md"); got != want {
		t.Fatalf("AGENTS.md =\n%s\nwant\n%s", got, want)
	}

	assertRun(t, fixture.installAgent("agents-md"), commandRun{stdout: "pre-push: already installed in " + path + " (agent instructions)\n"})
	if got := fixture.read("AGENTS.md"); got != want {
		t.Fatalf("a rerun changed AGENTS.md:\n%s", got)
	}
}

func TestAgentsMDInstallReplacesOnlyAStaleBlockAfterConfirmation(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	before, after := "# Rules\n\n", "\n## Later\n\nKept as written.\n"
	stale := before + agentsMDBlock(prePushLine) + after
	fixture.writeFile("AGENTS.md", stale)
	fixture.declare("pre-commit", "--waivers", "anyone")
	path := filepath.Join(fixture.repository, "AGENTS.md")

	plan := "pre-push: replace the stale review-party block in " + path + " (agent instructions)\npre-commit: replace the stale review-party block in " + path + " (agent instructions)\n"
	assertRun(t, fixture.installAgent("agents-md"), commandRun{stdout: plan, stderr: "review-party: rerun with --yes to write the hooks without a terminal\n", exit: 2})
	if got := fixture.read("AGENTS.md"); got != stale {
		t.Fatalf("an unconfirmed install wrote AGENTS.md:\n%s", got)
	}

	result := fixture.runWith("y\n", true, "checkpoint", "install", "agents-md", "--repo", fixture.repository)
	assertRun(t, result, commandRun{stdout: plan + "Write these files? [y/N] Wrote 1 file(s).\n"})
	waive := strings.TrimSuffix(preCommitLine, "\n") + " When a Review does not fit, waive it with `review-party checkpoint waive pre-commit --reason \"<why>\"`.\n"
	if got, want := fixture.read("AGENTS.md"), before+agentsMDBlock(prePushLine, waive)+after; got != want {
		t.Fatalf("AGENTS.md =\n%s\nwant\n%s", got, want)
	}
}

func TestAgentsMDInstallRefusesUnbalancedMarkers(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	broken := "<!-- review-party checkpoints: begin -->\nold\n<!-- review-party checkpoints: begin -->\n<!-- review-party checkpoints: end -->\n"
	fixture.writeFile("AGENTS.md", broken)
	path := filepath.Join(fixture.repository, "AGENTS.md")

	assertRun(t, fixture.installAgent("agents-md", "--yes"), commandRun{stdout: "pre-push: fix the review-party markers by hand in " + path + " (agent instructions)\n" +
		"  Found 2 \"<!-- review-party checkpoints: begin -->\" and 1 \"<!-- review-party checkpoints: end -->\" lines; the block needs one begin line before one end line.\n" +
		"  Delete the extra or misplaced marker lines, or both markers and the lines between them, then rerun the install.\n"})
	if got := fixture.read("AGENTS.md"); got != broken {
		t.Fatalf("malformed markers rewritten:\n%s", got)
	}

	fixture.writeFile("AGENTS.md", "<!-- review-party checkpoints: end -->\n<!-- review-party checkpoints: begin -->\n")
	assertRunContains(t, fixture.installAgent("agents-md", "--yes"), commandRun{stdout: "pre-push: fix the review-party markers by hand in "})
}

func TestAgentsMDInstallChoosesTheInstructionsFile(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.writeFile("CLAUDE.md", "# Claude\n")
	assertRunContains(t, fixture.installAgent("agents-md", "--yes"), commandRun{stdout: "pre-push: add the review-party block to " + filepath.Join(fixture.repository, "CLAUDE.md") + " "})
	if got, want := fixture.read("CLAUDE.md"), "# Claude\n\n"+agentsMDBlock(prePushLine); got != want {
		t.Fatalf("CLAUDE.md =\n%s\nwant\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(fixture.repository, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("AGENTS.md created beside CLAUDE.md: %v", err)
	}

	fixture.writeFile("AGENTS.md", "# Agents\n")
	assertRunContains(t, fixture.installAgent("agents-md", "--yes"), commandRun{stdout: "pre-push: add the review-party block to " + filepath.Join(fixture.repository, "AGENTS.md") + " "})

	empty := newHookInstallFixture(t, configuration.CheckpointPrePush)
	assertRunContains(t, empty.installAgent("agents-md", "--yes"), commandRun{stdout: "pre-push: create " + filepath.Join(empty.repository, "AGENTS.md") + " "})
	if got := empty.read("AGENTS.md"); got != agentsMDBlock(prePushLine) {
		t.Fatalf("created AGENTS.md =\n%s", got)
	}
}
