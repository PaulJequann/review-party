// Package store persists review records and evaluation state.
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

// currentLedgerSchemaVersion is the newest step in ledgerMigrations. State
// preparation upgrades a ledger at schema 10 or later additively, preserving its
// records, and replaces anything older without preserving it. The numbering
// continued past the last released migration so no obsolete ledger can collide.
const currentLedgerSchemaVersion = 13

var ledgerMigrations = []struct {
	version int
	path    string
}{
	{10, "migrations/initial.sql"},
	{11, "migrations/misses.sql"},
	{12, "migrations/finding_verdicts.sql"},
	{13, "migrations/content_changes.sql"},
}

var ErrReviewRecordStateNotInitialized = errors.New("Review Party is not initialized")
var ErrReviewRecordStateRequiresPreparation = errors.New("Review Party state requires preparation")
var errLedgerUpgradesInPlace = fmt.Errorf("%w", ErrReviewRecordStateRequiresPreparation)

var ErrReviewNotFound = errors.New("no review with id")

type reviewNotFoundError struct{ id string }

func (failure reviewNotFoundError) Error() string {
	return fmt.Sprintf("no review with id %q", failure.id)
}

func (failure reviewNotFoundError) Is(target error) bool { return target == ErrReviewNotFound }

func (failure reviewNotFoundError) Unwrap() error { return sql.ErrNoRows }

func notFoundAs(id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return reviewNotFoundError{id: id}
	}
	return err
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

type RecordStore interface {
	Save(model.ReviewRecord) error
	Load(model.ReviewID) (model.ReviewRecord, error)
}

type EvalRunStore interface {
	CreateEvalSuiteRun(model.EvalSuiteRun, []model.EvalRun) error
	CheckpointEvalRun(model.EvalSuiteRun, model.EvalRun) error
	TerminateEvalSuiteRun(model.EvalSuiteRun) error
	LoadEvalSuiteRun(model.EvalSuiteRunID) (model.EvalSuiteRun, error)
	LoadEvalRun(model.EvalRunID) (model.EvalRun, error)
}

type AdjudicationStore interface {
	PublishAdjudication(model.AdjudicationRevision) (model.AdjudicationRevision, error)
	LoadAdjudication(model.AdjudicationRevisionID) (model.AdjudicationRevision, error)
}

type BundleStore interface {
	CreateReviewBundle(bundle model.ReviewBundle, members []model.ReviewRecord) error
	SaveReviewBundle(bundle model.ReviewBundle) error
	LoadReviewBundle(id model.ReviewBundleID) (model.ReviewBundle, error)
}

type HistoryEntry struct {
	ID              model.ReviewID            `json:"id"`
	Lifecycle       model.Lifecycle           `json:"lifecycle"`
	Repository      string                    `json:"repository"`
	Subject         string                    `json:"subject"`
	Profile         string                    `json:"profile"`
	Reviewer        string                    `json:"reviewer"`
	Model           string                    `json:"model,omitempty"`
	Status          string                    `json:"status,omitempty"`
	DurationMS      int64                     `json:"duration_ms,omitempty"`
	Findings        int                       `json:"findings,omitempty"`
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
	db                     *sql.DB
	directory              string
	projection             reviewRecordProjection
	evalProjection         evalProjection
	bundleProjection       bundleProjection
	adjudicationProjection adjudicationProjection
	missProjection         missProjection
	verdictProjection      verdictProjection
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

// PrepareReviewRecordState creates the managed ledger, upgrades one at schema 10
// or later in place, or replaces an older one. Ordinary Review, inspect, and
// history paths never call this operation.
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

func (s *DeferredLedgerRecordStore) CreateEvalSuiteRun(run model.EvalSuiteRun, evalRuns []model.EvalRun) error {
	ledger, err := s.openExisting()
	if err != nil {
		return err
	}
	return ledger.CreateEvalSuiteRun(run, evalRuns)
}

func (s *DeferredLedgerRecordStore) CheckpointEvalRun(run model.EvalSuiteRun, evalRun model.EvalRun) error {
	ledger, err := s.openExisting()
	if err != nil {
		return err
	}
	return ledger.CheckpointEvalRun(run, evalRun)
}

func (s *DeferredLedgerRecordStore) TerminateEvalSuiteRun(run model.EvalSuiteRun) error {
	ledger, err := s.openExisting()
	if err != nil {
		return err
	}
	return ledger.TerminateEvalSuiteRun(run)
}

func (s *DeferredLedgerRecordStore) LoadEvalRun(id model.EvalRunID) (model.EvalRun, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return model.EvalRun{}, err
	}
	return ledger.LoadEvalRun(id)
}

func (s *DeferredLedgerRecordStore) LoadEvalSuiteRun(id model.EvalSuiteRunID) (model.EvalSuiteRun, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return model.EvalSuiteRun{}, err
	}
	return ledger.LoadEvalSuiteRun(id)
}

func (s *DeferredLedgerRecordStore) CreateReviewBundle(bundle model.ReviewBundle, members []model.ReviewRecord) error {
	ledger, err := s.openExisting()
	if err != nil {
		return err
	}
	return ledger.CreateReviewBundle(bundle, members)
}

func (s *DeferredLedgerRecordStore) SaveReviewBundle(bundle model.ReviewBundle) error {
	ledger, err := s.openExisting()
	if err != nil {
		return err
	}
	return ledger.SaveReviewBundle(bundle)
}

func (s *DeferredLedgerRecordStore) LoadReviewBundle(id model.ReviewBundleID) (model.ReviewBundle, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return model.ReviewBundle{}, err
	}
	return ledger.LoadReviewBundle(id)
}

func (s *DeferredLedgerRecordStore) PublishAdjudication(revision model.AdjudicationRevision) (model.AdjudicationRevision, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return model.AdjudicationRevision{}, err
	}
	return ledger.PublishAdjudication(revision)
}

func (s *DeferredLedgerRecordStore) LoadAdjudication(id model.AdjudicationRevisionID) (model.AdjudicationRevision, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return model.AdjudicationRevision{}, err
	}
	return ledger.LoadAdjudication(id)
}

func (s *DeferredLedgerRecordStore) RecordMisses(records []MissRecord) ([]model.Miss, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.RecordMisses(records)
}

func (s *DeferredLedgerRecordStore) ListMisses(query MissQuery) ([]model.Miss, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.ListMisses(query)
}

func (s *DeferredLedgerRecordStore) RemoveMisses(ids []model.MissID, removal model.MissRemoval) ([]MissRemovalOutcome, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.RemoveMisses(ids, removal)
}

func (s *DeferredLedgerRecordStore) RecordVerdicts(batch VerdictBatch) (VerdictTally, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return VerdictTally{}, err
	}
	return ledger.RecordVerdicts(batch)
}

func (s *DeferredLedgerRecordStore) ListVerdicts(query VerdictQuery) ([]model.FindingVerdict, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return nil, err
	}
	return ledger.ListVerdicts(query)
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
	store := &LedgerRecordStore{
		db: db, directory: directory,
		projection:             reviewRecordProjection{db: db},
		evalProjection:         evalProjection{db: db},
		bundleProjection:       bundleProjection{db: db},
		adjudicationProjection: adjudicationProjection{db: db},
		missProjection:         missProjection{db: db},
		verdictProjection:      verdictProjection{db: db},
	}
	if err := store.initialize(prepare); err != nil {
		return nil, errors.Join(err, db.Close())
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
	if version > currentLedgerSchemaVersion {
		return fmt.Errorf("review ledger schema %d is newer than supported schema %d", version, currentLedgerSchemaVersion)
	}
	if version != currentLedgerSchemaVersion && upgradableLedgerVersion(version) {
		return fmt.Errorf("%w: review ledger schema %d is compatible and upgrades in place to schema %d; run review-party init", errLedgerUpgradesInPlace, version, currentLedgerSchemaVersion)
	}
	if version != currentLedgerSchemaVersion {
		return fmt.Errorf("%w: review ledger schema %d requires state preparation for schema %d", ErrReviewRecordStateRequiresPreparation, version, currentLedgerSchemaVersion)
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

// obsoleteLedgerTables lists every table a replaced pre-release ledger could own,
// ordered so foreign-key children are dropped before their parents.
var obsoleteLedgerTables = []string{
	"finding_verdicts", "misses", "artifacts", "findings", "attempts", "passes",
	"eval_runs", "adjudication_revisions", "review_bundles",
	"eval_suite_runs", "review_content_changes", "reviews", "schema_migrations",
}

func (s *LedgerRecordStore) migrate() (returnErr error) {
	if _, err := s.db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)"); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, rollbackTransaction(tx))
	}()
	version, err := currentMigrationVersion(tx)
	if err != nil {
		return err
	}
	if err := upgradeLedgerSchema(tx, version); err != nil {
		return err
	}
	return tx.Commit()
}

func upgradeLedgerSchema(tx *sql.Tx, version int) error {
	if version > currentLedgerSchemaVersion {
		return fmt.Errorf("review ledger schema %d is newer than supported schema %d", version, currentLedgerSchemaVersion)
	}
	if !upgradableLedgerVersion(version) {
		// A version outside the additive chain (zero for a fresh database) is an
		// obsolete pre-release ledger: replace it rather than preserve it.
		if err := dropObsoleteLedgerTables(tx, version); err != nil {
			return err
		}
		version = 0
	}
	return applyLedgerMigrationsAfter(tx, version)
}

// dropObsoleteLedgerTables clears every table a replaced pre-release ledger
// could own, ordered so foreign-key children are dropped before their parents.
func dropObsoleteLedgerTables(tx *sql.Tx, replaced int) error {
	for _, table := range obsoleteLedgerTables {
		if _, err := tx.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			return fmt.Errorf("replace obsolete review ledger schema %d: %w", replaced, err)
		}
	}
	if _, err := tx.Exec("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)"); err != nil {
		return fmt.Errorf("recreate migration table: %w", err)
	}
	return nil
}

func upgradableLedgerVersion(version int) bool {
	for _, migration := range ledgerMigrations {
		if migration.version == version {
			return true
		}
	}
	return false
}

func applyLedgerMigrationsAfter(tx *sql.Tx, version int) error {
	for _, migration := range ledgerMigrations {
		if migration.version <= version {
			continue
		}
		if err := applyMigration(tx, migration.version, migration.path); err != nil {
			return err
		}
	}
	return nil
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

func (s *LedgerRecordStore) LoadEvalRun(id model.EvalRunID) (model.EvalRun, error) {
	return s.evalProjection.loadEvalRun(id)
}

func (s *LedgerRecordStore) CreateEvalSuiteRun(run model.EvalSuiteRun, evalRuns []model.EvalRun) error {
	return s.evalProjection.createSuiteRun(run, evalRuns)
}

func (s *LedgerRecordStore) CheckpointEvalRun(run model.EvalSuiteRun, evalRun model.EvalRun) error {
	return s.evalProjection.checkpoint(run, evalRun)
}

func (s *LedgerRecordStore) TerminateEvalSuiteRun(run model.EvalSuiteRun) error {
	return s.evalProjection.terminateSuiteRun(run)
}

func (s *LedgerRecordStore) LoadEvalSuiteRun(id model.EvalSuiteRunID) (model.EvalSuiteRun, error) {
	return s.evalProjection.loadSuiteRun(id)
}

func (s *LedgerRecordStore) CreateReviewBundle(bundle model.ReviewBundle, members []model.ReviewRecord) error {
	return s.bundleProjection.create(bundle, members)
}

func (s *LedgerRecordStore) SaveReviewBundle(bundle model.ReviewBundle) error {
	return s.bundleProjection.save(bundle)
}

func (s *LedgerRecordStore) LoadReviewBundle(id model.ReviewBundleID) (model.ReviewBundle, error) {
	return s.bundleProjection.load(id)
}

func (s *LedgerRecordStore) PublishAdjudication(revision model.AdjudicationRevision) (published model.AdjudicationRevision, returnErr error) {
	return s.adjudicationProjection.publish(revision)
}

func (s *LedgerRecordStore) LoadAdjudication(id model.AdjudicationRevisionID) (model.AdjudicationRevision, error) {
	return s.adjudicationProjection.load(id)
}

func (s *LedgerRecordStore) RecordMisses(records []MissRecord) ([]model.Miss, error) {
	return s.missProjection.record(records)
}

func (s *LedgerRecordStore) ListMisses(query MissQuery) ([]model.Miss, error) {
	return s.missProjection.list(query)
}

func (s *LedgerRecordStore) RemoveMisses(ids []model.MissID, removal model.MissRemoval) ([]MissRemovalOutcome, error) {
	return s.missProjection.remove(ids, removal)
}

func (s *LedgerRecordStore) RecordVerdicts(batch VerdictBatch) (VerdictTally, error) {
	return s.verdictProjection.record(batch)
}

func (s *LedgerRecordStore) ListVerdicts(query VerdictQuery) ([]model.FindingVerdict, error) {
	return s.verdictProjection.list(query)
}

func (s *LedgerRecordStore) History(query HistoryQuery) (page HistoryPage, returnErr error) {
	statement, arguments, limit, err := buildHistoryQuery(query)
	if err != nil {
		return HistoryPage{}, err
	}
	rows, err := s.db.Query(statement, arguments...)
	if err != nil {
		return HistoryPage{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, rows.Close())
	}()
	return scanHistoryPage(rows, limit)
}

func rollbackTransaction(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return err
	}
	return nil
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
		COALESCE(json_extract(profile_revision,'$.model'),''),COALESCE(result_status,''),
		COALESCE(json_extract(timings,'$.total_ms'),0),COALESCE(result_finding_count,0),
		COALESCE(json_extract(termination,'$.category'),''),created_at,replays_review_id FROM reviews`
	where, arguments := whereClause(historyFilters(query))
	statement += where + " ORDER BY created_at DESC,id DESC LIMIT ?"
	arguments = append(arguments, limit+1)
	return statement, arguments, limit, nil
}

func historyFilters(query HistoryQuery) []queryFilter {
	var since any
	if query.Since != nil {
		since = query.Since.UTC()
	}
	return []queryFilter{
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
		if err := rows.Scan(&entry.ID, &entry.Lifecycle, &entry.Repository, &entry.Subject, &entry.Profile, &entry.Reviewer, &entry.Model, &entry.Status, &entry.DurationMS, &entry.Findings, &entry.Termination, &entry.CreatedAt, &entry.ReplaysReviewID); err != nil {
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

type InFlight struct {
	Bundles []model.ReviewBundleID
	Reviews []model.ReviewID
}

func (s *LedgerRecordStore) InFlight(repository string) (InFlight, error) {
	bundles, err := queryInFlightIDs[model.ReviewBundleID](s.db, `SELECT id FROM review_bundles WHERE repository = ? AND lifecycle IN ('pending','running') ORDER BY created_at DESC, id DESC`, repository)
	if err != nil {
		return InFlight{}, err
	}
	reviews, err := queryInFlightIDs[model.ReviewID](s.db, `SELECT id FROM reviews WHERE json_extract(subject,'$.repository') = ? AND lifecycle IN ('pending','running') AND id NOT IN (SELECT json_extract(member.value,'$.review_id') FROM review_bundles, json_each(review_bundles.members) AS member WHERE json_extract(member.value,'$.review_id') IS NOT NULL) ORDER BY created_at DESC, id DESC`, repository)
	if err != nil {
		return InFlight{}, err
	}
	return InFlight{Bundles: bundles, Reviews: reviews}, nil
}

// ReviewBundleOwning returns the Review Bundle that lists the Review as a
// member, or "" when the Review ran on its own.
func (s *LedgerRecordStore) ReviewBundleOwning(id model.ReviewID) (model.ReviewBundleID, error) {
	var owner model.ReviewBundleID
	err := s.db.QueryRow(`SELECT review_bundles.id FROM review_bundles, json_each(review_bundles.members) AS member WHERE json_extract(member.value,'$.review_id') = ? LIMIT 1`, id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return owner, err
}

func queryInFlightIDs[ID ~string](db *sql.DB, statement, repository string) (ids []ID, returnErr error) {
	rows, err := db.Query(statement, repository)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	ids = []ID{}
	for rows.Next() {
		var id ID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *DeferredLedgerRecordStore) ReviewBundleOwning(id model.ReviewID) (model.ReviewBundleID, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return "", err
	}
	return ledger.ReviewBundleOwning(id)
}

func (s *DeferredLedgerRecordStore) InFlight(repository string) (InFlight, error) {
	ledger, err := s.openExisting()
	if err != nil {
		return InFlight{}, err
	}
	return ledger.InFlight(repository)
}
