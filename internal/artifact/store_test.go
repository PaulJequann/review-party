package artifact

import (
	"os"
	"path/filepath"
	"reviewparty/internal/model"
	"slices"
	"strings"
	"testing"
)

func TestPublishedArtifactReopensWithRecordedIntegrity(t *testing.T) {
	store := mustNewStore(t, t.TempDir())
	reference, err := store.Publish(model.ReviewID("rp_1723200000000_0123456789abcdef"), 1, Evidence{Kind: "assistant-text", Contents: []byte("decoded result")})
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
	reference, err := store.Publish(model.ReviewID("rp_1723200000000_0123456789abcdef"), 1, Evidence{Kind: "assistant-text", Contents: []byte("original")})
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

func TestIsTemporaryMatchesOnlyUnpublishedWrites(t *testing.T) {
	root := t.TempDir()
	reference, err := mustNewStore(t, root).Publish(model.ReviewID("rp_1723200000000_0123456789abcdef"), 1, Evidence{Kind: "assistant-text", Contents: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	attempt := filepath.Dir(filepath.Join(root, reference.Path))
	partial, err := os.CreateTemp(attempt, temporaryPattern)
	if err != nil {
		t.Fatal(err)
	}
	if err := partial.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(attempt, ".artifact-dir.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, want := temporaryNames(t, attempt), []string{filepath.Base(partial.Name())}; !slices.Equal(got, want) {
		t.Fatalf("IsTemporary matched %v; want only the partial write %v", got, want)
	}
}

func temporaryNames(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if IsTemporary(entry) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func mustNewStore(t *testing.T, root string) *Store {
	t.Helper()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestRemoveLeavesNoEmptyDirectoryBehind(t *testing.T) {
	root := t.TempDir()
	store := mustNewStore(t, root)
	id := model.ReviewID("rp_1723200000000_0123456789abcdef")
	first, err := store.Publish(id, 1, Evidence{Kind: AssistantText, Contents: []byte("first")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Publish(id, 2, Evidence{Kind: AssistantText, Contents: []byte("second")})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Remove(first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(filepath.Join(root, first.Path))); !os.IsNotExist(err) {
		t.Fatalf("emptied attempt directory remains: %v", err)
	}
	if err := store.Remove(second); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, Directory))
	if err != nil {
		t.Fatalf("artifacts directory itself must stay: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("artifacts directory holds %d entries, want the emptied review directory gone", len(entries))
	}
}

func TestPublishAcceptsOnlyFailureEvidenceKinds(t *testing.T) {
	store := mustNewStore(t, t.TempDir())
	for _, kind := range []string{"constructed-prompt", "native-stdout", "native-stderr"} {
		if _, err := store.Publish(model.ReviewID("rp_1723200000000_0123456789abcdef"), 1, Evidence{Kind: kind, Contents: []byte("x")}); err == nil {
			t.Errorf("published retired kind %q", kind)
		}
	}
}

func TestRemoveUnreferencedKeepsOnlyReferencedEvidence(t *testing.T) {
	root := t.TempDir()
	store := mustNewStore(t, root)
	kept, err := store.Publish("rp_1723200000000_0123456789abcdef", 1, Evidence{Kind: AssistantText, Contents: []byte("kept")})
	if err != nil {
		t.Fatal(err)
	}
	dropped, err := store.Publish("rp_1723200000001_0123456789abcdef", 1, Evidence{Kind: ReviewerNoise, Contents: []byte("dropped")})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.RemoveUnreferenced([]string{kept.Path}); err != nil {
		t.Fatal(err)
	}

	if contents, err := store.Read(kept); err != nil || string(contents) != "kept" {
		t.Fatalf("referenced evidence = %q, %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(root, Directory, "rp_1723200000001_0123456789abcdef")); !os.IsNotExist(err) {
		t.Fatalf("unreferenced evidence %s remains: %v", dropped.Path, err)
	}
}

func TestRemoveUnreferencedWithoutAnArtifactsDirectory(t *testing.T) {
	if err := mustNewStore(t, t.TempDir()).RemoveUnreferenced(nil); err != nil {
		t.Fatalf("RemoveUnreferenced = %v, want nothing to do", err)
	}
}
