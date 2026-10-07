package store

// BackupDirectory is the state subdirectory that holds backed-up ledgers.
const BackupDirectory = "backups"
const recoveryMarker = ".recovery-pending"

// ledgerSuffixes name the ledger and every SQLite sidecar that moves with it.
var ledgerSuffixes = []string{"", "-wal", "-shm", "-journal"}

// IsLedgerFile reports whether name, a state directory entry, is the ledger,
// one of its sidecars, or its pending-recovery marker.
func IsLedgerFile(name string) bool {
	if name == recoveryMarker {
		return true
	}
	for _, suffix := range ledgerSuffixes {
		if name == ledgerFilename+suffix {
			return true
		}
	}
	return false
}
