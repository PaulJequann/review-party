package subject

// A delta is the content between the state a Profile last reviewed and the
// current state, one object pair per path. Both sides are written as
// temporary trees so one git diff-tree measures or prints the whole delta,
// however many paths it spans. A gitlink path is written by its pointer, as
// git stores it, so a nested repository's commits are never needed here.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"reviewparty/internal/model"
)

// DeltaLines is the size of a delta: added plus deleted lines per path, and
// the paths git cannot count. A binary path exceeds every allowance.
type DeltaLines struct {
	ByPath map[string]int
	Binary []string
	Total  int
}

func (lines DeltaLines) Exceeds(allowance int) bool {
	return len(lines.Binary) > 0 || lines.Total > allowance
}

// MeasureDelta counts the lines between each path's two blobs.
func MeasureDelta(repository string, delta []model.ContentChange) (DeltaLines, error) {
	lines := DeltaLines{ByPath: map[string]int{}}
	if len(delta) == 0 {
		return lines, nil
	}
	output, err := repositoryRoot(repository).diffDeltaTrees(delta, "--numstat", "-z")
	if err != nil {
		return DeltaLines{}, err
	}
	counts, err := parseLineCounts(output)
	if err != nil {
		return DeltaLines{}, err
	}
	for path, count := range counts {
		if count.Binary {
			lines.Binary = append(lines.Binary, path)
			continue
		}
		lines.ByPath[path] = count.Added + count.Deleted
		lines.Total += count.Added + count.Deleted
	}
	sort.Strings(lines.Binary)
	return lines, nil
}

// DeltaPatch prints the delta as a patch under a/ and b/ prefixes.
func DeltaPatch(repository string, delta []model.ContentChange) ([]byte, error) {
	if len(delta) == 0 {
		return nil, errors.New("delta is empty")
	}
	return repositoryRoot(repository).diffDeltaTrees(delta, "-p", "--binary", "--no-ext-diff")
}

// ResolveUnreviewedDelta is the Subject a run --unreviewed reviews: the delta
// of a scope Subject from the states the Profiles reached. The delta and its
// patch are its identity, and its content changes are the delta edges, so the
// Review extends each Profile's chain to the scope's current content.
func ResolveUnreviewedDelta(scope model.ReviewSubject, delta []model.ContentChange) (Subject, error) {
	patch, err := DeltaPatch(scope.Repository, delta)
	if err != nil {
		return Subject{}, err
	}
	paths, facts, err := measureCapturedPatch(scope.Repository, patch)
	if err != nil {
		return Subject{}, err
	}
	facts.ChangedFiles = len(paths)
	hash := sha256.New()
	for _, value := range []string{string(model.SubjectUnreviewedDelta), scope.Repository, model.ContentChangesDigest(delta)} {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	hash.Write(patch)
	return Subject{ReviewSubject: model.ReviewSubject{
		Kind: model.SubjectUnreviewedDelta, Repository: scope.Repository, Identity: hex.EncodeToString(hash.Sum(nil)),
		BaseObject: scope.BaseObject, HeadObject: scope.HeadObject,
		ChangedPaths: paths, Patch: string(patch), Facts: &facts, ContentChanges: delta,
	}}, nil
}

func (root repositoryRoot) diffDeltaTrees(delta []model.ContentChange, options ...string) (output []byte, returnErr error) {
	scratch, err := os.MkdirTemp("", "review-party-delta-")
	if err != nil {
		return nil, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, os.RemoveAll(scratch))
	}()
	var trees [2]string
	for side, pick := range []func(model.ContentChange) string{
		func(change model.ContentChange) string { return change.Before },
		func(change model.ContentChange) string { return change.After },
	} {
		trees[side], err = writeDeltaTree(deltaIndex{repository: string(root), file: filepath.Join(scratch, fmt.Sprint("index-", side))}, delta, pick)
		if err != nil {
			return nil, err
		}
	}
	arguments := append([]string{"diff-tree", "-r", "--no-renames"}, options...)
	output, err = gitOutput(string(root), append(arguments, trees[0], trees[1])...)
	if err != nil {
		return nil, fmt.Errorf("diff delta: %w", err)
	}
	return output, nil
}

// MissingObjects is the set of the blobs the repository no longer has, such
// as a reviewed working-tree blob that git gc pruned, in one cat-file call
// however many are asked.
func MissingObjects(repository string, objects iter.Seq[string]) (map[string]bool, error) {
	var input strings.Builder
	for object := range objects {
		input.WriteString(object + "\n")
	}
	missing := map[string]bool{}
	if input.Len() == 0 {
		return missing, nil
	}
	output, err := gitInputOutput(repository, []byte(input.String()), "cat-file", "--batch-check")
	if err != nil {
		return nil, fmt.Errorf("check reviewed objects: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if object, kind, found := strings.Cut(line, " "); found && kind == "missing" {
			missing[object] = true
		}
	}
	return missing, nil
}

// writeDeltaTree stages one side of the delta into a private index and
// writes it as a tree. Paths whose side is the zero ID are absent from it.
// write-tree names the path of a blob the repository lacks.
func writeDeltaTree(index deltaIndex, delta []model.ContentChange, pick func(model.ContentChange) string) (string, error) {
	var entries strings.Builder
	for _, change := range delta {
		object := pick(change)
		if object == model.ZeroObjectID {
			continue
		}
		mode := "100644"
		if change.Gitlink {
			mode = "160000"
		}
		entries.WriteString(mode + " " + object + "\t" + change.Path + "\n")
	}
	if _, err := index.git([]byte(entries.String()), "update-index", "--index-info"); err != nil {
		return "", fmt.Errorf("stage delta side: %w", err)
	}
	tree, err := index.git(nil, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write delta side: %w", err)
	}
	return strings.TrimSpace(string(tree)), nil
}

// deltaIndex is a private index file in a repository, so staging a delta
// side never touches the repository's own index.
type deltaIndex struct {
	repository string
	file       string
}

func (index deltaIndex) git(input []byte, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = index.repository
	command.Env = append(os.Environ(), "GIT_INDEX_FILE="+index.file)
	command.Stdin = bytes.NewReader(input)
	output, err := command.Output()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitError.Stderr)))
	}
	return output, err
}
