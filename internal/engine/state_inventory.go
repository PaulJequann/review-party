package engine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"reviewparty/internal/artifact"
	"reviewparty/internal/hostrun"
	"reviewparty/internal/store"
)

// StateKind classifies one entry of the durable state directory.
type StateKind string

const (
	// StateLedger is the review ledger, a SQLite sidecar, or its
	// pending-recovery marker.
	StateLedger StateKind = "ledger"
	// StateBackups holds ledgers moved aside by `init --backup`.
	StateBackups StateKind = "backups"
	// StateArtifacts holds published Reviewer output. Its size excludes
	// partial writes, which are listed on their own.
	StateArtifacts StateKind = "artifacts"
	// StatePartialArtifact is an artifact write a process never published.
	StatePartialArtifact StateKind = "partial-artifact"
	// StateUnrecognized is anything Review Party did not write.
	StateUnrecognized StateKind = "unrecognized"
)

// StateEntry is one thing in the state directory.
type StateEntry struct {
	Path  string
	Kind  StateKind
	Bytes int64
	Err   error
}

// InventoryState lists the state directory without opening the ledger. A
// missing directory has no entries.
func InventoryState(directory string) ([]StateEntry, error) {
	names, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []StateEntry
	for _, name := range names {
		entry := sizedStateEntry(filepath.Join(directory, name.Name()), stateKind(name))
		if entry.Kind == StateArtifacts {
			partials, err := partialArtifacts(entry.Path)
			if entry.Err == nil {
				entry.Err = err
			}
			for _, partial := range partials {
				entry.Bytes -= partial.Bytes
			}
			entries = append(entries, partials...)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func stateKind(entry fs.DirEntry) StateKind {
	switch {
	case store.IsLedgerFile(entry.Name()) && !entry.IsDir():
		return StateLedger
	case entry.Name() == store.BackupDirectory && entry.IsDir():
		return StateBackups
	case entry.Name() == artifact.Directory && entry.IsDir():
		return StateArtifacts
	default:
		return StateUnrecognized
	}
}

func sizedStateEntry(path string, kind StateKind) StateEntry {
	bytes, err := hostrun.DiskUsage(path)
	return StateEntry{Path: path, Kind: kind, Bytes: bytes, Err: err}
}

func partialArtifacts(directory string) ([]StateEntry, error) {
	var partials []StateEntry
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if artifact.IsTemporary(entry) {
			partials = append(partials, sizedStateEntry(path, StatePartialArtifact))
		}
		return nil
	})
	return partials, err
}
