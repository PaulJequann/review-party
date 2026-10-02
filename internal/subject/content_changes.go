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

// RangeContentChanges is a committed range's whole content change set plus
// the set each commit in the range introduces over its first parent.
type RangeContentChanges struct {
	Base    string
	Head    string
	Changes []model.ContentChange
	Commits []CommitContentChanges
}

type CommitContentChanges struct {
	Commit  string
	Changes []model.ContentChange
}

// CommittedRangeContentChanges omits commits that change no content, because
// they leave nothing a Review could cover.
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
	commits, err := root.commitContentChanges(baseObject, headObject)
	if err != nil {
		return RangeContentChanges{}, err
	}
	return RangeContentChanges{Base: string(baseObject), Head: string(headObject), Changes: changes, Commits: commits}, nil
}

// Push range sources name where DefaultPushBase found the base of the range
// a push would publish.
const (
	PushBaseUpstream   = "upstream"
	PushBaseRemoteHead = "remote-head"
)

var ErrNoDefaultPushBase = errors.New("no upstream branch and no origin/HEAD to compare with")

// DefaultPushBase is the merge base of HEAD and the current branch's
// upstream, else of HEAD and origin/HEAD. A merge base keeps a diverged
// upstream's own commits out of the range.
func DefaultPushBase(repository string) (base, source string, err error) {
	if output, err := gitOutput(repository, "merge-base", "HEAD", "@{upstream}"); err == nil {
		return strings.TrimSpace(string(output)), PushBaseUpstream, nil
	}
	if output, err := gitOutput(repository, "merge-base", "HEAD", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(string(output)), PushBaseRemoteHead, nil
	}
	return "", "", ErrNoDefaultPushBase
}

func (root repositoryRoot) commitContentChanges(base, head commitObject) ([]CommitContentChanges, error) {
	listing, err := gitOutput(string(root), "rev-list", "--reverse", "--parents", string(base)+".."+string(head))
	if err != nil {
		return nil, fmt.Errorf("list range commits: %w", err)
	}
	var commits []CommitContentChanges
	for _, line := range strings.Split(strings.TrimSpace(string(listing)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		parent := revisionName(emptyGitTree)
		if len(fields) > 1 {
			parent = revisionName(fields[1])
		}
		changes, err := root.treeContentChanges(parent, revisionName(fields[0]))
		if err != nil {
			return nil, err
		}
		if len(changes) > 0 {
			commits = append(commits, CommitContentChanges{Commit: fields[0], Changes: changes})
		}
	}
	return commits, nil
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

// WorkingContentChanges compares the working tree, including untracked
// files, with HEAD, or with the empty tree before the first commit.
func WorkingContentChanges(repository string) ([]model.ContentChange, error) {
	untracked, err := untrackedPaths(repository)
	if err != nil {
		return nil, err
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
		change := model.ContentChange{Path: fields[index+1], Before: header[2], After: header[3]}
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
// would store for it, so a Working Changes Review matches the same content
// once staged or committed.
func (root repositoryRoot) hashWorktreeSides(entries []rawDiffEntry) error {
	var batch []*rawDiffEntry
	for index := range entries {
		entry := &entries[index]
		if !entry.inWorktree {
			continue
		}
		object, batched, err := root.worktreeObject(entry.change)
		if err != nil {
			return fmt.Errorf("hash working tree path %q: %w", entry.change.Path, err)
		}
		if batched {
			batch = append(batch, entry)
		}
		entry.change.After = object
	}
	return root.hashRegularFiles(batch)
}

// worktreeObject resolves what a regular file in the batch cannot share:
// a missing path, a symlink, whose blob is its target text, and a nested
// repository, whose gitlink is its HEAD commit.
func (root repositoryRoot) worktreeObject(change model.ContentChange) (object string, batched bool, err error) {
	repository, path := string(root), change.Path
	location := filepath.Join(repository, path)
	info, err := os.Lstat(location)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return model.ZeroObjectID, false, nil
	case err != nil:
		return "", false, err
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(location)
		if err != nil {
			return "", false, err
		}
		output, err := gitInputOutput(repository, []byte(target), "hash-object", "--stdin", "--no-filters")
		return strings.TrimSpace(string(output)), false, err
	case info.IsDir():
		if _, err := os.Lstat(filepath.Join(location, ".git")); err != nil {
			return "", false, fmt.Errorf("directory is not a nested repository: %w", err)
		}
		output, err := gitOutput(location, "rev-parse", "--verify", "HEAD")
		return strings.TrimSpace(string(output)), false, err
	case strings.Contains(path, "\n"):
		output, err := gitOutput(repository, "hash-object", "--", path)
		return strings.TrimSpace(string(output)), false, err
	default:
		return "", true, nil
	}
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
	output, err := gitInputOutput(string(root), []byte(input.String()), "hash-object", "--stdin-paths")
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
