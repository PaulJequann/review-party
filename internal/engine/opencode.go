package engine

import (
	"context"
	"os/exec"
	"strings"
)

const openCodeReviewConfig = `{"permission":{"*":"deny","read":"allow","glob":"allow","grep":"allow","list":"allow"},"share":"disabled"}`

type openCodeAdapter struct{}

func (openCodeAdapter) Name() string { return "opencode" }

func (openCodeAdapter) Check(_ context.Context, candidate reviewerCandidate) availability {
	if err := validateOpenCodeCandidate(candidate); err != nil {
		return availability{Diagnostic: err.Error()}
	}
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
	command.Env = reviewerEnvironment(spec.Candidate.ID,
		"OPENCODE_CONFIG_CONTENT="+openCodeReviewConfig,
		"OPENCODE_DISABLE_AUTOUPDATE=true",
	)
	return preparedAttempt{command: command}, nil
}

func (openCodeAdapter) Decode(output []byte, _ string) (decodedHarnessOutput, error) {
	decoded, err := decodeOpenCodeOutput(output)
	return decodedHarnessOutput{assistantText: decoded.assistantText, diagnostic: decoded.diagnostic}, err
}

func openCodeCommand(candidate reviewerCandidate) []string {
	command := []string{"opencode", "run", "--pure", "--agent", "build", "--format", "json", "--model", candidate.Model}
	if usesOpenCodeVariant(candidate) {
		command = append(command, "--variant", candidate.Effort)
	}
	return command
}

func validateOpenCodeCandidate(candidate reviewerCandidate) error {
	if candidate.Effort == "auto" {
		return ReviewerEffortNotSupportedError{Reviewer: candidate.ID, Model: candidate.Model, Effort: candidate.Effort}
	}
	return nil
}

func usesOpenCodeVariant(candidate reviewerCandidate) bool {
	if candidate.Effort == "" {
		return false
	}
	if candidate.Effort == "default" {
		return false
	}
	return candidate.Effort != "auto"
}

type decodedOpenCodeOutput struct {
	assistantText string
	diagnostic    string
}

type openCodeEvent struct {
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

func decodeOpenCodeOutput(output []byte) (decodedOpenCodeOutput, error) {
	var decoded decodedOpenCodeOutput
	var text strings.Builder
	if err := scanJSONLines[openCodeEvent](output, "opencode", func(event openCodeEvent) {
		applyOpenCodeEvent(&decoded, &text, event)
	}); err != nil {
		return decodedOpenCodeOutput{}, err
	}
	decoded.assistantText = text.String()
	return decoded, nil
}

func applyOpenCodeEvent(decoded *decodedOpenCodeOutput, text *strings.Builder, event openCodeEvent) {
	switch event.Type {
	case "text":
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(event.Part.Text)
	case "error":
		decoded.diagnostic = event.Error.Data.Message
	}
}
