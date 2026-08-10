package reviewparty

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const emptyGitTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func resolveSubject(repository string, reference SubjectReference) (ReviewSubject, error) {
	if reference.Kind != SubjectWorkingChanges {
		return ReviewSubject{}, fmt.Errorf("unsupported review subject %q", reference.Kind)
	}
	return resolveWorkingChangesAtRoot(repository)
}

func resolveWorkingChanges(repository string) (ReviewSubject, error) {
	repositoryRoot, err := resolveRepositoryRoot(repository)
	if err != nil {
		return ReviewSubject{}, err
	}
	return resolveWorkingChangesAtRoot(repositoryRoot)
}

func resolveWorkingChangesAtRoot(repositoryRoot string) (ReviewSubject, error) {
	capture, err := captureWorkingChanges(repositoryRoot)
	if err != nil {
		return ReviewSubject{}, err
	}
	return newWorkingChangesSubject(repositoryRoot, capture), nil
}

func newWorkingChangesSubject(repositoryRoot string, capture workingChangesCapture) ReviewSubject {
	hash := sha256.New()
	hash.Write([]byte(SubjectWorkingChanges))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.Join(capture.paths, "\x00")))
	hash.Write([]byte{0})
	hash.Write(capture.patch)

	return ReviewSubject{
		Kind:         SubjectWorkingChanges,
		Repository:   repositoryRoot,
		Identity:     hex.EncodeToString(hash.Sum(nil)),
		ChangedPaths: capture.paths,
		Patch:        string(capture.patch),
		Facts:        &capture.facts,
	}
}

func resolveRepositoryRoot(repository string) (string, error) {
	root, err := gitOutput(repository, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	repositoryRoot, err := filepath.Abs(strings.TrimSpace(string(root)))
	if err != nil {
		return "", fmt.Errorf("make repository root absolute: %w", err)
	}
	return repositoryRoot, nil
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
