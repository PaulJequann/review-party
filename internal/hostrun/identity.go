package hostrun

import (
	"errors"
	"syscall"
	"time"
)

var errProcessGone = errors.New("process is gone")

const killedExitCode = 128 + int(syscall.SIGKILL)

func (id Identity) sameAs(current Identity) bool {
	return current.Start == id.Start && sameBoot(current.Boot, id.Boot)
}

func (id Identity) alive() bool {
	current, err := identify(id.PID)
	return err == nil && id.sameAs(current)
}

func (id Identity) awaitExit(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for id.alive() && time.Now().Before(deadline) {
		time.Sleep(exitPoll)
	}
}

const exitPoll = 20 * time.Millisecond
