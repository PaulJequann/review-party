package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cacheSchemaVersion = 1

// defaultCacheMaxAge bounds how long a cached discovery result is served.
// Cached material is advisory, so entries expire rather than surviving
// forever.
const defaultCacheMaxAge = 24 * time.Hour

type fileCache struct {
	root   cacheRoot
	now    func() time.Time
	maxAge time.Duration
}

type cacheDocument struct {
	SchemaVersion int    `json:"schema_version"`
	Result        Result `json:"result"`
}

type cacheRoot string
type reviewerID string

type cacheEntry struct {
	root     cacheRoot
	reviewer reviewerID
	path     string
}

// NewFileCache returns a disposable cache rooted at root. The cache does not
// create its directory until a successful discovery is saved, and entries
// expire after defaultCacheMaxAge.
func NewFileCache(root string) Cache { return newFileCache(root, nil, 0) }

// newFileCache lets tests pin the clock and age limit; non-positive values
// fall back to the defaults.
func newFileCache(root string, now func() time.Time, maxAge time.Duration) fileCache {
	if now == nil {
		now = time.Now
	}
	if maxAge <= 0 {
		maxAge = defaultCacheMaxAge
	}
	return fileCache{root: cacheRoot(root), now: now, maxAge: maxAge}
}

// DefaultCache returns the user cache location, or a disabled cache when the
// platform does not expose one.
func DefaultCache() Cache {
	root, err := os.UserCacheDir()
	if err != nil || root == "" {
		return nil
	}
	return NewFileCache(filepath.Join(root, "review-party", "model-discovery"))
}

func (cache fileCache) Load(reviewer string) (Result, bool, error) {
	entry, err := cache.entry(reviewerID(reviewer))
	if err != nil {
		return Result{}, false, err
	}
	payload, found, err := readCachePayload(entry)
	if err != nil || !found {
		return Result{}, found, err
	}
	return decodeCachePayload(payload, entry, cache.now(), cache.maxAge)
}

// Forget removes the reviewer's cache entry. A missing entry is not an error;
// the operation is best effort and never blocks discovery on failure.
func (cache fileCache) Forget(reviewer string) error {
	entry, err := cache.entry(reviewerID(reviewer))
	if err != nil {
		return err
	}
	if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("forget discovery cache: %w", err)
	}
	return nil
}

func readCachePayload(entry cacheEntry) ([]byte, bool, error) {
	info, err := os.Lstat(entry.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect discovery cache: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("discovery cache is not a regular file")
	}
	payload, err := os.ReadFile(entry.path)
	if err != nil {
		return nil, false, fmt.Errorf("read discovery cache: %w", err)
	}
	if len(payload) > maxCaptureBytes {
		return nil, false, errors.New("discovery cache exceeds the capture limit")
	}
	return payload, true, nil
}

func decodeCachePayload(payload []byte, entry cacheEntry, now time.Time, maxAge time.Duration) (Result, bool, error) {
	var document cacheDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return Result{}, false, fmt.Errorf("decode discovery cache: %w", err)
	}
	if !validCacheDocument(document, entry) {
		return Result{}, false, errors.New("discovery cache is not a supported result")
	}
	if !cacheEntryIsFresh(document.Result.ObservedAt, now, maxAge) {
		return Result{}, false, nil
	}
	return document.Result, true, nil
}

// cacheEntryIsFresh reports whether an entry observed at observedAt is still
// servable. Entries without a usable timestamp or with a future timestamp are
// never fresh, so a broken clock never promotes stale material.
func cacheEntryIsFresh(observedAt time.Time, now time.Time, maxAge time.Duration) bool {
	if observedAt.IsZero() || observedAt.After(now) {
		return false
	}
	return now.Sub(observedAt) <= maxAge
}

func validCacheDocument(document cacheDocument, entry cacheEntry) bool {
	return document.SchemaVersion == cacheSchemaVersion && document.Result.Reviewer == string(entry.reviewer) && document.Result.Status == StatusSupported
}

func (cache fileCache) Save(reviewer string, result Result) error {
	entry, err := cache.entry(reviewerID(reviewer))
	if err != nil {
		return err
	}
	document := cacheDocument{SchemaVersion: cacheSchemaVersion, Result: cacheableResult(result)}
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode discovery cache: %w", err)
	}
	payload = append(payload, '\n')
	if err := prepareCacheRoot(entry); err != nil {
		return fmt.Errorf("create discovery cache: %w", err)
	}
	if err := writeCacheFile(entry, payload); err != nil {
		return err
	}
	return nil
}

func prepareCacheRoot(entry cacheEntry) error {
	if err := os.MkdirAll(string(entry.root), 0o700); err != nil {
		return err
	}
	return os.Chmod(string(entry.root), 0o700)
}

func writeCacheFile(entry cacheEntry, payload []byte) error {
	temporary, err := os.CreateTemp(string(entry.root), ".discovery-*.tmp")
	if err != nil {
		return fmt.Errorf("create discovery cache temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		if closeErr := temporary.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return fmt.Errorf("set discovery cache permissions: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		if closeErr := temporary.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return fmt.Errorf("write discovery cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close discovery cache: %w", err)
	}
	if err := replaceCacheFile(temporaryPath, entry.path); err != nil {
		return fmt.Errorf("publish discovery cache: %w", err)
	}
	return nil
}

func (cache fileCache) entry(reviewer reviewerID) (cacheEntry, error) {
	if !validReviewerPathComponent(reviewer) {
		return cacheEntry{}, fmt.Errorf("invalid Reviewer ID %q", reviewer)
	}
	if cache.root == "" {
		return cacheEntry{}, errors.New("discovery cache root is unavailable")
	}
	return cacheEntry{
		root: cache.root, reviewer: reviewer,
		path: filepath.Join(string(cache.root), string(reviewer)+".json"),
	}, nil
}

func validReviewerPathComponent(reviewer reviewerID) bool {
	value := string(reviewer)
	return strings.TrimSpace(value) != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`)
}
