package subject

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"reviewparty/internal/model"
	"sort"
	"strconv"
	"strings"
)

func measureCapturedPatch(repositoryRoot string, patch []byte) ([]string, model.SubjectFacts, error) {
	command := exec.Command("git", "apply", "--numstat", "-z")
	command.Dir = repositoryRoot
	command.Stdin = bytes.NewReader(patch)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return nil, model.SubjectFacts{}, fmt.Errorf("measure captured patch: %s", strings.TrimSpace(string(exitError.Stderr)))
		}
		return nil, model.SubjectFacts{}, fmt.Errorf("measure captured patch: %w", err)
	}
	paths, facts, err := parseGitNumStat(output)
	if err != nil {
		return nil, model.SubjectFacts{}, fmt.Errorf("parse captured patch facts: %w", err)
	}
	sort.Strings(paths)
	return paths, facts, nil
}

func parseGitNumStat(output []byte) ([]string, model.SubjectFacts, error) {
	byPath, err := parseLineCounts(output)
	if err != nil {
		return nil, model.SubjectFacts{}, err
	}
	return summarizeNumStat(byPath), summarizeNumStatFacts(byPath), nil
}

// LineCount is one path's added and deleted lines. Binary marks a path whose
// lines git does not count.
type LineCount struct {
	Added   int
	Deleted int
	Binary  bool
}

// LineCounts maps repository paths to their line counts.
type LineCounts map[string]LineCount

// rangeLineCounts counts lines from base to head, without rename pairing so
// each path counts on its own.
func (root repositoryRoot) rangeLineCounts(base, head commitObject) (LineCounts, error) {
	return gitLineCounts(string(root), "diff", "--numstat", "-z", "--no-renames", string(base), string(head), "--")
}

// StagedLineCounts counts lines the index changes over HEAD, or over the empty
// tree before the first commit.
func StagedLineCounts(repository string) (LineCounts, error) {
	return gitLineCounts(repository, "diff", "--numstat", "-z", "--no-renames", "--cached", workingChangesBase(repository), "--")
}

func gitLineCounts(repository string, args ...string) (LineCounts, error) {
	output, err := gitOutput(repository, args...)
	if err != nil {
		return nil, fmt.Errorf("count changed lines: %w", err)
	}
	return parseLineCounts(output)
}

func parseLineCounts(output []byte) (LineCounts, error) {
	entries := bytes.Split(output, []byte{0})
	byPath := LineCounts{}
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		fields := bytes.SplitN(entry, []byte{'\t'}, 3)
		if len(fields) < 3 {
			continue
		}
		path, consumed := numStatPath(entries, index, fields[2])
		index += consumed
		counts, err := parseNumStatCounts(fields[0], fields[1])
		if err != nil {
			return nil, err
		}
		current := byPath[path]
		current.Added += counts.Added
		current.Deleted += counts.Deleted
		current.Binary = current.Binary || counts.Binary
		byPath[path] = current
	}
	return byPath, nil
}

func numStatPath(entries [][]byte, index int, encoded []byte) (string, int) {
	if len(encoded) > 0 || index+2 >= len(entries) {
		return string(encoded), 0
	}
	return string(entries[index+2]), 2
}

func parseNumStatCounts(additionsField, deletionsField []byte) (LineCount, error) {
	if bytes.Equal(additionsField, []byte("-")) || bytes.Equal(deletionsField, []byte("-")) {
		return LineCount{Binary: true}, nil
	}
	additions, err := strconv.Atoi(string(additionsField))
	if err != nil {
		return LineCount{}, fmt.Errorf("invalid addition count %q", additionsField)
	}
	deletions, err := strconv.Atoi(string(deletionsField))
	if err != nil {
		return LineCount{}, fmt.Errorf("invalid deletion count %q", deletionsField)
	}
	return LineCount{Added: additions, Deleted: deletions}, nil
}

func summarizeNumStat(byPath LineCounts) []string {
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	return paths
}

func summarizeNumStatFacts(byPath LineCounts) model.SubjectFacts {
	facts := model.SubjectFacts{}
	for _, counts := range byPath {
		facts.Additions += counts.Added
		facts.Deletions += counts.Deleted
		if counts.Binary {
			facts.BinaryFiles++
		}
	}
	return facts
}
