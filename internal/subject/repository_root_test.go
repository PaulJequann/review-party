package subject

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRepositoryRootReportsADirectoryOutsideEveryRepository(t *testing.T) {
	plain := t.TempDir()
	_, err := ResolveRepositoryRoot(plain)
	if !errors.Is(err, ErrNotRepository) {
		t.Fatalf("ResolveRepositoryRoot(plain directory) = %v, want ErrNotRepository", err)
	}
}

func TestResolveRepositoryRootFindsTheWorkTreeOfANestedDirectory(t *testing.T) {
	repository := testRepository(t)
	nested := filepath.Join(repository, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := ResolveRepositoryRoot(nested)
	if err != nil {
		t.Fatalf("ResolveRepositoryRoot(nested) error = %v", err)
	}
	want, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	if root != want {
		t.Fatalf("ResolveRepositoryRoot(nested) = %q, want %q", root, want)
	}
}

func TestResolveRepositoryRootKeepsOtherFailuresDistinct(t *testing.T) {
	for name, directory := range map[string]string{
		"missing directory": filepath.Join(t.TempDir(), "missing"),
		"git directory":     filepath.Join(testRepository(t), ".git"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveRepositoryRoot(directory)
			if err == nil || errors.Is(err, ErrNotRepository) {
				t.Fatalf("ResolveRepositoryRoot(%s) = %v, want an error other than ErrNotRepository", name, err)
			}
		})
	}
}

func TestResolveRepositoryRootKeepsBrokenRepositoryMetadataDistinct(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	brokenLink := t.TempDir()
	if err := os.WriteFile(filepath.Join(brokenLink, ".git"), []byte("gitdir: "+missing+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("worktree link to a missing git directory", func(t *testing.T) {
		assertBrokenRepository(t, brokenLink, brokenLink)
	})
	t.Run("GIT_DIR naming a missing git directory", func(t *testing.T) {
		t.Setenv("GIT_DIR", missing)
		assertBrokenRepository(t, t.TempDir(), missing)
	})
}

func assertBrokenRepository(t *testing.T, directory, named string) {
	t.Helper()
	_, err := ResolveRepositoryRoot(directory)
	if err == nil || errors.Is(err, ErrNotRepository) || !strings.Contains(err.Error(), named) {
		t.Fatalf("ResolveRepositoryRoot = %v, want an error naming %s other than ErrNotRepository", err, named)
	}
}
