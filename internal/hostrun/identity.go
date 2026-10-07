package hostrun

import (
	"errors"
	"syscall"
)

var errProcessGone = errors.New("process is gone")

const killedExitCode = 128 + int(syscall.SIGKILL)

func (id Identity) sameAs(current Identity) bool {
	return current.Start == id.Start && sameBoot(current.Boot, id.Boot)
}
