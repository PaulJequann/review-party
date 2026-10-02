package main

// Caller Agent installers add one PreToolUse entry that runs review-party
// checkpoint hook <agent> to a settings file; every declared Checkpoint shares
// it, since the hook decides which Checkpoint a command meets. The Claude Code
// entry carries the if rule Bash(git *), so the hook starts only for git
// commands, including git -C and git after && or VAR=value
// (https://code.claude.com/docs/en/hooks#common-fields). Codex has no such
// filter and runs the hook before every shell command
// (https://learn.chatgpt.com/docs/hooks). Each command fails open: a guard
// skips the hook while review-party is not on PATH, and || true keeps any
// exit status of an old or broken review-party from blocking the tool call.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
)

// agentHookHandler and agentHookGroup are the PreToolUse settings shape both
// agents read: groups select a tool by matcher and list command handlers.
type agentHookHandler struct {
	Type    string `json:"type"`
	If      string `json:"if,omitempty"`
	Command string `json:"command"`
}

type agentHookGroup struct {
	Matcher string             `json:"matcher"`
	Hooks   []agentHookHandler `json:"hooks"`
}

var (
	claudeCodeHookHandler = agentHookHandler{
		Type: "command", If: "Bash(git *)",
		Command: `if command -v review-party >/dev/null 2>&1; then review-party checkpoint hook claude-code || true; else echo '{"systemMessage":"warning: review-party is not on PATH, so Review Checkpoints were not checked"}'; fi`,
	}
	codexHookHandler = agentHookHandler{
		Type:    "command",
		Command: "if command -v review-party >/dev/null 2>&1; then review-party checkpoint hook codex || true; fi",
	}
)

// codexPersonalHooksFile is the user hooks file under CODEX_HOME, which
// defaults to ~/.codex (https://learn.chatgpt.com/codex/config-file/config-advanced).
func codexPersonalHooksFile(string) (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "hooks.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the Codex home: %w", err)
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}

const (
	codexApprovalStep = "next: open Codex in this repository and run /hooks to trust the review-party entry; Codex skips a new or changed hook until it is trusted"
	codexCostNote     = "note: Codex starts a login shell for this hook before every shell command, so each command waits about 70 ms more (measured p50 68 to 73 ms, p95 71 to 89 ms), most of it the login shell"
)

func newAgentInstallCommand(agent agentIntegration, streams commandIO) *cobra.Command {
	command := &cobra.Command{
		Use:   string(agent.integration),
		Short: "Install the " + string(agent.agent) + " hook that decides every declared Checkpoint",
		Long: fmt.Sprintf(`Install one %[1]s PreToolUse entry that runs review-party checkpoint hook
%[2]s for every declared Checkpoint. The team file is %[3]s in the
repository; --personal writes %[4]s instead.

Entries are appended: existing keys, entries, and indentation are kept, and a
missing file is created. An installed entry is left as is and an edited
review-party entry is reported and left alone, so rerunning is safe.`, agent.agent, agent.integration, agent.teamFile, agent.personalHelp),
		Example: fmt.Sprintf("  review-party checkpoint install %[1]s\n  review-party checkpoint install %[1]s --personal --yes", agent.integration),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := hookInstallOptions{
				repository: stringFlag(cmd, "repo"), configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes"),
				integration: agent.integration, personal: boolFlag(cmd, "personal"),
			}
			return commandResult(executeCheckpointInstall(options, streams))
		},
	}
	addRepositoryFlag(command, "Git repository whose declared Checkpoints to install hooks for")
	addConfigurationFlag(command)
	command.Flags().Bool("yes", false, "Write the hooks without a confirmation prompt")
	command.Flags().Bool("personal", false, "Write "+agent.personalHelp+" instead of the team file")
	return command
}

func planAgentHookInstall(target hookInstallTarget, manager *configuration.Manager, agent agentIntegration) (hookInstallPlan, error) {
	declared, err := manager.Checkpoints(configuration.Repository(target.root))
	if err != nil {
		return hookInstallPlan{}, err
	}
	path, err := agent.hooksFile(target.root, target.personal)
	if err != nil {
		return hookInstallPlan{}, err
	}
	plan := hookInstallPlan{
		tool: agent.agent, location: path, writes: map[string][]byte{}, fileMode: 0o644, config: configurationArgument(target.configuration),
		warnings: missingBinaryNote(agent.missingBinary), followUp: agent.followUp,
	}
	if target.personal && committable(target.root, path) {
		plan.warnings = append(plan.warnings, "warning: git does not ignore "+path+"; commit it only if the team should share these entries")
	}
	for _, name := range configuration.SortedCheckpointNames(declared) {
		step, err := plan.planAgentEntry(agent, name)
		if err != nil {
			return hookInstallPlan{}, err
		}
		plan.steps = append(plan.steps, step)
	}
	return plan, nil
}

// hooksFile is the settings file an install writes: the Caller's own with
// personal, else the team's.
func (agent agentIntegration) hooksFile(root string, personal bool) (string, error) {
	if personal {
		return agent.personalFile(root)
	}
	return filepath.Join(root, agent.teamFile), nil
}

// committable reports whether path lies in the repository and git does not
// ignore it.
func committable(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return false
	}
	return exec.Command("git", "-C", root, "check-ignore", "-q", "--", relative).Run() != nil
}

// planAgentEntry adds the agent's group for one Checkpoint unless the file
// already has its handler. A later Checkpoint shares the group an earlier one
// added, the only write an agent plan makes.
func (plan *hookInstallPlan) planAgentEntry(agent agentIntegration, name configuration.CheckpointName) (hookInstallStep, error) {
	step := hookInstallStep{checkpoint: name, path: plan.location}
	if _, added := plan.writes[plan.location]; added {
		step.outcome = hookEntryShared
		return step, nil
	}
	content, found, err := plan.pending(plan.location)
	if err != nil {
		return step, err
	}
	if step.outcome, err = findAgentHookEntry(content, agent); err != nil {
		return step, fmt.Errorf("read %s: %w", plan.location, err)
	}
	if step.outcome != hookEntryAdded {
		return step, nil
	}
	group := agentHookGroup{Matcher: agent.shellTool, Hooks: []agentHookHandler{agent.handler}}
	updated, err := appendJSONArrayElement(content, []string{"hooks", "PreToolUse"}, group)
	if err != nil {
		return step, fmt.Errorf("add to %s: %w", plan.location, err)
	}
	if !found {
		step.outcome = hookCreated
	}
	plan.writes[plan.location] = updated
	return step, nil
}

// findAgentHookEntry reports hookInstalled when a group for the shell tool
// has the agent's handler, and hookEntryEdited when a handler with the same if
// rule runs this agent's review-party hook in some other form.
func findAgentHookEntry(content []byte, agent agentIntegration) (hookOutcome, error) {
	entries, err := preToolUseEntries(content)
	if err != nil {
		return "", err
	}
	want, err := canonicalJSON(agent.handler)
	if err != nil {
		return "", err
	}
	marker := "review-party checkpoint hook " + string(agent.integration)
	outcome := hookEntryAdded
	for _, entry := range entries {
		if entry.handler.If != agent.handler.If || !strings.Contains(entry.handler.Command, marker) {
			continue
		}
		if entry.matcher == agent.shellTool && bytes.Equal(entry.canonical, want) {
			return hookInstalled, nil
		}
		outcome = hookEntryEdited
	}
	return outcome, nil
}

// settingsHookEntry is one command handler in a settings file's PreToolUse
// groups, with its JSON in canonical form for comparison.
type settingsHookEntry struct {
	matcher   string
	handler   agentHookHandler
	canonical []byte
}

// preToolUseEntries reads the PreToolUse handlers of a settings file, leaving
// out any that is not a command handler object.
func preToolUseEntries(content []byte) ([]settingsHookEntry, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}
	var settings struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string            `json:"matcher"`
				Hooks   []json.RawMessage `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil, err
	}
	var entries []settingsHookEntry
	for _, group := range settings.Hooks.PreToolUse {
		for _, raw := range group.Hooks {
			entry := settingsHookEntry{matcher: group.Matcher}
			if json.Unmarshal(raw, &entry.handler) != nil {
				continue
			}
			canonical, err := canonicalJSON(raw)
			if err != nil {
				return nil, err
			}
			entry.canonical = canonical
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// canonicalJSON renders a value with object keys sorted, so handlers compare
// by content rather than by key order or spacing.
func canonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}
