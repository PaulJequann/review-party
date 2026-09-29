package configurationhub

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"charm.land/huh/v2"
)

const accessibleSelectEOFChild = "REVIEWPARTY_ACCESSIBLE_SELECT_EOF_CHILD"

// DEV-115: huh's accessible Select keeps a rejected answer as the input when
// the reader then returns EOF, so it indexes options[-1] and the whole process
// dies. The accessible adapter must survive that sequence; whether it returns
// an error or a default is the fix's decision, so the child only has to exit
// cleanly.
func TestAccessibleSelectSurvivesRejectedAnswerThenEOF(t *testing.T) {
	if os.Getenv(accessibleSelectEOFChild) == "1" {
		var value string
		editor := editor{RunOptions: RunOptions{Context: context.Background(), Input: newLineInput("x\n"), Output: io.Discard, Accessible: true}}
		if err := editor.form(huh.NewSelect[string]().Title("Model").Options(huh.NewOption("alpha", "alpha"), huh.NewOption("beta", "beta")).Value(&value)); err != nil {
			t.Logf("form returned %v", err)
		}
		return
	}
	command := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestAccessibleSelectSurvivesRejectedAnswerThenEOF$")
	command.Env = append(os.Environ(), accessibleSelectEOFChild+"=1")
	output, err := command.CombinedOutput()
	if err != nil || strings.Contains(string(output), "panic:") {
		t.Fatalf("accessible Select with a rejected answer then EOF crashed: %v\n%s", err, output)
	}
}
