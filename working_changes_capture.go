package reviewparty

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

type workingChangesCapture struct {
	paths []string
	patch []byte
	facts SubjectFacts
}

func captureWorkingChanges(repositoryRoot string) (workingChangesCapture, error) {
	base := workingChangesBase(repositoryRoot)
	trackedPatch, err := gitOutput(repositoryRoot, "diff", "--binary", "--no-ext-diff", base, "--")
	if err != nil {
		return workingChangesCapture{}, fmt.Errorf("capture tracked working changes: %w", err)
	}

	untracked, err := gitOutput(repositoryRoot, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return workingChangesCapture{}, fmt.Errorf("list untracked files: %w", err)
	}
	paths, err := changedPaths(repositoryRoot, base, untracked)
	if err != nil {
		return workingChangesCapture{}, err
	}

	untrackedPaths := splitNUL(untracked)
	patch, err := combineWorkingChangePatches(repositoryRoot, trackedPatch, untrackedPaths)
	if err != nil {
		return workingChangesCapture{}, err
	}
	facts, err := captureWorkingChangeFacts(repositoryRoot, base, untrackedPaths)
	if err != nil {
		return workingChangesCapture{}, err
	}
	facts.ChangedFiles = len(paths)
	return workingChangesCapture{paths: paths, patch: patch, facts: facts}, nil
}

func workingChangesBase(repositoryRoot string) string {
	if _, err := gitOutput(repositoryRoot, "rev-parse", "--verify", "HEAD"); err != nil {
		return emptyGitTree
	}
	return "HEAD"
}

func combineWorkingChangePatches(repositoryRoot string, trackedPatch []byte, untrackedPaths []string) ([]byte, error) {
	patch := bytes.NewBuffer(trackedPatch)
	for _, path := range untrackedPaths {
		untrackedPatch, err := gitDiffNoIndex(repositoryRoot, []string{"--binary"}, os.DevNull, path)
		if err != nil {
			return nil, fmt.Errorf("capture untracked file %q: %w", path, err)
		}
		patch.Write(untrackedPatch)
	}
	if patch.Len() == 0 {
		return nil, errors.New("working changes are empty")
	}
	return patch.Bytes(), nil
}

func gitDiffNoIndex(repositoryRoot string, options []string, left, right string) ([]byte, error) {
	arguments := append([]string{"diff", "--no-index"}, options...)
	arguments = append(arguments, "--", left, right)
	command := exec.Command("git", arguments...)
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
