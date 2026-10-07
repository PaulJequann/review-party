package hostrun

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	rootLockTimeout  = 10 * time.Second
	lockPollInterval = 10 * time.Millisecond
)

var errRootLockTimeout = errors.New("the runtime root lock was held for more than " + rootLockTimeout.String())

// The kernel releases the lock when the holder dies.
type lockFile struct {
	file *os.File
}

func openLockFile(path string, create bool) (*lockFile, error) {
	flag := os.O_RDWR
	if create {
		flag |= os.O_CREATE
	}
	file, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		return nil, err
	}
	return &lockFile{file: file}, nil
}

func (l *lockFile) lock(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		locked, err := l.tryLock()
		if err != nil || locked {
			return err
		}
		if time.Now().After(deadline) {
			return errRootLockTimeout
		}
		time.Sleep(lockPollInterval)
	}
}

func (l *lockFile) close() error {
	return l.file.Close()
}

func lockRoot(root string) (*lockFile, error) {
	lock, err := openLockFile(filepath.Join(root, rootLockName), true)
	if err != nil {
		return nil, err
	}
	if err := lock.lock(rootLockTimeout); err != nil {
		closeLease(lock)
		return nil, err
	}
	return lock, nil
}

func closeLease(lease *lockFile) {
	if lease != nil {
		_ = lease.close() //nolint:errcheck // The file is the lease; a failed close changes nothing the caller can act on.
	}
}
