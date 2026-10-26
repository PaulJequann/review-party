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
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		journey.runWithContext(t, ctx, "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCheckpointJourneyRejectsAnExhaustedScript$", "-test.v")
	child.Env = append(os.Environ(), "REVIEW_PARTY_EXHAUST_CHECKPOINT_SCRIPT=1")
	output, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("exhausted script silently passed:\n%s", output)
	}
	for _, want := range []string{
		"RunFirstUse: <nil>; context: context deadline exceeded",
		"What should the Checkpoint require?",
		"Setup stopped.",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("missing diagnostic %q in child failure %v:\n%s", want, err, output)
		}
	}
}
