package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reviewparty/internal/model"
	"strings"
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
	ID              model.ReviewID            `json:"id"`
	Lifecycle       model.Lifecycle           `json:"lifecycle"`
	Repository      string                    `json:"repository"`
	Subject         string                    `json:"subject"`
	Profile         string                    `json:"profile"`
	Reviewer        string                    `json:"reviewer"`
	Termination     model.TerminationCategory `json:"termination,omitempty"`
	ReplaysReviewID *model.ReviewID           `json:"replays_review_id,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
}

const (
	DefaultHistoryLimit = 20
	MaxHistoryLimit     = 200
)

type HistoryQuery struct {
	Repository  string
	Reviewer    string
	Profile     string
	Lifecycle   model.Lifecycle
	Termination model.TerminationCategory
	Subject     string
	Since       *time.Time
	Limit       int
}

type HistoryPage struct {
	Entries []HistoryEntry `json:"entries"`
	Limit   int            `json:"limit"`
	HasMore bool           `json:"has_more"`
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

func (s *DeferredLedgerRecordStore) History(query HistoryQuery) (HistoryPage, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return HistoryPage{}, err
	}
	return ledger.History(query)
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
	const current = 3
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
	const current = 3
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
	for version < current {
		if err := applyKnownMigration(tx, version+1); err != nil {
			return err
		}
		version++
	}
	return tx.Commit()
}

func applyKnownMigration(tx *sql.Tx, version int) error {
	paths := map[int]string{1: "migrations/001_initial.sql", 2: "migrations/002_history_indexes.sql", 3: "migrations/003_replay_lineage.sql"}
	path, exists := paths[version]
	if !exists {
		return fmt.Errorf("no migration for review ledger schema %d", version)
	}
	return applyMigration(tx, version, path)
}

func currentMigrationVersion(tx *sql.Tx) (int, error) {
	var version int
	if err := tx.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func applyMigration(tx *sql.Tx, version int, path string) error {
	result, err := tx.Exec("INSERT OR IGNORE INTO schema_migrations(version) VALUES(?)", version)
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
	payload, err := migrationFiles.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(string(payload)); err != nil {
		return fmt.Errorf("apply review ledger migration %d: %w", version, err)
	}
	return nil
}

func (s *LedgerRecordStore) Save(record model.ReviewRecord) error {
	return s.projection.save(record)
}

func (s *LedgerRecordStore) Load(id model.ReviewID) (model.ReviewRecord, error) {
	return s.projection.load(id)
}

func (s *LedgerRecordStore) History(query HistoryQuery) (HistoryPage, error) {
	statement, arguments, limit, err := buildHistoryQuery(query)
	if err != nil {
		return HistoryPage{}, err
	}
	rows, err := s.db.Query(statement, arguments...)
	if err != nil {
		return HistoryPage{}, err
	}
	defer rows.Close()
	return scanHistoryPage(rows, limit)
}

func buildHistoryQuery(query HistoryQuery) (string, []any, int, error) {
	limit := query.Limit
	if limit == 0 {
		limit = DefaultHistoryLimit
	}
	if limit < 1 || limit > MaxHistoryLimit {
		return "", nil, 0, fmt.Errorf("history limit must be between 1 and %d", MaxHistoryLimit)
	}
	statement := `SELECT id,lifecycle,
		json_extract(subject,'$.repository'),json_extract(subject,'$.identity'),
		json_extract(profile_revision,'$.name'),json_extract(profile_revision,'$.reviewer_id'),
		COALESCE(json_extract(termination,'$.category'),''),created_at,replays_review_id FROM reviews`
	var predicates []string
	var arguments []any
	filters := historyFilters(query)
	for _, filter := range filters {
		if filter.enabled {
			predicates = append(predicates, filter.predicate)
			arguments = append(arguments, filter.value)
		}
	}
	if len(predicates) > 0 {
		statement += " WHERE " + strings.Join(predicates, " AND ")
	}
	statement += " ORDER BY created_at DESC,id DESC LIMIT ?"
	arguments = append(arguments, limit+1)
	return statement, arguments, limit, nil
}

type historyFilter struct {
	predicate string
	value     any
	enabled   bool
}

func historyFilters(query HistoryQuery) []historyFilter {
	var since any
	if query.Since != nil {
		since = query.Since.UTC()
	}
	return []historyFilter{
		{"json_extract(subject,'$.repository') = ?", query.Repository, query.Repository != ""},
		{"json_extract(profile_revision,'$.reviewer_id') = ?", query.Reviewer, query.Reviewer != ""},
		{"json_extract(profile_revision,'$.name') = ?", query.Profile, query.Profile != ""},
		{"lifecycle = ?", query.Lifecycle, query.Lifecycle != ""},
		{"json_extract(termination,'$.category') = ?", query.Termination, query.Termination != ""},
		{"json_extract(subject,'$.identity') = ?", query.Subject, query.Subject != ""},
		{"created_at >= ?", since, query.Since != nil},
	}
}

func scanHistoryPage(rows *sql.Rows, limit int) (HistoryPage, error) {
	page := HistoryPage{Entries: []HistoryEntry{}, Limit: limit}
	for rows.Next() {
		var entry HistoryEntry
		if err := rows.Scan(&entry.ID, &entry.Lifecycle, &entry.Repository, &entry.Subject, &entry.Profile, &entry.Reviewer, &entry.Termination, &entry.CreatedAt, &entry.ReplaysReviewID); err != nil {
			return HistoryPage{}, err
		}
		page.Entries = append(page.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return HistoryPage{}, err
	}
	if len(page.Entries) > limit {
		page.HasMore = true
		page.Entries = page.Entries[:limit]
	}
	return page, nil
}
