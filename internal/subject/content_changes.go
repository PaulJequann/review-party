package subject

// Content changes name reviewed content by path and blob object, with no
// commit IDs, repository path, or patch text, so a Checkpoint can recognize a
// Review of the same content after a rebase, amend, or commit.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"reviewparty/internal/model"
)

// RangeContentChanges is a committed range's whole content change set with
// its resolved base and head commits.
type RangeContentChanges struct {
	Base    string
	Head    string
	Changes []model.ContentChange
}

func CommittedRangeContentChanges(repository string, reference model.SubjectReference) (RangeContentChanges, error) {
	root := repositoryRoot(repository)
	baseObject, headObject, err := committedRangeResolver{repository: root}.resolveRange(revisionName(reference.Base), revisionName(reference.Head))
	if err != nil {
		return RangeContentChanges{}, err
	}
	changes, err := root.treeContentChanges(revisionName(baseObject), revisionName(headObject))
	if err != nil {
		return RangeContentChanges{}, err
	}
	return RangeContentChanges{Base: string(baseObject), Head: string(headObject), Changes: changes}, nil
}

// StagedContentChanges compares the index with HEAD, or with the empty tree
// before the first commit.
func StagedContentChanges(repository string) ([]model.ContentChange, error) {
	entries, err := repositoryRoot(repository).rawDiff("diff-index", "--cached", workingChangesBase(repository))
	if err != nil {
		return nil, err
	}
	return contentChangeSet(entries), nil
}

// WorkingFiles chooses which working-tree files WorkingContentChanges reads.
type WorkingFiles int

const (
	// TrackedFiles reads the files git tracks: what git commit -a commits.
	TrackedFiles WorkingFiles = iota
	// AllFiles adds the untracked files git does not ignore.
	AllFiles
)

// WorkingContentChanges compares the working tree's files with HEAD, or with
// the empty tree before the first commit.
func WorkingContentChanges(repository string, files WorkingFiles) ([]model.ContentChange, error) {
	var untracked []string
	if files == AllFiles {
		var err error
		if untracked, err = untrackedPaths(repository); err != nil {
			return nil, err
		}
	}
	return repositoryRoot(repository).workingContentChanges(revisionName(workingChangesBase(repository)), untracked)
}

func (root repositoryRoot) workingContentChanges(base revisionName, untracked []string) ([]model.ContentChange, error) {
	entries, err := root.rawDiff("diff-index", string(base))
	if err != nil {
		return nil, err
	}
	for _, path := range untracked {
		change := model.ContentChange{Path: strings.TrimSuffix(path, "/"), Before: model.ZeroObjectID, After: model.ZeroObjectID}
		entries = append(entries, rawDiffEntry{change: change, inWorktree: true})
	}
	if err := root.hashWorktreeSides(entries); err != nil {
		return nil, err
	}
	return contentChangeSet(entries), nil
}

func (root repositoryRoot) treeContentChanges(base, head revisionName) ([]model.ContentChange, error) {
	entries, err := root.rawDiff("diff-tree", "-r", string(base), string(head))
	if err != nil {
		return nil, err
	}
	return contentChangeSet(entries), nil
}

// rawDiff runs a Git diff command in the raw format with full object IDs and
// renames split into a deletion and an addition, which is the shape a content
// change set needs.
func (root repositoryRoot) rawDiff(command ...string) ([]rawDiffEntry, error) {
	output, err := gitOutput(string(root), append([]string{command[0], "-z", "--no-renames", "--no-abbrev"}, command[1:]...)...)
	if err != nil {
		return nil, fmt.Errorf("list content changes with git %s: %w", command[0], err)
	}
	return parseRawDiff(output)
}

// rawDiffEntry is one path from Git's raw diff format. inWorktree marks an
// After side that Git leaves as the zero ID because it lives only in the
// working tree and still needs hashing.
type rawDiffEntry struct {
	change     model.ContentChange
	inWorktree bool
}

// gitlinkMode is the mode git gives a nested repository's path.
const gitlinkMode = "160000"

// parseRawDiff reads `:<oldmode> <newmode> <before> <after> <status>\0<path>\0`
// records, which --no-renames limits to one path each.
func parseRawDiff(output []byte) ([]rawDiffEntry, error) {
	fields := strings.Split(string(output), "\x00")
	var entries []rawDiffEntry
	for index := 0; index+1 < len(fields); index += 2 {
		header := strings.Fields(strings.TrimPrefix(fields[index], ":"))
		if len(header) != 5 || !strings.HasPrefix(fields[index], ":") {
			return nil, fmt.Errorf("parse raw diff record %q", fields[index])
		}
		change := model.ContentChange{Path: fields[index+1], Before: header[2], After: header[3], BeforeGitlink: header[0] == gitlinkMode, AfterGitlink: header[1] == gitlinkMode}
		deleted := header[1] == "000000"
		entries = append(entries, rawDiffEntry{change: change, inWorktree: change.After == model.ZeroObjectID && !deleted})
	}
	return entries, nil
}

// contentChangeSet drops entries whose sides agree, which Git reports for
// files that are only stat-dirty, and keeps the last record per path.
func contentChangeSet(entries []rawDiffEntry) []model.ContentChange {
	byPath := make(map[string]model.ContentChange, len(entries))
	for _, entry := range entries {
		byPath[entry.change.Path] = entry.change
	}
	changes := make([]model.ContentChange, 0, len(byPath))
	for _, change := range byPath {
		if change.Before != change.After {
			changes = append(changes, change)
		}
	}
	model.SortContentChanges(changes)
	return changes
}

// hashWorktreeSides fills each working-tree After side with the object ID Git
// stores for it, so a Working Changes Review matches the same content once
// staged or committed. The objects are written so a later delta can be
// measured from a reviewed working state.
func (root repositoryRoot) hashWorktreeSides(entries []rawDiffEntry) error {
	var batch []*rawDiffEntry
	for index := range entries {
		entry := &entries[index]
		if !entry.inWorktree {
			continue
		}
		batched, err := root.worktreeObject(entry)
		if err != nil {
			return fmt.Errorf("hash working tree path %q: %w", entry.change.Path, err)
		}
		if batched {
			batch = append(batch, entry)
		}
	}
	return root.hashRegularFiles(batch)
}

// worktreeObject resolves what a regular file in the batch cannot share:
// a missing path, a symlink, whose blob is its target text, and a nested
// repository, whose gitlink is its HEAD commit.
func (root repositoryRoot) worktreeObject(entry *rawDiffEntry) (batched bool, err error) {
	repository, path := string(root), entry.change.Path
	location := filepath.Join(repository, path)
	info, err := os.Lstat(location)
	var output []byte
	switch {
	case errors.Is(err, fs.ErrNotExist):
		entry.change.After = model.ZeroObjectID
		return false, nil
	case err != nil:
		return false, err
	case info.Mode()&fs.ModeSymlink != 0:
		target, readErr := os.Readlink(location)
		if readErr != nil {
			return false, readErr
		}
		output, err = gitInputOutput(repository, []byte(target), "hash-object", "-w", "--stdin", "--no-filters")
	case info.IsDir():
		if _, err := os.Lstat(filepath.Join(location, ".git")); err != nil {
			return false, fmt.Errorf("directory is not a nested repository: %w", err)
		}
		entry.change.AfterGitlink = true
		output, err = gitOutput(location, "rev-parse", "--verify", "HEAD")
	case strings.Contains(path, "\n"):
		output, err = gitOutput(repository, "hash-object", "-w", "--", path)
	default:
		return true, nil
	}
	entry.change.After = strings.TrimSpace(string(output))
	return false, err
}

// hashRegularFiles feeds newline-separated paths to one hash-object process;
// the Git this targets has no -z form of --stdin-paths.
func (root repositoryRoot) hashRegularFiles(entries []*rawDiffEntry) error {
	if len(entries) == 0 {
		return nil
	}
	var input strings.Builder
	for _, entry := range entries {
		input.WriteString(entry.change.Path + "\n")
	}
	output, err := gitInputOutput(string(root), []byte(input.String()), "hash-object", "-w", "--stdin-paths")
	if err != nil {
		return fmt.Errorf("hash working tree files: %w", err)
	}
	objects := strings.Fields(string(output))
	if len(objects) != len(entries) {
		return fmt.Errorf("hash working tree files: got %d objects for %d paths", len(objects), len(entries))
	}
	for index, entry := range entries {
		entry.change.After = objects[index]
	}
	return nil
}
