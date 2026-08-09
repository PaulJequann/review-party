package reviewparty

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type copilotAdapter struct{}

func (copilotAdapter) Name() string { return "copilot" }

func (copilotAdapter) Check(_ context.Context, _ reviewerCandidate) availability {
	if _, err := exec.LookPath("copilot"); err != nil {
		return availability{Diagnostic: "copilot is not installed"}
	}
	return availability{Available: true}
}

func (copilotAdapter) Prepare(spec attemptSpec) (preparedAttempt, error) {
	arguments := copilotCommand(spec.Candidate)
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Dir = spec.Repository
	command.Stdin = strings.NewReader(spec.Prompt)
	return preparedAttempt{command: command}, nil
}

func (copilotAdapter) Decode(output []byte) (decodedHarnessOutput, error) {
	decoded, err := decodeCopilotOutput(output)
	return decodedHarnessOutput{
		assistantText: decoded.assistantText,
		model:         decoded.model,
		effort:        decoded.effort,
	}, err
}

func copilotCommand(candidate reviewerCandidate) []string {
	command := []string{
		"copilot",
		"--available-tools=view,grep,glob",
		"--deny-tool=shell",
		"--deny-tool=write",
		"--deny-tool=url",
		"--disable-builtin-mcps",
		"--no-ask-user",
		"--allow-all-tools",
		"--no-color",
		"--output-format=json",
		"--model=" + candidate.Model,
	}
	if candidate.Model != "auto" && candidate.Effort != "" && candidate.Effort != "auto" {
		command = append(command, "--effort="+candidate.Effort)
	}
	return command
}

type decodedCopilotOutput struct {
	assistantText string
	model         string
	effort        string
}

func decodeCopilotOutput(output []byte) (decodedCopilotOutput, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var decoded decodedCopilotOutput
	var chunks strings.Builder
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Data struct {
				Content         string `json:"content"`
				Model           string `json:"model"`
				ChosenModel     string `json:"chosenModel"`
				ReasoningBucket string `json:"reasoningBucket"`
			} `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return decodedCopilotOutput{}, fmt.Errorf("decode copilot event: %w", err)
		}
		if event.Type == "assistant.message" {
			chunks.WriteString(event.Data.Content)
			if event.Data.Model != "" {
				decoded.model = event.Data.Model
			}
		}
		if event.Type == "session.auto_mode_resolved" {
			decoded.model = event.Data.ChosenModel
			decoded.effort = event.Data.ReasoningBucket
		}
	}
	if err := scanner.Err(); err != nil {
		return decodedCopilotOutput{}, fmt.Errorf("scan copilot output: %w", err)
	}
	decoded.assistantText = chunks.String()
	return decoded, nil
}

func classifyCopilotFailure(diagnostic string, waitErr error) attemptExecution {
	return classifyHarnessFailure(diagnostic, waitErr)
}

func compactDiagnostic(value string) string {
	fields := strings.Fields(value)
	if len(fields) > 40 {
		fields = fields[len(fields)-40:]
	}
	return strings.Join(fields, " ")
}
