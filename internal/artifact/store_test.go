package artifact

import (
	"os"
	"path/filepath"
	"reviewparty/internal/model"
	"strings"
	"testing"
)

func TestPublishedArtifactReopensWithRecordedIntegrity(t *testing.T) {
	store := mustNewStore(t, t.TempDir())
	reference, err := store.Publish(model.ReviewID("rp_1723200000000_0123456789abcdef"), 1, "assistant-text", []byte("decoded result"), false)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := store.Read(reference)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "decoded result" || reference.Size != int64(len(contents)) {
		t.Fatalf("contents = %q, reference = %#v", contents, reference)
	}
}

func TestReadRejectsTamperedOrEscapingArtifact(t *testing.T) {
	root := t.TempDir()
	store := mustNewStore(t, root)
	reference, err := store.Publish(model.ReviewID("rp_1723200000000_0123456789abcdef"), 1, "assistant-text", []byte("original"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, reference.Path), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(reference); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("error = %v, want integrity failure", err)
	}
	if _, err := store.Read(model.ArtifactReference{Path: "../outside", Size: 0}); err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("error = %v, want root escape rejection", err)
	}
}

func mustNewStore(t *testing.T, root string) *Store {
	t.Helper()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
