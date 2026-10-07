package subject

// A delta is the content between the state a Profile last reviewed and the
// current state, one blob pair per path. Both sides are written as temporary
// trees so one git diff-tree measures or prints the whole delta, however many
// paths it spans.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"maps"
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

var ErrMissingObject = errors.New("content object is missing from the repository")

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
	types, err := root.deltaObjectTypes(delta)
	if err != nil {
		return nil, err
	}
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
		trees[side], err = writeDeltaTree(deltaIndex{repository: string(root), file: filepath.Join(scratch, fmt.Sprint("index-", side))}, delta, pick, types)
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

// MissingObjects is the set of the objects the repository no longer has,
// such as a reviewed working-tree blob that git gc pruned.
func MissingObjects(repository string, objects iter.Seq[string]) (map[string]bool, error) {
	types, err := repositoryRoot(repository).objectTypes(objects)
	if err != nil {
		return nil, fmt.Errorf("check reviewed objects: %w", err)
	}
	missing := map[string]bool{}
	for object := range objects {
		if _, present := types[object]; !present {
			missing[object] = true
		}
	}
	return missing, nil
}

// deltaObjectTypes checks every non-zero side exists and learns its type, so
// a nested repository's commit is listed as a gitlink rather than a blob.
func (root repositoryRoot) deltaObjectTypes(delta []model.ContentChange) (map[string]string, error) {
	paths := deltaObjectPaths(delta)
	types, err := root.objectTypes(maps.Keys(paths))
	if err != nil {
		return nil, fmt.Errorf("check delta objects: %w", err)
	}
	for object, path := range paths {
		if _, present := types[object]; !present {
			return nil, fmt.Errorf("%w: %s of %s", ErrMissingObject, object, path)
		}
	}
	return types, nil
}

// objectTypes maps each object the repository has to its type, in one
// cat-file call however many objects are asked.
func (root repositoryRoot) objectTypes(objects iter.Seq[string]) (map[string]string, error) {
	types := map[string]string{}
	var input strings.Builder
	for object := range objects {
		input.WriteString(object + "\n")
	}
	if input.Len() == 0 {
		return types, nil
	}
	output, err := gitInputOutput(string(root), []byte(input.String()), "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		object, kind, found := strings.Cut(line, " ")
		if !found || kind == "missing" {
			continue
		}
		types[object] = strings.Fields(kind)[0]
	}
	return types, nil
}

// deltaObjectPaths maps every non-zero side of the delta to its path.
func deltaObjectPaths(delta []model.ContentChange) map[string]string {
	paths := map[string]string{}
	for _, change := range delta {
		for _, object := range []string{change.Before, change.After} {
			if object != model.ZeroObjectID {
				paths[object] = change.Path
			}
		}
	}
	return paths
}

// writeDeltaTree stages one side of the delta into a private index and
// writes it as a tree. Paths whose side is the zero ID are absent from it.
func writeDeltaTree(index deltaIndex, delta []model.ContentChange, pick func(model.ContentChange) string, types map[string]string) (string, error) {
	var entries strings.Builder
	for _, change := range delta {
		object := pick(change)
		if object == model.ZeroObjectID {
			continue
		}
		mode := "100644"
		if types[object] == "commit" {
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
