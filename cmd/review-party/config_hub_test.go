package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurationHubResolvesGitRepositoryRoot(t *testing.T) {
	root := testGitRepository(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveConfigurationHubRepository(nested)
	if err != nil {
		t.Fatalf("resolve repository: %v", err)
	}
	if resolved != root {
		t.Fatalf("resolved repository = %q, want %q", resolved, root)
	}
}
