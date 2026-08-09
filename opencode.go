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

type openCodeExecutor struct{}

func (openCodeExecutor) Check(_ context.Context, _ reviewerCandidate) availability {
	if _, err := exec.LookPath("opencode"); err != nil {
		return availability{Diagnostic: "opencode is not installed"}
	}
	return availability{Available: true}
}

func (openCodeExecutor) Execute(ctx context.Context, spec attemptSpec) attemptExecution {
	arguments := openCodeCommand(spec.Candidate)
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Dir = spec.Repository
	command.Stdin = strings.NewReader(spec.Prompt)
	command.Env = append(os.Environ(),
		"OPENCODE_CONFIG_CONTENT="+openCodeReviewConfig,
		"OPENCODE_DISABLE_AUTOUPDATE=true",
	)
	run := runCommand(ctx, command)
	if run.StartErr != nil {
		return finalizeHarnessRun(run, "", run.Stderr, "", "", "opencode")
	}
	if run.OutputOverflow {
		return overflowExecution(run, "opencode")
	}
	decoded, err := decodeOpenCodeOutput(run.Stdout)
	if err != nil {
		return decodedRunFailure(run, err, "opencode")
	}
	diagnostic := strings.TrimSpace(strings.Join([]string{decoded.diagnostic, run.Stderr}, " "))
	return finalizeHarnessRun(run, decoded.assistantText, diagnostic, spec.Candidate.Model, spec.Candidate.Effort, "opencode")
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
