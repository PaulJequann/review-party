package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type grokAdapter struct{}

const grokReviewMaxTurns = "30"

func (grokAdapter) Name() string { return "grok" }

func (grokAdapter) Check(_ context.Context, _ reviewerCandidate) availability {
	if _, err := exec.LookPath("grok"); err != nil {
		return availability{Diagnostic: "grok is not installed"}
	}
	return availability{Available: true}
}

func (grokAdapter) Prepare(spec attemptSpec) (preparedAttempt, error) {
	promptPath, err := writePromptFile(spec.Prompt)
	if err != nil {
		return preparedAttempt{}, err
	}
	arguments := grokCommand(spec.Candidate, spec.Repository, promptPath)
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Dir = spec.Repository
	return preparedAttempt{command: command, cleanup: func() { _ = os.Remove(promptPath) }}, nil
}

func (grokAdapter) Decode(output []byte) (decodedHarnessOutput, error) {
	assistantText, err := decodeGrokOutput(output)
	return decodedHarnessOutput{assistantText: assistantText}, err
}

func grokCommand(candidate reviewerCandidate, repository, promptPath string) []string {
	return []string{
		"grok",
		"--prompt-file", promptPath,
		"--cwd", repository,
		"--model", candidate.Model,
		"--reasoning-effort", candidate.Effort,
		"--tools", "view,grep,glob",
		"--disable-web-search",
		"--no-subagents",
		"--no-memory",
		"--no-plan",
		"--permission-mode", "dontAsk",
		"--max-turns", grokReviewMaxTurns,
		"--output-format", "streaming-json",
		"--verbatim",
	}
}

func decodeGrokOutput(output []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var text strings.Builder
	inText := false
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return "", fmt.Errorf("decode grok event: %w", err)
		}
		if event.Type != "text" {
			inText = false
			continue
		}
		if !inText && text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(event.Data)
		inText = true
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan grok output: %w", err)
	}
	return strings.TrimSpace(text.String()), nil
}

func writePromptFile(prompt string) (string, error) {
	file, err := os.CreateTemp("", "review-party-prompt-*.txt")
	if err != nil {
		return "", fmt.Errorf("create grok prompt file: %w", err)
	}
	path := file.Name()
	if _, err := file.WriteString(prompt); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("write grok prompt file: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close grok prompt file: %w", err)
	}
	return path, nil
}
