// Package subject resolves and captures review subjects.
package subject

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reviewparty/internal/model"
	"runtime"
	"sync"
)

type workingChangesCapture struct {
	paths   []string
	patch   []byte
	facts   model.SubjectFacts
	changes []model.ContentChange
}

func captureWorkingChanges(repository string) (workingChangesCapture, error) {
	root := repositoryRoot(repository)
	base := workingChangesBase(repository)
	trackedPatch, err := gitOutput(repository, "diff", "--binary", "--no-ext-diff", base, "--")
	if err != nil {
		return workingChangesCapture{}, fmt.Errorf("capture tracked working changes: %w", err)
	}

	untracked, err := untrackedPaths(repository)
	if err != nil {
		return workingChangesCapture{}, err
	}
	patch, err := combineWorkingChangePatches(repository, trackedPatch, untracked)
	if err != nil {
		return workingChangesCapture{}, err
	}
	paths, facts, err := measureCapturedPatch(repository, patch)
	if err != nil {
		return workingChangesCapture{}, err
	}
	facts.ChangedFiles = len(paths)
	changes, err := root.workingContentChanges(revisionName(base), untracked)
	if err != nil {
		return workingChangesCapture{}, err
	}
	return workingChangesCapture{paths: paths, patch: patch, facts: facts, changes: changes}, nil
}

func untrackedPaths(repositoryRoot string) ([]string, error) {
	listing, err := gitOutput(repositoryRoot, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	return splitNUL(listing), nil
}

func workingChangesBase(repositoryRoot string) string {
	if _, err := gitOutput(repositoryRoot, "rev-parse", "--verify", "HEAD"); err != nil {
		return emptyGitTree
	}
	return "HEAD"
}

// untrackedPatchResult carries one file's diff back to the stitching loop.
type untrackedPatchResult struct {
	index int
	patch []byte
	err   error
}

// combineWorkingChangePatches diffs every untracked file against the empty
// tree and appends the patches after the tracked patch. The diffs run in
// parallel because each one spawns a git subprocess, which dominates capture
// time for repositories with many untracked files; results are stitched back
// in listing order so the patch stays deterministic. A file that vanishes or
// becomes unreadable between listing and diffing yields an empty patch: git
// reports an inaccessible file through exit code 1, the same code it uses for
// a real diff with no content.
func combineWorkingChangePatches(repositoryRoot string, trackedPatch []byte, untrackedPaths []string) ([]byte, error) {
	patches := make([][]byte, len(untrackedPaths))
	results := make(chan untrackedPatchResult, len(untrackedPaths))
	next := make(chan int)
	// GOMAXPROCS(0) instead of runtime.NumCPU(): containers may cap CPU via
	// cgroup quota or an explicit GOMAXPROCS below the affinity mask, and
	// oversubscribing that limit makes parallel capture slower than
	// sequential capture.
	lanes := min(runtime.GOMAXPROCS(0), len(untrackedPaths))
	var group sync.WaitGroup
	for range lanes {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range next {
				untrackedPatch, err := gitDiffNoIndex(repositoryRoot, []string{"--binary"}, os.DevNull, untrackedPaths[index])
				results <- untrackedPatchResult{index: index, patch: untrackedPatch, err: err}
			}
		}()
	}
	for index := range untrackedPaths {
		next <- index
	}
	close(next)
	group.Wait()
	close(results)

	var failure error
	for result := range results {
		if result.err != nil {
			if failure == nil {
				failure = fmt.Errorf("capture untracked file %q: %w", untrackedPaths[result.index], result.err)
			}
			continue
		}
		patches[result.index] = result.patch
	}
	if failure != nil {
		return nil, failure
	}

	patch := bytes.NewBuffer(trackedPatch)
	for _, untrackedPatch := range patches {
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
