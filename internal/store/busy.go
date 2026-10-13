package store

import (
	"errors"
	"math/rand/v2"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ledgerBusyTimeoutMS is how long one connection waits inside SQLite for
// another process to release the write lock before reporting SQLITE_BUSY.
const ledgerBusyTimeoutMS = 10000

// busyRetryAttempts bounds whole-transaction retries after SQLite's own busy
// wait has expired, for example when many Review Party processes finish at once.
const busyRetryAttempts = 5

var busyRetryBackoff = 50 * time.Millisecond

// retryBusy reruns a write transaction that failed only because another
// connection held the ledger lock. A failed transaction rolls back whole, so
// the rerun starts from the same ledger state.
func retryBusy[T any](write func() (T, error)) (T, error) {
	backoff := busyRetryBackoff
	for range busyRetryAttempts - 1 {
		value, err := write()
		if !ledgerBusy(err) {
			return value, err
		}
		time.Sleep(backoff + rand.N(backoff))
		backoff *= 2
	}
	return write()
}

func retryBusyWrite(write func() error) error {
	_, err := retryBusy(func() (struct{}, error) { return struct{}{}, write() })
	return err
}

func ledgerBusy(err error) bool {
	var failure *sqlite.Error
	if !errors.As(err, &failure) {
		return false
	}
	primary := failure.Code() & 0xff
	return primary == sqlite3.SQLITE_BUSY || primary == sqlite3.SQLITE_LOCKED
}
