package subject

import (
	"errors"
	"os"
	"path/filepath"
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
