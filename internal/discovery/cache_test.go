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
	cache := newFileCache(root, func() time.Time { return time.Unix(10, 0).UTC() }, 0)
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

func TestDefaultCacheWritesOnlyUnderCacheDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "local"))
	directory, err := CacheDirectory()
	if err != nil {
		t.Fatal(err)
	}
	result := Result{Reviewer: "grok", Status: StatusSupported, ObservedAt: time.Now().UTC()}
	if err := DefaultCache().Save(result.Reviewer, result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "grok.json")); err != nil {
		t.Fatalf("DefaultCache did not write under CacheDirectory %s: %v", directory, err)
	}
}

func TestFileCacheSaveReplacesExistingEntry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "model-discovery")
	cache := newFileCache(root, func() time.Time { return time.Unix(20, 0).UTC() }, 0)
	first := Result{Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "grok-4.5"}}, ObservedAt: time.Unix(10, 0).UTC()}
	second := Result{Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}, ObservedAt: time.Unix(20, 0).UTC()}
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

func TestFileCacheLoadExpiresStaleEntry(t *testing.T) {
	now := time.Unix(10_000, 0)
	cache := newFileCache(filepath.Join(t.TempDir(), "model-discovery"), func() time.Time { return now }, time.Hour)
	fresh := Result{Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}, ObservedAt: now.Add(-30 * time.Minute)}
	stale := Result{Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "grok-4.5"}}, ObservedAt: now.Add(-2 * time.Hour)}
	if err := cache.Save(fresh.Reviewer, fresh); err != nil {
		t.Fatal(err)
	}
	if got, found, err := cache.Load("grok"); err != nil || !found || got.Models[0].ID != "grok-4.6" {
		t.Fatalf("fresh load = %#v, found %v, error %v", got, found, err)
	}
	if err := cache.Save(stale.Reviewer, stale); err != nil {
		t.Fatal(err)
	}
	got, found, err := cache.Load("grok")
	if err != nil || found {
		t.Fatalf("stale load = %#v, found %v, error %v", got, found, err)
	}
}

func TestFileCacheLoadRejectsZeroAndFutureObservedAt(t *testing.T) {
	now := time.Unix(10_000, 0)
	for name, observedAt := range map[string]time.Time{"zero": {}, "future": now.Add(time.Hour)} {
		t.Run(name, func(t *testing.T) {
			cache := newFileCache(filepath.Join(t.TempDir(), "model-discovery"), func() time.Time { return now }, time.Hour)
			if err := cache.Save("grok", Result{Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}, ObservedAt: observedAt}); err != nil {
				t.Fatal(err)
			}
			if got, found, err := cache.Load("grok"); err != nil || found {
				t.Fatalf("load = %#v, found %v, error %v", got, found, err)
			}
		})
	}
}

func TestFileCacheForgetRemovesEntry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "model-discovery")
	cache := NewFileCache(root)
	result := Result{Reviewer: "grok", Status: StatusSupported, Models: []Model{{ID: "grok-4.6"}}, ObservedAt: time.Unix(10, 0).UTC()}
	if err := cache.Save(result.Reviewer, result); err != nil {
		t.Fatal(err)
	}
	if err := cache.Forget("grok"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := cache.Load("grok"); err != nil || found {
		t.Fatalf("load after forget = found %v, error %v", found, err)
	}
	if err := cache.Forget("grok"); err != nil {
		t.Fatalf("forget of missing entry = %v", err)
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
