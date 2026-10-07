package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

// toolCall is the tool call a PreToolUse event announces.
type toolCall struct{ tool, command, cwd string }

// codexEvent is a Codex PreToolUse event with every field its documented
// input schema requires.
func codexEvent(t *testing.T, call toolCall) string {
	t.Helper()
	event, err := json.Marshal(map[string]any{
		"session_id": "019a0000-0000-7000-8000-000000000000", "turn_id": "turn-1", "transcript_path": nil,
		"cwd": call.cwd, "hook_event_name": "PreToolUse", "model": "gpt-5.5", "permission_mode": "default",
		"tool_name": call.tool, "tool_use_id": "call_1", "tool_input": map[string]string{"command": call.command},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(event)
}

// claudeEvent is a Claude Code PreToolUse event with the fields its Bash tool
// sends.
func claudeEvent(t *testing.T, call toolCall) string {
	t.Helper()
	event, err := json.Marshal(map[string]any{
		"session_id": "abc123", "transcript_path": "/tmp/transcript.jsonl", "cwd": call.cwd, "permission_mode": "default",
		"hook_event_name": "PreToolUse", "tool_name": call.tool, "tool_use_id": "toolu_01",
		"tool_input": map[string]any{"command": call.command, "description": "Push the branch", "timeout": 120000},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(event)
}

// denial is the reason a hook gives for denying a tool call.
type denial string

// run is the hook run that denies the tool call: a JSON PreToolUse deny on
// stdout and exit 0, which no agent reads as a failed hook.
func (reason denial) run() commandRun {
	quoted := strings.ReplaceAll(string(reason), `"`, `\"`)
	return commandRun{stdout: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"` + quoted + `"}}` + "\n"}
}

func (fixture checkpointFixture) agentHook(agent configuration.IntegrationName, event string, arguments ...string) commandRun {
	fixture.t.Helper()
	fixture.t.Chdir(fixture.t.TempDir())
	return fixture.runWith(event, false, append([]string{"checkpoint", "hook", string(agent)}, arguments...)...)
}

func TestAgentHookClassifiesBeforeReadingConfiguration(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push")
	fixture.git("update-ref", "refs/remotes/origin/main", fixture.base)
	fixture.commit("one.go", "package app\n\nconst one = 1\n")
	notAFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notAFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(notAFile, "config.json")
	outsideGit := t.TempDir()

	for _, adapter := range agentIntegrations {
		agent := adapter.integration
		assertRun(t, fixture.agentHook(agent, codexEvent(t, toolCall{"Bash", "ls -la", outsideGit}), "--config", unreadable), commandRun{})
		assertRun(t, fixture.agentHook(agent, codexEvent(t, toolCall{"apply_patch", "git push", fixture.repository}), "--config", unreadable), commandRun{})
	}
	pushing := fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", "git push", fixture.repository}), "--config", unreadable)
	assertRunContains(t, pushing, commandRun{stdout: `{"systemMessage":"warning: pre-push Checkpoint not checked: `})
}

func TestAgentHookRefusesAnUnreviewedPushInEachAgentsForm(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.git("update-ref", "refs/remotes/origin/main", fixture.base)
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	assertRun(t, fixture.agentHook(configuration.IntegrationClaudeCode, claudeEvent(t, toolCall{"Bash", "git push", fixture.repository})), commandRun{})

	fixture.declare("pre-push")
	repo := " --repo " + shellQuoteArgument(fixture.repository)
	refusal := denial("pre-push Checkpoint: 3 unreviewed lines in " + fixture.base[:12] + ".." + head[:12] +
		"; next: review-party run --unreviewed --base " + fixture.base + " --head " + head + repo).run()
	assertRun(t, fixture.agentHook(configuration.IntegrationClaudeCode, claudeEvent(t, toolCall{"Bash", "git push", fixture.repository})), refusal)
	assertRun(t, fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", "git -C " + fixture.repository + " push origin main | tail -1", "/"})), refusal)

	changes := fixture.rangeChanges(head).Changes
	fixture.saveReview("rp_1725192000000_00000000000000d1", "bugs", model.LifecycleCompleted, changes)
	fixture.saveReview("rp_1725192000000_00000000000000d2", "docs", model.LifecycleCompleted, changes)
	assertRun(t, fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", "git push", fixture.repository})), commandRun{})
}

func TestAgentHookRefusesCommitsItCannotSee(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-commit", "--waivers", "anyone")
	fixture.writeFile("app.go", "package app\n\nconst changed = true\n")
	fixture.git("add", "app.go")
	hook := func(command string) commandRun {
		return fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", command, fixture.repository}))
	}

	repo := " --repo " + shellQuoteArgument(fixture.repository)
	staged := denial("pre-commit Checkpoint: 2 unreviewed lines in the staged changes; next: review-party run --unreviewed" + repo)
	assertRun(t, hook("git commit -m x"), staged.run())
	tracked := denial("pre-commit Checkpoint: 2 unreviewed lines in the tracked changes; next: review-party run --unreviewed" + repo)
	assertRun(t, hook("git commit -am x"), tracked.run())
	paths := denial("pre-commit Checkpoint cannot see the content a git commit of paths or picked hunks takes; stage the change and run git commit without paths")
	assertRun(t, hook("git commit -m x app.go"), paths.run())
	chained := denial("pre-commit Checkpoint cannot check a git commit chained after another command; run that git commit as its own command")
	assertRun(t, hook("git add -A && git commit -m x"), chained.run())
	assertRun(t, hook("git push origin main"), commandRun{})
}

func TestAgentHookWarnsAndAllowsWithoutADecision(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push")
	fixture.commit("one.go", "package app\n\nconst one = 1\n")
	warning := func(message string) commandRun {
		return commandRun{stdout: `{"systemMessage":"warning: ` + message + `"}` + "\n"}
	}

	assertRun(t, fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", "git push --tags", fixture.repository})),
		warning("pre-push Checkpoint not checked: git push names refs the hook does not resolve"))
	assertRun(t, fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", "git push origin missing", fixture.repository})),
		warning(`pre-push Checkpoint not checked: cannot resolve pushed revision \"missing\"`))
	assertRun(t, fixture.agentHook(configuration.IntegrationClaudeCode, claudeEvent(t, toolCall{"Bash", `git push "unfinished`, fixture.repository})),
		warning("Checkpoints not checked: unterminated quote in the command"))
	assertRunContains(t, fixture.agentHook(configuration.IntegrationClaudeCode, "not json"), commandRun{stdout: `{"systemMessage":"warning: Checkpoints not checked: read the Claude Code PreToolUse event: `})
}

func TestAgentHookDecidesAGitCommandAfterALiteralCdInItsDirectory(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.declare("pre-push")
	fixture.git("update-ref", "refs/remotes/origin/main", fixture.base)
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	parent, name := filepath.Split(fixture.repository)
	hook := func(command string) commandRun {
		return fixture.agentHook(configuration.IntegrationClaudeCode, claudeEvent(t, toolCall{"Bash", command, parent}))
	}

	repo := " --repo " + shellQuoteArgument(fixture.repository)
	unreviewed := denial("pre-push Checkpoint: 3 unreviewed lines in " + fixture.base[:12] + ".." + head[:12] +
		"; next: review-party run --unreviewed --base " + fixture.base + " --head " + head + repo)
	assertRun(t, hook("cd "+name+" && git push"), unreviewed.run())
	assertRun(t, hook("cd /; cd "+fixture.repository+"\ngit push origin main"), unreviewed.run())
	chained := denial("pre-push Checkpoint cannot check a git push chained after another command; run that git push as its own command")
	for _, command := range []string{"cd $X && git push", "echo hi && git push", "cd " + name + " || git push", "(cd " + name + "); git push"} {
		assertRun(t, fixture.agentHook(configuration.IntegrationCodex, codexEvent(t, toolCall{"Bash", command, fixture.repository})), chained.run())
	}

	changes := fixture.rangeChanges(head).Changes
	fixture.saveReview("rp_1725192000000_00000000000000d1", "bugs", model.LifecycleCompleted, changes)
	fixture.saveReview("rp_1725192000000_00000000000000d2", "docs", model.LifecycleCompleted, changes)
	assertRun(t, hook("cd "+name+" && git push"), commandRun{})
}
