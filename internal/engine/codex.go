package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type codexAdapter struct{}

func (codexAdapter) Name() string { return "codex" }

func (codexAdapter) Check(_ context.Context, candidate reviewerCandidate) availability {
	if err := validateCodexCandidate(candidate); err != nil {
		return availability{Diagnostic: err.Error()}
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return availability{Diagnostic: "codex is not installed"}
	}
	return availability{Available: true}
}

func (codexAdapter) Prepare(spec attemptSpec) (preparedAttempt, error) {
	arguments := codexCommand(spec.Candidate, spec.Repository)
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Dir = spec.Repository
	command.Stdin = strings.NewReader(spec.Prompt)
	command.Env = reviewerEnvironment(spec.Candidate.ID)
	return preparedAttempt{command: command}, nil
}

func (codexAdapter) Decode(output []byte) (decodedHarnessOutput, error) {
	decoded, err := decodeCodexOutput(output)
	return decodedHarnessOutput{assistantText: decoded.assistantText, diagnostic: decoded.diagnostic, noise: strings.Join(decoded.noise, "\n")}, err
}

func codexCommand(candidate reviewerCandidate, repository string) []string {
	command := []string{"codex", "exec", "--json", "--skip-git-repo-check", "--ephemeral", "-C", repository, "-s", "read-only", "-m", candidate.Model}
	if usesCodexEffort(candidate) {
		command = append(command, "-c", fmt.Sprintf("model_reasoning_effort=%q", candidate.Effort))
	}
	return command
}

func validateCodexCandidate(candidate reviewerCandidate) error {
	if candidate.Effort == "auto" {
		return ReviewerEffortNotSupportedError{Reviewer: candidate.ID, Model: candidate.Model, Effort: candidate.Effort}
	}
	return nil
}

func usesCodexEffort(candidate reviewerCandidate) bool {
	if candidate.Effort == "" {
		return false
	}
	if candidate.Effort == "default" {
		return false
	}
	return candidate.Effort != "auto"
}

type decodedCodexOutput struct {
	assistantText string
	diagnostic    string
	noise         []string
}

func (decoded *decodedCodexOutput) report(message string) {
	if message == "" || message == decoded.diagnostic {
		return
	}
	if isCodexTransportNoise(message) {
		decoded.noise = append(decoded.noise, message)
		return
	}
	if decoded.diagnostic != "" {
		decoded.noise = append(decoded.noise, decoded.diagnostic)
	}
	decoded.diagnostic = message
}

func isCodexTransportNoise(message string) bool {
	normalized := strings.ToLower(message)
	return strings.HasPrefix(normalized, "reconnecting...") ||
		strings.HasPrefix(normalized, "falling back from websockets") ||
		strings.Contains(normalized, "failed to connect to websocket")
}

type codexEvent struct {
	Type string `json:"type"`
	Item *struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Message string `json:"message"`
	} `json:"item,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Message string `json:"message"`
}

func decodeCodexOutput(output []byte) (decodedCodexOutput, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var decoded decodedCodexOutput
	var text strings.Builder
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var event codexEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return decodedCodexOutput{}, fmt.Errorf("decode codex event: %w", err)
		}
		applyCodexEvent(&decoded, &text, event)
	}
	if err := scanner.Err(); err != nil {
		return decodedCodexOutput{}, fmt.Errorf("scan codex output: %w", err)
	}
	decoded.assistantText = strings.TrimSpace(text.String())
	return decoded, nil
}

func applyCodexEvent(decoded *decodedCodexOutput, text *strings.Builder, event codexEvent) {
	switch event.Type {
	case "item.completed":
		if event.Item == nil {
			return
		}
		switch event.Item.Type {
		case "agent_message":
			if event.Item.Text == "" {
				return
			}
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(event.Item.Text)
		case "error":
			decoded.report(event.Item.Message)
		}
	case "error":
		if event.Message != "" {
			decoded.report(event.Message)
		} else if event.Error != nil {
			decoded.report(event.Error.Message)
		}
	case "turn.failed":
		if event.Error != nil {
			decoded.report(event.Error.Message)
		}
	}
}
