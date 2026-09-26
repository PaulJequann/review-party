package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// claudeInstructionFiles follows Claude Code's claude-md-and-agents-md order.
// Like Codex, which runs at the repository root, only root files are loaded;
// the Review prompt covers nested AGENTS.md files.
var claudeInstructionFiles = []string{"CLAUDE.md", "AGENTS.md"}

// claudeInstructionBudget matches Codex's default project_doc_max_bytes.
const claudeInstructionBudget = 32 << 10

type claudeAdapter struct{}

func (claudeAdapter) Name() string { return "claude" }

func (claudeAdapter) Check(_ context.Context, candidate reviewerCandidate) availability {
	if err := validateClaudeCandidate(candidate); err != nil {
		return availability{Diagnostic: err.Error()}
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return availability{Diagnostic: "claude is not installed"}
	}
	return availability{Available: true}
}

func (claudeAdapter) Prepare(spec attemptSpec) (preparedAttempt, error) {
	instructions, err := repositoryInstructions(spec.Repository)
	if err != nil {
		return preparedAttempt{}, err
	}
	arguments := claudeCommand(spec.Candidate, instructions)
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Dir = spec.Repository
	command.Stdin = strings.NewReader(spec.Prompt)
	command.Env = reviewerEnvironment(spec.Candidate.ID)
	return preparedAttempt{command: command}, nil
}

func (claudeAdapter) Decode(output []byte) (decodedHarnessOutput, error) {
	decoded, err := decodeClaudeOutput(output)
	return decodedHarnessOutput{
		assistantText: decoded.assistantText,
		diagnostic:    decoded.diagnostic,
		model:         decoded.model,
		incomplete:    decoded.incomplete,
	}, err
}

// claudeCommand confines Claude Code to read-only repository tools.
// --restricted limits the file tools to the working directory and ignores
// user, project, and local settings; --safe-mode drops auto-memory, hooks,
// skills, and plugins. Either flag also stops Claude Code from auto-loading
// AGENTS.md and CLAUDE.md, and loading them natively requires the project
// settings source, which lets the reviewed revision's .claude/settings.json
// run hooks and redirect credentialed API traffic. Repository instructions
// are therefore read by Review Party and appended to the system prompt.
func claudeCommand(candidate reviewerCandidate, instructions string) []string {
	command := []string{
		"claude", "-p",
		"--output-format", "stream-json", "--verbose",
		"--model", candidate.Model,
		"--tools", "Read,Grep,Glob",
		"--permission-mode", "dontAsk",
		"--permission-prompts", "none",
		"--restricted",
		"--safe-mode",
		"--strict-mcp-config",
		"--no-session-persistence",
	}
	if usesClaudeEffort(candidate) {
		command = append(command, "--effort", candidate.Effort)
	}
	if instructions != "" {
		command = append(command, "--append-system-prompt", instructions)
	}
	return command
}

// repositoryInstructions reads the root instruction files through an os.Root,
// so a symlink that escapes the repository fails the attempt instead of
// exposing files outside it. A file linked to an earlier one loads once.
func repositoryInstructions(repository string) (_ string, err error) {
	root, err := os.OpenRoot(repository)
	if err != nil {
		return "", fmt.Errorf("open repository instructions: %w", err)
	}
	defer func() {
		err = errors.Join(err, root.Close())
	}()
	var sections []string
	var loaded []fs.FileInfo
	remaining := claudeInstructionBudget
	for _, name := range claudeInstructionFiles {
		if remaining <= 0 {
			break
		}
		info, content, readErr := readRepositoryInstruction(root, name, remaining)
		if errors.Is(readErr, fs.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return "", readErr
		}
		if content == "" || slices.ContainsFunc(loaded, func(other fs.FileInfo) bool { return os.SameFile(info, other) }) {
			continue
		}
		loaded = append(loaded, info)
		remaining -= len(content)
		sections = append(sections, fmt.Sprintf("Contents of %s (project instructions, checked into the codebase):\n\n%s", name, content))
	}
	return strings.Join(sections, "\n\n"), nil
}

func readRepositoryInstruction(root *os.Root, name string, limit int) (_ fs.FileInfo, _ string, err error) {
	info, err := root.Stat(name)
	if err != nil {
		return nil, "", fmt.Errorf("inspect repository instructions %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("repository instructions %s must be a regular file", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, "", fmt.Errorf("open repository instructions %s: %w", name, err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	content, err := io.ReadAll(io.LimitReader(file, int64(limit)))
	if err != nil {
		return nil, "", fmt.Errorf("read repository instructions %s: %w", name, err)
	}
	return info, strings.TrimSpace(string(content)), nil
}

func validateClaudeCandidate(candidate reviewerCandidate) error {
	if candidate.Effort == "auto" {
		return ReviewerEffortNotSupportedError{Reviewer: candidate.ID, Model: candidate.Model, Effort: candidate.Effort}
	}
	return nil
}

func usesClaudeEffort(candidate reviewerCandidate) bool {
	return hasExplicitEffort(candidate) && candidate.Effort != "default"
}

type decodedClaudeOutput struct {
	assistantText string
	diagnostic    string
	model         string
	incomplete    bool
}

type claudeEvent struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	Model   string          `json:"model"`
	Message json.RawMessage `json:"message"`
	IsError bool            `json:"is_error"`
	Result  string          `json:"result"`
	Errors  []string        `json:"errors"`
}

type claudeAssistantMessage struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// decodeClaudeOutput joins the Reviewer's text blocks and requires a
// successful terminal result event. Claude reports failures such as an
// unknown model in-band, so a missing or erroring result is incomplete even
// when the process exits cleanly.
func decodeClaudeOutput(output []byte) (decodedClaudeOutput, error) {
	decoded := decodedClaudeOutput{incomplete: true}
	var text strings.Builder
	var applyErr error
	err := scanJSONLines[claudeEvent](output, "claude", func(event claudeEvent) {
		if applyErr == nil {
			applyErr = applyClaudeEvent(&decoded, &text, event)
		}
	})
	if err = errors.Join(err, applyErr); err != nil {
		return decodedClaudeOutput{}, err
	}
	decoded.assistantText = text.String()
	if decoded.incomplete && decoded.diagnostic == "" {
		decoded.diagnostic = "claude output ended without a result event"
	}
	return decoded, nil
}

func applyClaudeEvent(decoded *decodedClaudeOutput, text *strings.Builder, event claudeEvent) error {
	switch event.Type {
	case "system":
		if event.Subtype == "init" && event.Model != "" {
			decoded.model = event.Model
		}
	case "assistant":
		var message claudeAssistantMessage
		if err := json.Unmarshal(event.Message, &message); err != nil {
			return fmt.Errorf("decode claude assistant message: %w", err)
		}
		if message.Model == "<synthetic>" {
			return nil
		}
		for _, block := range message.Content {
			if block.Type != "text" || block.Text == "" {
				continue
			}
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(block.Text)
		}
	case "result":
		decoded.incomplete = event.IsError
		if event.IsError {
			decoded.diagnostic = strings.TrimSpace(strings.Join(append([]string{event.Result}, event.Errors...), " "))
			if decoded.diagnostic == "" {
				decoded.diagnostic = "claude reported " + event.Subtype
			}
		}
	}
	return nil
}
