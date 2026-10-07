//go:build windows

package hostrun

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Handles opened by os.OpenFile are non-inheritable, so no child can keep a
// dead owner's lease locked. The lock covers byte 0 of the empty file.
func (l *lockFile) tryLock() (bool, error) {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(windows.Handle(l.file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return false, err
}
