package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reviewparty/internal/model"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const ledgerFilename = "ledger.sqlite"

var ErrReviewRecordStateNotInitialized = errors.New("Review Party is not initialized")

//go:embed migrations/*.sql
var migrationFiles embed.FS

type RecordStore interface {
	Save(model.ReviewRecord) error
	Load(model.ReviewID) (model.ReviewRecord, error)
}

type HistoryEntry struct {
	ID        model.ReviewID  `json:"id"`
	Lifecycle model.Lifecycle `json:"lifecycle"`
	CreatedAt time.Time       `json:"created_at"`
}

type LedgerRecordStore struct {
	db         *sql.DB
	directory  string
	projection reviewRecordProjection
}

// DeferredLedgerRecordStore preserves read-only commands: their construction
// does not create a state directory or open a database.
type DeferredLedgerRecordStore struct {
	directory string
	mu        sync.Mutex
	ledger    *LedgerRecordStore
	err       error
}

func NewDeferredLedgerRecordStore(directory string) (*DeferredLedgerRecordStore, error) {
	if directory == "" {
		return nil, errors.New("review state directory is required")
	}
	return &DeferredLedgerRecordStore{directory: directory}, nil
}

func (s *DeferredLedgerRecordStore) Save(record model.ReviewRecord) error {
	ledger, err := s.openExisting()
	if err != nil {
		return err
	}
	return ledger.Save(record)
}

func (s *DeferredLedgerRecordStore) RequirePrepared() error {
	_, err := s.openExisting()
	return err
}

// PrepareReviewRecordState creates or upgrades the managed ledger explicitly.
// Ordinary Review, inspect, and history paths never call this operation.
func PrepareReviewRecordState(directory string) error {
	ledger, err := NewLedgerRecordStore(directory)
	if err != nil {
		return err
	}
	return ledger.Close()
}

// ReviewRecordStatePrepared reports whether a usable current ledger exists.
// It does not create, migrate, repair, or change permissions on state.
func ReviewRecordStatePrepared(directory string) (bool, error) {
	ledgerPath := filepath.Join(directory, ledgerFilename)
	if _, err := os.Stat(ledgerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("access review state at %q: %w", directory, err)
	}
	ledger, err := openLedgerRecordStore(directory, false)
	if err != nil {
		return false, err
	}
	if err := ledger.Close(); err != nil {
		return false, fmt.Errorf("close review ledger: %w", err)
	}
	return true, nil
}

func (s *DeferredLedgerRecordStore) Load(id model.ReviewID) (model.ReviewRecord, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return model.ReviewRecord{}, err
	}
	return ledger.Load(id)
}

func (s *DeferredLedgerRecordStore) History(limit int) ([]HistoryEntry, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.History(limit)
}

func (s *DeferredLedgerRecordStore) openExisting() (*LedgerRecordStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ledger != nil {
		return s.ledger, nil
	}
	ledgerPath := filepath.Join(s.directory, ledgerFilename)
	if _, err := os.Stat(ledgerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w at %q", ErrReviewRecordStateNotInitialized, s.directory)
		}
		return nil, fmt.Errorf("access review state at %q: %w", s.directory, err)
	}
	s.ledger, s.err = openLedgerRecordStore(s.directory, false)
	if s.err != nil {
		s.ledger = nil
	}
	return s.ledger, s.err
}

func NewLedgerRecordStore(directory string) (*LedgerRecordStore, error) {
	return openLedgerRecordStore(directory, true)
}

func openLedgerRecordStore(directory string, prepare bool) (*LedgerRecordStore, error) {
	if directory == "" {
		return nil, errors.New("review state directory is required")
	}
	if err := prepareLedgerDirectory(directory, prepare); err != nil {
		return nil, err
	}
	if !prepare {
		if err := validateExistingLedger(filepath.Join(directory, ledgerFilename)); err != nil {
			return nil, err
		}
	}
	db, err := openLedgerDatabase(directory, prepare)
	if err != nil {
		return nil, err
	}
	// Reads reconstruct nested rows while their parent cursor is still open, so
	// they need a second pooled connection. SQLite's busy timeout serializes the
	// supported concurrent writer pattern.
	db.SetMaxOpenConns(4)
	store := &LedgerRecordStore{db: db, directory: directory, projection: reviewRecordProjection{db: db}}
	if err := store.initialize(prepare); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func openLedgerDatabase(directory string, prepare bool) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(directory, ledgerFilename)) + "?_pragma=foreign_keys%3Don&_pragma=busy_timeout%3D5000"
	if !prepare {
		dsn += "&mode=rw"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open review ledger: %w", err)
	}
	return db, nil
}

func (s *LedgerRecordStore) initialize(prepare bool) error {
	if !prepare {
		return s.requirePreparedSchema()
	}
	if err := s.configure(); err != nil {
		return err
	}
	if err := s.migrate(); err != nil {
		return err
	}
	return s.restrictPermissions()
}

func (s *LedgerRecordStore) requirePreparedSchema() error {
	var version int
	if err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("read review ledger schema: %w", err)
	}
	const current = 1
	if version > current {
		return fmt.Errorf("review ledger schema %d is newer than supported schema %d", version, current)
	}
	if version < current {
		return fmt.Errorf("review ledger schema %d requires state preparation for schema %d", version, current)
	}
	return nil
}

func (s *LedgerRecordStore) restrictPermissions() error {
	if err := os.Chmod(s.directory, 0o700); err != nil {
		return fmt.Errorf("restrict review state directory permissions: %w", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := filepath.Join(s.directory, ledgerFilename+suffix)
		if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restrict review ledger permissions: %w", err)
		}
	}
	return nil
}

func (s *LedgerRecordStore) Close() error { return s.db.Close() }

func (s *LedgerRecordStore) configure() error {
	for _, statement := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL", "PRAGMA synchronous = FULL"} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("configure review ledger: %w", err)
		}
	}
	return nil
}

func (s *LedgerRecordStore) migrate() error {
	if _, err := s.db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)"); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	const current = 1
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	version, err := currentMigrationVersion(tx)
	if err != nil {
		return err
	}
	if version > current {
		return fmt.Errorf("review ledger schema %d is newer than supported schema %d", version, current)
	}
	if version == current {
		return tx.Commit()
	}
	if err := applyInitialMigration(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func currentMigrationVersion(tx *sql.Tx) (int, error) {
	var version int
	if err := tx.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func applyInitialMigration(tx *sql.Tx) error {
	result, err := tx.Exec("INSERT OR IGNORE INTO schema_migrations(version) VALUES(1)")
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		return nil
	}
	payload, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(string(payload)); err != nil {
		return fmt.Errorf("apply review ledger migration 1: %w", err)
	}
	return nil
}

func (s *LedgerRecordStore) Save(record model.ReviewRecord) error {
	return s.projection.save(record)
}

func (s *LedgerRecordStore) Load(id model.ReviewID) (model.ReviewRecord, error) {
	return s.projection.load(id)
}

func (s *LedgerRecordStore) History(limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query("SELECT id,lifecycle,created_at FROM reviews ORDER BY created_at DESC,id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []HistoryEntry
	for rows.Next() {
		var entry HistoryEntry
		if err := rows.Scan(&entry.ID, &entry.Lifecycle, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
