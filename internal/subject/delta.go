package subject

// A delta is the content between the state a Profile last reviewed and the
// current state, one blob pair per path. Both sides are written as temporary
// trees so one git diff-tree measures or prints the whole delta, however many
// paths it spans.

import (
	"errors"
	"fmt"
	"os"
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
	output, err := diffDeltaTrees(repository, delta, "--numstat", "-z")
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
	return diffDeltaTrees(repository, delta, "-p", "--binary", "--no-ext-diff")
}

func diffDeltaTrees(repository string, delta []model.ContentChange, options ...string) ([]byte, error) {
	types, err := deltaObjectTypes(repository, delta)
	if err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp("", "review-party-delta-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	var trees [2]string
	for side, pick := range []func(model.ContentChange) string{
		func(change model.ContentChange) string { return change.Before },
		func(change model.ContentChange) string { return change.After },
	} {
		trees[side], err = writeDeltaTree(repository, filepath.Join(scratch, fmt.Sprint("index-", side)), delta, pick, types)
		if err != nil {
			return nil, err
		}
	}
	arguments := append([]string{"diff-tree", "-r", "--no-renames"}, options...)
	output, err := gitOutput(repository, append(arguments, trees[0], trees[1])...)
	if err != nil {
		return nil, fmt.Errorf("diff delta: %w", err)
	}
	return output, nil
}

// deltaObjectTypes checks every non-zero side exists and learns its type, so
// a nested repository's commit is listed as a gitlink rather than a blob.
func deltaObjectTypes(repository string, delta []model.ContentChange) (map[string]string, error) {
	var input strings.Builder
	paths := map[string]string{}
	for _, change := range delta {
		for _, object := range []string{change.Before, change.After} {
			if object != model.ZeroObjectID {
				input.WriteString(object + "\n")
				paths[object] = change.Path
			}
		}
	}
	output, err := gitInputOutput(repository, []byte(input.String()), "cat-file", "--batch-check")
	if err != nil {
		return nil, fmt.Errorf("check delta objects: %w", err)
	}
	types := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "missing" {
			return nil, fmt.Errorf("%w: %s of %s", ErrMissingObject, fields[0], paths[fields[0]])
		}
		if len(fields) >= 2 {
			types[fields[0]] = fields[1]
		}
	}
	return types, nil
}

// writeDeltaTree stages one side of the delta into a private index and
// writes it as a tree. Paths whose side is the zero ID are absent from it.
func writeDeltaTree(repository, indexFile string, delta []model.ContentChange, pick func(model.ContentChange) string, types map[string]string) (string, error) {
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
	env := []string{"GIT_INDEX_FILE=" + indexFile}
	if _, err := gitEnvInputOutput(repository, env, []byte(entries.String()), "update-index", "--index-info"); err != nil {
		return "", fmt.Errorf("stage delta side: %w", err)
	}
	tree, err := gitEnvInputOutput(repository, env, nil, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write delta side: %w", err)
	}
	return strings.TrimSpace(string(tree)), nil
}
