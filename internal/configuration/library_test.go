package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPartyInventoryPreservesScopedInvalidDefinitions(t *testing.T) {
	globalRoot := t.TempDir()
	repository := t.TempDir()
	writeDocument(t, filepath.Join(globalRoot, "parties", "release.json"), "GLOBAL")
	writeDocument(t, filepath.Join(repository, ".reviewparty", "parties", "release.json"), "REPOSITORY")
	entries, err := testManager(t, globalRoot).PartyInventory(Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("inventory length = %d", len(entries))
	}
	if entries[0].Scope != ScopeRepository {
		t.Fatalf("first scope = %q", entries[0].Scope)
	}
	if entries[1].Scope != ScopeGlobal {
		t.Fatalf("second scope = %q", entries[1].Scope)
	}
	if entries[0].Err == nil {
		t.Fatalf("invalid Repository definition disappeared from inventory: %#v", entries)
	}
	if entries[1].Err == nil {
		t.Fatalf("invalid Global definition disappeared from inventory: %#v", entries)
	}
}

func TestProfileInventoryPreservesScopedIncompleteDefinitions(t *testing.T) {
	root := t.TempDir()
	repository := t.TempDir()
	writeDocument(t, filepath.Join(root, "profiles", "global-profile", "instructions.md"), "GLOBAL")
	writeDocument(t, filepath.Join(repository, ".reviewparty", "profiles", "repository-profile", "instructions.md"), "REPOSITORY")
	entries, err := testManager(t, root).ProfileInventory(Repository(repository))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("inventory length = %d", len(entries))
	}
	if entries[0].Scope != ScopeRepository {
		t.Fatalf("first scope = %q", entries[0].Scope)
	}
	if entries[1].Scope != ScopeGlobal {
		t.Fatalf("second scope = %q", entries[1].Scope)
	}
	if entries[0].Err == nil {
		t.Fatalf("incomplete Repository definition disappeared from inventory: %#v", entries)
	}
	if entries[1].Err == nil {
		t.Fatalf("incomplete Global definition disappeared from inventory: %#v", entries)
	}
}

func TestPartyLibraryRejectsSymlinkedFiles(t *testing.T) {
	root := t.TempDir()
	repository := t.TempDir()
	outside := filepath.Join(t.TempDir(), "party.json")
	writeDocument(t, outside, "OUTSIDE")
	path := filepath.Join(repository, ".reviewparty", "parties", "linked.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testManager(t, root).LoadParty(ScopeRepository, Repository(repository), "linked"); err == nil {
		t.Fatal("symlinked Party was accepted")
	}
}
