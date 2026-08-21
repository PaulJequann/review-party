package engine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type seedWorkspace struct {
	base string
	head string
}

type seedRepository string

func materializeSeededDefect(base, head string, seed evalSeedFile, patch []byte) error {
	workspace := seedWorkspace{base: base, head: head}
	if err := prepareSeedWorktree(workspace, seed); err != nil {
		return err
	}
	if err := applySeedPatch(workspace, seed, patch); err != nil {
		return err
	}
	return stripSeedGitMetadata(workspace)
}

func prepareSeedWorktree(workspace seedWorkspace, seed evalSeedFile) error {
	repository := seedRepository(workspace.base)
	if err := initializeSeedRepository(repository); err != nil {
		return err
	}
	commit, err := repository.output("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if commit != seed.SourceCommit {
		return fmt.Errorf("seed %q source commit mismatch: got %s, want %s", seed.ID, commit, seed.SourceCommit)
	}
	if err := os.Remove(workspace.head); err != nil {
		return err
	}
	_, err = repository.output("worktree", "add", "--detach", workspace.head, commit)
	return err
}

func applySeedPatch(workspace seedWorkspace, seed evalSeedFile, patch []byte) error {
	repository := seedRepository(workspace.head)
	patchPath := filepath.Join(filepath.Dir(workspace.head), "seed.patch")
	if err := os.WriteFile(patchPath, patch, 0o600); err != nil {
		return err
	}
	if _, err := repository.output("apply", "--check", patchPath); err != nil {
		return fmt.Errorf("seed %q does not apply cleanly: %w", seed.ID, err)
	}
	if _, err := repository.output("apply", patchPath); err != nil {
		return err
	}
	if _, err := repository.output("add", "--intent-to-add", "--all"); err != nil {
		return err
	}
	changed, err := seedChangedFiles(repository)
	if err != nil {
		return err
	}
	want := append([]string(nil), seed.ExpectedFiles...)
	sort.Strings(want)
	if !equalStrings(changed, want) {
		return fmt.Errorf("seed %q changed files %v, want exactly %v", seed.ID, changed, want)
	}
	return nil
}

func stripSeedGitMetadata(workspace seedWorkspace) error {
	if err := os.RemoveAll(filepath.Join(workspace.base, ".git")); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(workspace.head, ".git")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func initializeSeedRepository(repository seedRepository) error {
	if _, err := repository.output("init", "--quiet"); err != nil {
		return err
	}
	if _, err := repository.output("add", "--all"); err != nil {
		return err
	}
	tree, err := repository.output("write-tree")
	if err != nil {
		return err
	}
	commit, err := repository.output("commit-tree", tree, "-m", "Review Party seeded source")
	if err != nil {
		return err
	}
	_, err = repository.output("update-ref", "HEAD", commit)
	return err
}

func (repository seedRepository) output(arguments ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", string(repository)}, arguments...)...)
	command.Env = append(reviewerEnvironment(""),
		"GIT_AUTHOR_NAME=Review Party", "GIT_AUTHOR_EMAIL=review-party@localhost",
		"GIT_COMMITTER_NAME=Review Party", "GIT_COMMITTER_EMAIL=review-party@localhost",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
	)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(output)), nil
}

func seedChangedFiles(repository seedRepository) ([]string, error) {
	output, err := repository.output("diff", "--name-only", "--no-renames", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	if output == "" {
		return nil, nil
	}
	files := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	sort.Strings(files)
	return files, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
