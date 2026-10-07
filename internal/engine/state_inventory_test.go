package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"reviewparty/internal/artifact"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func TestStateDirectoryResolvesLikeNew(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	configPath := filepath.Join(home, "config", "review-party", "config.json")

	directory, err := StateDirectory(configPath)
	if err != nil || directory != filepath.Join(home, "state", "review-party") {
		t.Fatalf("StateDirectory without a configured directory = %q, %v", directory, err)
	}
	configured := filepath.Join(home, "chosen")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"schema_version":1,"state_directory":"`+filepath.ToSlash(configured)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err = StateDirectory(configPath)
	if err != nil || filepath.Clean(directory) != configured {
		t.Fatalf("StateDirectory with state_directory %s = %q, %v", configured, directory, err)
	}
	if _, err := os.Stat(configured); !os.IsNotExist(err) {
		t.Fatalf("StateDirectory created %s: %v", configured, err)
	}
}

func TestInventoryStateClassifiesEveryEntryAndChangesNothing(t *testing.T) {
	directory := t.TempDir()
	seedState(t, directory)
	before := stateTree(t, directory)

	entries, err := InventoryState(directory)
	if err != nil {
		t.Fatal(err)
	}
	kinds, sizes := summarizeState(t, entries)
	if !slices.Contains(kinds[StateLedger], "ledger.sqlite") {
		t.Fatalf("ledger entries = %v", kinds[StateLedger])
	}
	delete(kinds, StateLedger)
	want := map[StateKind][]string{
		StateBackups:         {store.BackupDirectory},
		StateArtifacts:       {artifact.Directory},
		StatePartialArtifact: {".artifact-123.tmp"},
		StateUnrecognized:    {"notes.txt"},
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("entries by kind = %v, want %v", kinds, want)
	}
	if sizes[artifact.Directory] != int64(len("published")) || sizes[".artifact-123.tmp"] != int64(len("partial write")) {
		t.Errorf("artifacts sized %d and partial %d; want the published bytes apart from the partial", sizes[artifact.Directory], sizes[".artifact-123.tmp"])
	}
	if after := stateTree(t, directory); !reflect.DeepEqual(before, after) {
		t.Fatalf("InventoryState changed the state directory:\nbefore %v\nafter  %v", before, after)
	}
}

func seedState(t *testing.T, directory string) {
	t.Helper()
	if err := store.PrepareReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}
	artifacts, err := artifact.NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := artifacts.Publish(model.ReviewID("rp_1723200000000_0123456789abcdef"), 1, "assistant-text", []byte("published"), false)
	if err != nil {
		t.Fatal(err)
	}
	writeStateFile(t, filepath.Join(directory, filepath.Dir(reference.Path), ".artifact-123.tmp"), "partial write")
	writeStateFile(t, filepath.Join(directory, store.BackupDirectory, "20260101T000000Z", "ledger.sqlite"), "old ledger")
	writeStateFile(t, filepath.Join(directory, "notes.txt"), "mine")
}

func summarizeState(t *testing.T, entries []StateEntry) (map[StateKind][]string, map[string]int64) {
	t.Helper()
	kinds := map[StateKind][]string{}
	sizes := map[string]int64{}
	for _, entry := range entries {
		if entry.Err != nil {
			t.Fatalf("entry %s: %v", entry.Path, entry.Err)
		}
		kinds[entry.Kind] = append(kinds[entry.Kind], filepath.Base(entry.Path))
		sizes[filepath.Base(entry.Path)] = entry.Bytes
	}
	return kinds, sizes
}

func TestInventoryStateOfAMissingDirectoryIsEmpty(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	entries, err := InventoryState(missing)
	if err != nil || entries != nil {
		t.Fatalf("InventoryState(missing) = %v, %v", entries, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("InventoryState created %s", missing)
	}
}

func writeStateFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stateTree(t *testing.T, root string) []string {
	t.Helper()
	var tree []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		tree = append(tree, strings.TrimPrefix(path, root)+" "+info.Mode().String()+" "+info.ModTime().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
