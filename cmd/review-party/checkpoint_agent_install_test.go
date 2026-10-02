package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func (fixture hookInstallFixture) installAgent(agent string, arguments ...string) commandRun {
	fixture.t.Helper()
	return fixture.runWith("", false, append([]string{"checkpoint", "install", agent, "--repo", fixture.repository}, arguments...)...)
}

const codexFollowUp = codexApprovalStep + "\n" + codexCostNote + "\n"

func TestClaudeCodeInstallAppendsToExistingSettingsOnce(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPreCommit, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	existing := `{
  "permissions": {"allow": ["Bash(go test *)"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Edit", "hooks": [{"type": "command", "command": "./lint.sh"}]}
    ]
  },
  "model": "opus"
}
`
	fixture.writeFile(".claude/settings.json", existing)
	path := filepath.Join(fixture.repository, ".claude", "settings.json")

	added := "pre-push: add a PreToolUse entry to " + path + " (Claude Code)\npre-commit: share the PreToolUse entry added to " + path + " (Claude Code)\n"
	assertRun(t, fixture.installAgent("claude-code", "--yes"), commandRun{stdout: added + "Wrote 1 hook file(s).\n"})
	want := `{
  "permissions": {"allow": ["Bash(go test *)"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Edit", "hooks": [{"type": "command", "command": "./lint.sh"}]},
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "if": "Bash(git *)",
            "command": "if command -v review-party >/dev/null 2>&1; then review-party checkpoint hook claude-code || true; else echo '{\"systemMessage\":\"warning: review-party is not on PATH, so Review Checkpoints were not checked\"}'; fi"
          }
        ]
      }
    ]
  },
  "model": "opus"
}
`
	if got := fixture.read(".claude/settings.json"); got != want {
		t.Fatalf("settings.json =\n%s\nwant\n%s", got, want)
	}

	installed := "pre-push: already installed in " + path + " (Claude Code)\npre-commit: already installed in " + path + " (Claude Code)\n"
	assertRun(t, fixture.installAgent("claude-code"), commandRun{stdout: installed})
	if got := fixture.read(".claude/settings.json"); got != want {
		t.Fatalf("a rerun changed settings.json:\n%s", got)
	}
}

func TestCodexInstallSharesOneEntryAndNamesTheApprovalStep(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPreCommit, configuration.CheckpointPrePush)
	path := filepath.Join(fixture.repository, ".codex", "hooks.json")

	missing := "warning: review-party is not on PATH; the Codex hook allows every command silently until it is\n"
	created := "pre-push: create " + path + " (Codex)\npre-commit: share the PreToolUse entry added to " + path + " (Codex)\n"
	assertRun(t, fixture.installAgent("codex", "--yes"), commandRun{stdout: created + missing + "Wrote 1 hook file(s).\n" + codexFollowUp})
	want := `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "if command -v review-party >/dev/null 2>&1; then review-party checkpoint hook codex || true; fi"
          }
        ]
      }
    ]
  }
}
`
	if got := fixture.read(".codex/hooks.json"); got != want {
		t.Fatalf("hooks.json =\n%s\nwant\n%s", got, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("hooks.json mode = %v, %v", info, err)
	}

	fixture.provideStandIn()
	installed := "pre-push: already installed in " + path + " (Codex)\npre-commit: already installed in " + path + " (Codex)\n"
	assertRun(t, fixture.installAgent("codex"), commandRun{stdout: installed + codexFollowUp})
}

func TestAgentInstallLeavesAnEditedEntryAlone(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	edited := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"review-party checkpoint hook codex --config ~/rp.json"}]}]}}`
	fixture.writeFile(".codex/hooks.json", edited)
	path := filepath.Join(fixture.repository, ".codex", "hooks.json")

	assertRun(t, fixture.installAgent("codex"), commandRun{stdout: "pre-push: edited review-party entry left unchanged in " + path + " (Codex)\n" + codexFollowUp})
	if got := fixture.read(".codex/hooks.json"); got != edited {
		t.Fatalf("edited entry rewritten:\n%s", got)
	}
	fixture.writeFile(".claude/settings.json", `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","if":"Bash(git *)","command":"review-party checkpoint hook claude-code --config x"}]}]}}`)
	assertRunContains(t, fixture.installAgent("claude-code"), commandRun{stdout: "pre-push: edited review-party entry left unchanged in "})
	fixture.writeFile(".claude/settings.json", `{"hooks": []}`)
	assertRunContains(t, fixture.installAgent("claude-code", "--yes"), commandRun{exit: 1, stderr: "read " + filepath.Join(fixture.repository, ".claude", "settings.json") + ": json: cannot unmarshal array"})
}

func TestAgentInstallPersonalTargets(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	assertRunContains(t, fixture.installAgent("codex", "--personal", "--yes"), commandRun{stdout: "pre-push: create " + filepath.Join(codexHome, "hooks.json") + " (Codex)\n"})
	if _, err := os.Stat(filepath.Join(fixture.repository, ".codex")); err == nil {
		t.Fatal("a personal install wrote the team file")
	}

	local := filepath.Join(fixture.repository, ".claude", "settings.local.json")
	unignored := "warning: git does not ignore " + local + "; commit it only if the team should share these entries\n"
	assertRun(t, fixture.installAgent("claude-code", "--personal", "--yes"), commandRun{stdout: "pre-push: create " + local + " (Claude Code)\n" + unignored + "Wrote 1 hook file(s).\n"})
	fixture.writeFile(".gitignore", ".claude/settings.local.json\n")
	result := fixture.installAgent("claude-code", "--personal")
	if strings.Contains(result.stdout, "warning:") || !strings.Contains(fixture.read(".claude/settings.local.json"), `"if": "Bash(git *)"`) {
		t.Fatalf("ignored personal install = %+v", result)
	}
}

func TestAgentInstallWarnsThatTheHookIgnoresConfig(t *testing.T) {
	fixture := newHookInstallFixture(t, configuration.CheckpointPrePush)
	fixture.provideStandIn()
	config := filepath.Join(t.TempDir(), "review-party.json")

	path := filepath.Join(fixture.repository, ".claude", "settings.json")
	warning := "warning: the hooks load each Caller's default Global Configuration, not --config " + shellQuoteArgument(config) + "\n"
	assertRun(t, fixture.installAgent("claude-code", "--config", config), commandRun{stdout: "pre-push: create " + path + " (Claude Code)\n" + warning, stderr: "review-party: rerun with --yes to write the hooks without a terminal\n", exit: 2})
}
