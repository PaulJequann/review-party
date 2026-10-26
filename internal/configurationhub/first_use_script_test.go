package configurationhub

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCheckpointJourneyRejectsAnExhaustedScript(t *testing.T) {
	if os.Getenv("REVIEW_PARTY_EXHAUST_CHECKPOINT_SCRIPT") == "1" {
		journey := newCheckpointJourney(t, "bugs")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		input := newHoldingInput("")
		input.onExhausted = cancel
		journey.runWithContext(t, ctx, input)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCheckpointJourneyRejectsAnExhaustedScript$", "-test.v")
	child.Env = append(os.Environ(), "REVIEW_PARTY_EXHAUST_CHECKPOINT_SCRIPT=1")
	output, err := child.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("fixture child exceeded hang guard: %v\n%s", ctx.Err(), output)
	}
	if err == nil {
		t.Fatalf("exhausted script silently passed:\n%s", output)
	}
	for _, want := range []string{
		"context: context canceled",
		"What should the Checkpoint require?",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("missing diagnostic %q in child failure %v:\n%s", want, err, output)
		}
	}
}
