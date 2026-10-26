package subject

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestGitFixturesIgnoreInheritedRouting(t *testing.T) {
	victim := testRepository(t)
	gitDir := filepath.Join(victim, ".git")
	for name, overrides := range map[string][]string{
		"repository": {"GIT_DIR=" + gitDir},
		"worktree":   {"GIT_WORK_TREE=" + victim},
		"index":      {"GIT_INDEX_FILE=" + filepath.Join(gitDir, "index")},
		"common":     {"GIT_COMMON_DIR=" + gitDir},
		"objects":    {"GIT_OBJECT_DIRECTORY=" + filepath.Join(gitDir, "objects")},
		"alternates": {"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + filepath.Join(gitDir, "objects")},
		"config": {
			"GIT_CONFIG=" + filepath.Join(gitDir, "config"),
			"GIT_CONFIG_GLOBAL=" + filepath.Join(gitDir, "config"),
			"GIT_CONFIG_SYSTEM=" + filepath.Join(gitDir, "config"),
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.worktree", "GIT_CONFIG_VALUE_0=" + victim,
			"GIT_CONFIG_KEY_99=core.bare", "GIT_CONFIG_VALUE_99=true",
			"GIT_CONFIG_PARAMETERS='core.bare=false'",
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := repositorySnapshot(t, victim)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGitFixtureChild$", "-test.v")
			child.Env = append(os.Environ(), overrides...)
			output, err := child.CombinedOutput()
			if after := repositorySnapshot(t, victim); !reflect.DeepEqual(before, after) {
				t.Errorf("inherited %s routing changed the external repository", name)
			}
			if err != nil {
				t.Fatalf("fixture child: %v\n%s", err, output)
			}
		})
	}
}

func TestGitFixtureChild(t *testing.T) {
	repository := testRepository(t)
	if _, err := os.Stat(filepath.Join(repository, ".git", "index")); err != nil {
		t.Fatalf("fixture does not own its index: %v", err)
	}
	for _, key := range []string{"GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_KEY_99", "GIT_CONFIG_VALUE_99"} {
		if _, found := os.LookupEnv(key); found {
			t.Errorf("inherited config injection remains: %s", key)
		}
	}
}

func repositorySnapshot(t *testing.T, repository string) map[string][sha256.Size]byte {
	t.Helper()
	files := make(map[string][sha256.Size]byte)
	err := filepath.WalkDir(repository, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = sha256.Sum256(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
