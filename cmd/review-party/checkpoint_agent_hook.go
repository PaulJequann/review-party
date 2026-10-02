package main

// Caller Agent hooks decide Checkpoints from the agent's PreToolUse event,
// before its shell runs a git push or git commit. Claude Code and Codex send
// the same event fields and read the same JSON deny and warning output, so
// each adapter is a table entry; the decision is the one the git hook makes.
// Every decision exits 0, because both agents block a tool call on exit 2,
// which a stale review-party also returns for an unknown command.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
)

// agentIntegration is one Caller Agent's Checkpoint Integration: its
// PreToolUse protocol and the hook settings its installer writes.
type agentIntegration struct {
	integration configuration.IntegrationName
	agent       hookTool
	// binary is the agent's executable, which init looks for on PATH.
	binary string
	// shellTool is the tool_name the agent gives the tool that runs shell
	// commands; events for other tools are not relevant.
	shellTool string
	// teamFile is the committed settings file, relative to the repository
	// root; personalFile is the Caller's own, which personalHelp names.
	teamFile     string
	personalFile func(root string) (string, error)
	personalHelp string
	// handler is the hook handler every declared Checkpoint shares.
	handler agentHookHandler
	// missingBinary is what the installed hook does while review-party is
	// not on PATH.
	missingBinary string
	followUp      []string
}

var agentIntegrations = []agentIntegration{
	{
		integration: configuration.IntegrationClaudeCode, agent: hookToolClaude, binary: "claude", shellTool: "Bash",
		teamFile: ".claude/settings.json",
		personalFile: func(root string) (string, error) {
			return filepath.Join(root, ".claude", "settings.local.json"), nil
		},
		personalHelp:  ".claude/settings.local.json",
		handler:       claudeCodeHookHandler,
		missingBinary: "Claude Code shows a warning on each git command and runs it unchecked",
	},
	{
		integration: configuration.IntegrationCodex, agent: hookToolCodex, binary: "codex", shellTool: "Bash",
		teamFile:      ".codex/hooks.json",
		personalFile:  codexPersonalHooksFile,
		personalHelp:  "$CODEX_HOME/hooks.json (~/.codex/hooks.json by default)",
		handler:       codexHookHandler,
		missingBinary: "the Codex hook allows every command silently",
		followUp:      []string{codexApprovalStep, codexCostNote},
	},
}

// agentHookDeny is the PreToolUse output both agents read as a denied tool
// call, with the reason passed to the model
// (https://code.claude.com/docs/en/hooks#pretooluse-decision-control,
// https://learn.chatgpt.com/docs/hooks).
type agentHookDeny struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// preToolUseEvent holds the PreToolUse fields the hook reads. tool_input
// stays raw until the tool is known to be the shell, because other tools
// send their own arguments there.
type preToolUseEvent struct {
	ToolName  string          `json:"tool_name"`
	Cwd       string          `json:"cwd"`
	ToolInput json.RawMessage `json:"tool_input"`
}

func newAgentHookCommand(adapter agentIntegration, streams commandIO) *cobra.Command {
	command := &cobra.Command{
		Use:   string(adapter.integration),
		Short: "Decide declared Checkpoints from a " + string(adapter.agent) + " PreToolUse hook",
		Long: fmt.Sprintf(`Decide declared Checkpoints from a %[1]s PreToolUse hook. review-party
checkpoint install %[2]s wires this command into %[1]s's hook settings.

Reads the PreToolUse event on standard input. A shell command with no git push
or git commit exits 0 with no output before reading any configuration. A git
push decides pre-push and a git commit decides pre-commit in the repository the
command runs in. A refused Checkpoint prints a JSON PreToolUse deny whose
reason names the next command, which %[1]s passes to the model. Setup,
configuration, git, or parse errors print one warning as a JSON systemMessage.
Both exit 0; only a failed write exits 1.`, adapter.agent, adapter.integration),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return commandResult(executeAgentHook(cmd.Context(), adapter, stringFlag(cmd, "config"), streams))
		},
	}
	addConfigurationFlag(command)
	return command
}

func executeAgentHook(ctx context.Context, adapter agentIntegration, configurationPath string, streams commandIO) int {
	var output any
	decision := decideAgentHook(ctx, adapter, configurationPath, streams)
	switch {
	case decision.refusal != "":
		var deny agentHookDeny
		deny.HookSpecificOutput.HookEventName = "PreToolUse"
		deny.HookSpecificOutput.PermissionDecision = "deny"
		deny.HookSpecificOutput.PermissionDecisionReason = decision.refusal
		output = deny
	case decision.warning != nil:
		output = map[string]string{"systemMessage": "warning: " + decision.warning.Error()}
	default:
		return 0
	}
	encoder := json.NewEncoder(streams.output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(output); err != nil {
		return 1
	}
	return 0
}

func decideAgentHook(ctx context.Context, adapter agentIntegration, configurationPath string, streams commandIO) hookDecision {
	commands, err := adapter.gitCommands(streams.input)
	if err != nil {
		return hookDecision{warning: fmt.Errorf("Checkpoints not checked: %w", err)}
	}
	var warnings []error
	for _, command := range commands {
		decision := decideAgentGitCommand(ctx, command, configurationPath, streams)
		if decision.refusal != "" {
			return decision
		}
		if decision.warning != nil {
			warnings = append(warnings, fmt.Errorf("%s Checkpoint not checked: %w", command.checkpoint, decision.warning))
		}
	}
	return joinedWarning(warnings)
}

// gitCommands reads a PreToolUse event and returns the git push and git
// commit commands in its shell command, each with the directory git runs in.
func (agent agentIntegration) gitCommands(input io.Reader) ([]agentGitCommand, error) {
	var event preToolUseEvent
	if err := json.NewDecoder(input).Decode(&event); err != nil {
		return nil, fmt.Errorf("read the %s PreToolUse event: %w", agent.agent, err)
	}
	if event.ToolName != agent.shellTool {
		return nil, nil
	}
	var shell struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(event.ToolInput, &shell); err != nil {
		return nil, fmt.Errorf("read the %s command: %w", agent.agent, err)
	}
	commands, err := findAgentGitCommands(shell.Command)
	for index, command := range commands {
		commands[index].directory = within(cmp.Or(event.Cwd, "."), command.directory)
	}
	return commands, err
}

// decideAgentGitCommand decides one git push or git commit. A declared
// Checkpoint refuses a command chained after one other than a cd, since the
// content at hook time is not what git will see.
func decideAgentGitCommand(ctx context.Context, command agentGitCommand, configurationPath string, streams commandIO) hookDecision {
	options := checkpointOptions{name: command.checkpoint, repository: command.directory, configuration: configurationPath}
	root, declared, err := declaredCheckpoint(options, streams)
	switch {
	case err != nil:
		return hookDecision{warning: err}
	case !declared:
		return hookDecision{}
	case command.preceded:
		return hookDecision{refusal: fmt.Sprintf("%s Checkpoint cannot check a git %s chained after another command; run that git %[2]s as its own command", command.checkpoint, command.verb())}
	}
	return command.decideContent(ctx, root, options)
}

// decideContent decides the content the command sends against its declared
// Checkpoint. A commit of paths or hunks is refused, since the hook cannot
// see what it takes.
func (command agentGitCommand) decideContent(ctx context.Context, root string, options checkpointOptions) hookDecision {
	switch command.form {
	case formUndecidable:
		return hookDecision{warning: errors.New(command.reason)}
	case formCommitPaths:
		return hookDecision{refusal: fmt.Sprintf("%s Checkpoint cannot see the content a git commit of paths or picked hunks takes; stage the change and run git commit without paths", command.checkpoint)}
	case formCommitTracked:
		options.commit = commitTracked
		return decideHookContent(ctx, options)
	case formCommitStaged:
		return decideHookContent(ctx, options)
	case formPush:
	}
	refs, err := command.push.Refs(root)
	if err != nil {
		return hookDecision{warning: err}
	}
	return decidePushedRefs(ctx, root, options, refs)
}
