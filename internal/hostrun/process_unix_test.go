//go:build unix

package hostrun

import (
	"syscall"
	"testing"
	"time"
)

func TestStopTerminatesAWillingReviewerWithoutEscalating(t *testing.T) {
	r := openRun(t, testRoot(t), nil)
	p := startHelper(t, r, helperCommand("sleep"))
	const grace = 5 * time.Second
	began := time.Now()
	if err := p.Stop(grace); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(began); elapsed >= grace {
		t.Fatalf("Stop took %s; the Reviewer should have left on terminate", elapsed)
	}
	if code, want := exitCode(t, p.Wait()), 128+int(syscall.SIGTERM); code != want {
		t.Fatalf("terminated Reviewer exited %d; want %d", code, want)
	}
}
