package reviewparty

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

const openCodeReviewConfig = `{"permission":{"*":"deny","read":"allow","glob":"allow","grep":"allow","list":"allow"},"share":"disabled"}`

type openCodeAdapter struct{}

func (openCodeAdapter) Name() string { return "opencode" }

func (openCodeAdapter) Check(_ context.Context, _ reviewerCandidate) availability {
	if _, err := exec.LookPath("opencode"); err != nil {
		return availability{Diagnostic: "opencode is not installed"}
	}
	return availability{Available: true}
}

func (openCodeAdapter) Prepare(spec attemptSpec) (preparedAttempt, error) {
	arguments := openCodeCommand(spec.Candidate)
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Dir = spec.Repository
	command.Stdin = strings.NewReader(spec.Prompt)
	command.Env = append(os.Environ(),
		"OPENCODE_CONFIG_CONTENT="+openCodeReviewConfig,
		"OPENCODE_DISABLE_AUTOUPDATE=true",
	)
	return preparedAttempt{command: command}, nil
}

func (openCodeAdapter) Decode(output []byte) (decodedHarnessOutput, error) {
	decoded, err := decodeOpenCodeOutput(output)
	return decodedHarnessOutput{assistantText: decoded.assistantText, diagnostic: decoded.diagnostic}, err
}

func openCodeCommand(candidate reviewerCandidate) []string {
	command := []string{"opencode", "run", "--pure", "--agent", "build", "--format", "json", "--model", candidate.Model}
	if candidate.Effort != "" && candidate.Effort != "default" && candidate.Effort != "auto" {
		command = append(command, "--variant", candidate.Effort)
	}
	return command
}

type decodedOpenCodeOutput struct {
	assistantText string
	diagnostic    string
}

func decodeOpenCodeOutput(output []byte) (decodedOpenCodeOutput, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var decoded decodedOpenCodeOutput
	var text strings.Builder
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Part struct {
				Text string `json:"text"`
			} `json:"part"`
			Error struct {
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			} `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return decodedOpenCodeOutput{}, fmt.Errorf("decode opencode event: %w", err)
		}
		if event.Type == "text" {
			text.WriteString(event.Part.Text)
		}
		if event.Type == "error" {
			decoded.diagnostic = event.Error.Data.Message
		}
	}
	if err := scanner.Err(); err != nil {
		return decodedOpenCodeOutput{}, fmt.Errorf("scan opencode output: %w", err)
	}
	decoded.assistantText = text.String()
	return decoded, nil
}
