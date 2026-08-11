package subject

import (
	"bytes"
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
		if exitError, ok := err.(*exec.ExitError); ok {
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
	entries := bytes.Split(output, []byte{0})
	byPath := make(map[string]numStatCounts)
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
			return nil, model.SubjectFacts{}, err
		}
		current := byPath[path]
		current.additions += counts.additions
		current.deletions += counts.deletions
		current.binary = current.binary || counts.binary
		byPath[path] = current
	}
	return summarizeNumStat(byPath), summarizeNumStatFacts(byPath), nil
}

type numStatCounts struct {
	additions int
	deletions int
	binary    bool
}

func numStatPath(entries [][]byte, index int, encoded []byte) (string, int) {
	if len(encoded) > 0 || index+2 >= len(entries) {
		return string(encoded), 0
	}
	return string(entries[index+2]), 2
}

func parseNumStatCounts(additionsField, deletionsField []byte) (numStatCounts, error) {
	if bytes.Equal(additionsField, []byte("-")) || bytes.Equal(deletionsField, []byte("-")) {
		return numStatCounts{binary: true}, nil
	}
	additions, err := strconv.Atoi(string(additionsField))
	if err != nil {
		return numStatCounts{}, fmt.Errorf("invalid addition count %q", additionsField)
	}
	deletions, err := strconv.Atoi(string(deletionsField))
	if err != nil {
		return numStatCounts{}, fmt.Errorf("invalid deletion count %q", deletionsField)
	}
	return numStatCounts{additions: additions, deletions: deletions}, nil
}

func summarizeNumStat(byPath map[string]numStatCounts) []string {
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	return paths
}

func summarizeNumStatFacts(byPath map[string]numStatCounts) model.SubjectFacts {
	facts := model.SubjectFacts{}
	for _, counts := range byPath {
		facts.Additions += counts.additions
		facts.Deletions += counts.deletions
		if counts.binary {
			facts.BinaryFiles++
		}
	}
	return facts
}
