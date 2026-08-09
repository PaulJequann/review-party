package reviewparty

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const emptyGitTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func resolveSubject(repository string, reference SubjectReference) (ReviewSubject, error) {
	if reference.Kind != SubjectWorkingChanges {
		return ReviewSubject{}, fmt.Errorf("unsupported review subject %q", reference.Kind)
	}
	return resolveWorkingChanges(repository)
}

func resolveWorkingChanges(repository string) (ReviewSubject, error) {
	root, err := gitOutput(repository, "rev-parse", "--show-toplevel")
	if err != nil {
		return ReviewSubject{}, fmt.Errorf("resolve repository root: %w", err)
	}
	repositoryRoot, err := filepath.Abs(strings.TrimSpace(string(root)))
	if err != nil {
		return ReviewSubject{}, fmt.Errorf("make repository root absolute: %w", err)
	}

	base := "HEAD"
	if _, err := gitOutput(repositoryRoot, "rev-parse", "--verify", "HEAD"); err != nil {
		base = emptyGitTree
	}
	trackedPatch, err := gitOutput(repositoryRoot, "diff", "--binary", "--no-ext-diff", base, "--")
	if err != nil {
		return ReviewSubject{}, fmt.Errorf("capture tracked working changes: %w", err)
	}

	untracked, err := gitOutput(repositoryRoot, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return ReviewSubject{}, fmt.Errorf("list untracked files: %w", err)
	}
	paths, err := changedPaths(repositoryRoot, base, untracked)
	if err != nil {
		return ReviewSubject{}, err
	}

	patch := bytes.NewBuffer(trackedPatch)
	for _, path := range splitNUL(untracked) {
		untrackedPatch, diffErr := gitDiffUntracked(repositoryRoot, path)
		if diffErr != nil {
			return ReviewSubject{}, fmt.Errorf("capture untracked file %q: %w", path, diffErr)
		}
		patch.Write(untrackedPatch)
	}
	if patch.Len() == 0 {
		return ReviewSubject{}, errors.New("working changes are empty")
	}

	hash := sha256.New()
	hash.Write([]byte(SubjectWorkingChanges))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.Join(paths, "\x00")))
	hash.Write([]byte{0})
	hash.Write(patch.Bytes())

	return ReviewSubject{
		Kind:         SubjectWorkingChanges,
		Repository:   repositoryRoot,
		Identity:     hex.EncodeToString(hash.Sum(nil)),
		ChangedPaths: paths,
		Patch:        patch.String(),
	}, nil
}

func changedPaths(repositoryRoot, base string, untracked []byte) ([]string, error) {
	tracked, err := gitOutput(repositoryRoot, "diff", "--name-only", "-z", base, "--")
	if err != nil {
		return nil, fmt.Errorf("list tracked working changes: %w", err)
	}
	unique := make(map[string]struct{})
	for _, path := range append(splitNUL(tracked), splitNUL(untracked)...) {
		unique[path] = struct{}{}
	}
	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func gitDiffUntracked(repositoryRoot, path string) ([]byte, error) {
	command := exec.Command("git", "diff", "--no-index", "--binary", "--", os.DevNull, path)
	command.Dir = repositoryRoot
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return output, nil
	}
	return nil, err
}

func gitOutput(repository string, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = repository
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitError.Stderr)))
	}
	return nil, err
}

func splitNUL(value []byte) []string {
	parts := bytes.Split(value, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result
}
