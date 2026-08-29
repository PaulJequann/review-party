package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFileCacheRoundTripIsPrivateAndStripsTransientFacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "model-discovery")
	cache := NewFileCache(root)
	want := Result{
		Reviewer:       "grok",
		Status:         StatusSupported,
		Models:         []Model{{ID: "grok-4.6"}},
		HarnessVersion: "1.2.3",
		Authentication: Authentication{Status: AuthAvailable, Diagnostic: "transient"},
		ObservedAt:     time.Unix(10, 0).UTC(),
		Diagnostic:     "transient",
		Cached:         true,
	}
	if err := cache.Save(want.Reviewer, want); err != nil {
		t.Fatal(err)
	}
	got, found, err := cache.Load(want.Reviewer)
	if err != nil || !found {
		t.Fatalf("load = %#v, found %v, error %v", got, found, err)
	}
	want.Authentication = Authentication{}
	want.Diagnostic = ""
	want.Cached = false
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cached result = %#v, want %#v", got, want)
	}
	assertMode(t, root, 0o700)
	assertMode(t, filepath.Join(root, "grok.json"), 0o600)
}

func TestFileCacheSaveReplacesExistingEntry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "model-discovery")
	cache := NewFileCache(root)
	first := Result{Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "grok-4.5"}}}
	second := Result{Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}}
	if err := cache.Save(first.Reviewer, first); err != nil {
		t.Fatal(err)
	}
	if err := cache.Save(second.Reviewer, second); err != nil {
		t.Fatal(err)
	}
	got, found, err := cache.Load(second.Reviewer)
	if err != nil || !found {
		t.Fatalf("replacement load = %#v, found %v, error %v", got, found, err)
	}
	if len(got.Models) != 1 || got.Models[0].ID != "grok-4.6" {
		t.Fatalf("replaced cache = %#v", got.Models)
	}
}

func TestFileCacheRejectsSymlinkedEntries(t *testing.T) {
	root := t.TempDir()
	cache := NewFileCache(root)
	path := filepath.Join(root, "grok.json")
	if err := os.Symlink("missing", path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, found, err := cache.Load("grok"); err == nil || found {
		t.Fatalf("symlink load = found %v, error %v", found, err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
