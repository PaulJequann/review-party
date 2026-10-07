//go:build unix

package hostrun

import (
	"errors"
	"syscall"
)

// os.OpenFile sets O_CLOEXEC, so no child inherits the descriptor and no
// Reviewer can keep a dead owner's lease locked.
func (l *lockFile) tryLock() (bool, error) {
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}
