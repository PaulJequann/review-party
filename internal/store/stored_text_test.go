package store

import (
	"bytes"
	"database/sql"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

const storedTextSentinel = "STORED-TEXT-SENTINEL-9d2e"

var historyTables = []string{"reviews", "passes", "attempts", "findings", "finding_verdicts", "review_content_changes", "checkpoint_waivers", "misses"}

func openTestLedgerDB(t *testing.T, directory string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(directory, ledgerFilename))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, db.Close) })
	return db
}

func execTestStatements(t *testing.T, db *sql.DB, statements [][]any) {
	t.Helper()
	for _, statement := range statements {
		if _, err := db.Exec(statement[0].(string), statement[1:]...); err != nil {
			t.Fatalf("%s: %v", statement[0], err)
		}
	}
}

// seedSchemaFourteenText gives a schema 14 ledger the text it used to store, a
// failed Review beside the completed one, and one row of each kind of history.
func seedSchemaFourteenText(t *testing.T, db *sql.DB, completed model.ReviewID) {
	t.Helper()
	failed := ledgerFixture(model.LifecycleIncomplete)
	failed.ID = "rp_1723200000001_0123456789abcdef"
	if err := (reviewRecordProjection{db: db}).save(failed); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat(storedTextSentinel, 1<<16)
	evidence := "INSERT INTO artifacts(review_id,pass_ordinal,attempt_ordinal,ordinal,kind,path,size,digest,truncated) VALUES(?,0,0,?,?,?,1,'digest',0)"
	execTestStatements(t, db, [][]any{
		{"UPDATE reviews SET subject = CAST(json_set(subject, '$.patch', ?) AS BLOB), result_raw = ?", text, text},
		{"UPDATE attempts SET raw_output = ?", text},
		{evidence, completed, 0, "constructed-prompt", "artifacts/completed-prompt"},
		{evidence, completed, 1, "assistant-text", "artifacts/completed-text"},
		{evidence, failed.ID, 1, "constructed-prompt", "artifacts/failed-prompt"},
		{"INSERT INTO review_content_changes VALUES(?, 'a.go', 'before', 'after')", completed},
		{"INSERT INTO finding_verdicts(review_id,ordinal,finding_digest,verdict,reason,recorded_by,recorded_at) VALUES(?,1,'0123456789abcdef','accepted','fixed','tester',?)", completed, failed.CreatedAt},
		{"INSERT INTO checkpoint_waivers VALUES('w1','pre-push','digest','/repo','reason','tester',?)", failed.CreatedAt},
		{"INSERT INTO misses(id,review_id,path,source,description,recorded_by,recorded_at) VALUES('m1',?,'a.go','human','missed','tester',?)", completed, failed.CreatedAt},
	})
}

func historyRowCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range historyTables {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func queryTestStrings(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, rows.Close)
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

// ledgerFiles reads the database and its write-ahead log as the host stores them.
func ledgerFiles(t *testing.T, directory string) []byte {
	t.Helper()
	var contents []byte
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(filepath.Join(directory, ledgerFilename+suffix))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		contents = append(contents, data...)
	}
	return contents
}

func TestPrepareCompactsStoredTextAndKeepsHistory(t *testing.T) {
	directory := t.TempDir()
	completed := writeLedgerAtSchema(t, directory, 14)
	seedSchemaFourteenText(t, openTestLedgerDB(t, directory), completed.ID)
	history := historyRowCounts(t, openTestLedgerDB(t, directory))
	before := len(ledgerFiles(t, directory))

	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}

	stored := ledgerFiles(t, directory)
	if bytes.Contains(stored, []byte(storedTextSentinel)) || len(stored) > before/8 {
		t.Fatalf("ledger holds %d of %d bytes and still carries stored text = %t", len(stored), before, bytes.Contains(stored, []byte(storedTextSentinel)))
	}
	upgraded := openTestLedgerDB(t, directory)
	if got := historyRowCounts(t, upgraded); !maps.Equal(got, history) {
		t.Fatalf("history rows = %v, want %v", got, history)
	}
	if got := queryTestStrings(t, upgraded, "SELECT path FROM artifacts ORDER BY path"); !reflect.DeepEqual(got, []string{"artifacts/a"}) {
		t.Fatalf("evidence = %v, want only the failed Review's assistant text", got)
	}
	columns := queryTestStrings(t, upgraded, "SELECT name FROM pragma_table_info('reviews') UNION ALL SELECT name FROM pragma_table_info('attempts')")
	if slices.Contains(columns, "result_raw") || slices.Contains(columns, "raw_output") {
		t.Fatalf("columns = %v, want result_raw and raw_output dropped", columns)
	}
}
