package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRecordStoreLoadsUnversionedReviewRecord(t *testing.T) {
	directory := t.TempDir()
	id := ReviewID("rp_1723200000000_0123456789abcdef")
	payload := `{
  "id": "rp_1723200000000_0123456789abcdef",
  "lifecycle": "incomplete",
  "subject": {"kind":"working-changes","repository":"/repo","identity":"subject","changed_paths":[],"patch":"diff"},
  "profile_revision": {},
  "profile_snapshot": {},
  "passes": [],
  "incomplete_cause": "legacy diagnostic",
  "created_at": "2026-08-09T12:00:00Z",
  "updated_at": "2026-08-09T12:00:01Z"
}`
	writeRecordFixture(t, directory, id, payload)
	store := mustNewFileRecordStore(t, directory)

	record, err := store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if record.SchemaVersion != legacyReviewRecordSchemaVersion {
		t.Fatalf("schema version = %d", record.SchemaVersion)
	}
	if record.IncompleteCause != "legacy diagnostic" {
		t.Fatalf("incomplete cause = %q", record.IncompleteCause)
	}
	if record.Termination != nil {
		t.Fatalf("termination = %#v", record.Termination)
	}
	if record.Subject.Facts != nil {
		t.Fatalf("subject facts = %#v", record.Subject.Facts)
	}
	if record.Runtime != nil {
		t.Fatalf("runtime = %#v", record.Runtime)
	}
	if record.Timings != nil {
		t.Fatalf("timings = %#v", record.Timings)
	}
}

func TestFileRecordStoreRejectsNewerSchema(t *testing.T) {
	directory := t.TempDir()
	id := ReviewID("rp_1723200000000_0123456789abcdef")
	payload := `{"schema_version":99,"id":"rp_1723200000000_0123456789abcdef"}`
	writeRecordFixture(t, directory, id, payload)
	store := mustNewFileRecordStore(t, directory)

	_, err := store.Load(id)
	if err == nil {
		t.Fatal("expected newer schema to be rejected")
	}
	if !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("error = %v", err)
	}
}

func writeRecordFixture(t *testing.T, directory string, id ReviewID, payload string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, string(id)+".json"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustNewFileRecordStore(t *testing.T, directory string) *fileRecordStore {
	t.Helper()
	store, err := newFileRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
