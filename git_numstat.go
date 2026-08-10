package reviewparty

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func measureCapturedPatch(repositoryRoot string, patch []byte) ([]string, SubjectFacts, error) {
	command := exec.Command("git", "apply", "--numstat", "-z")
	command.Dir = repositoryRoot
	command.Stdin = bytes.NewReader(patch)
	output, err := command.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return nil, SubjectFacts{}, fmt.Errorf("measure captured patch: %s", strings.TrimSpace(string(exitError.Stderr)))
		}
		return nil, SubjectFacts{}, fmt.Errorf("measure captured patch: %w", err)
	}
	paths, facts, err := parseGitNumStat(output)
	if err != nil {
		return nil, SubjectFacts{}, fmt.Errorf("parse captured patch facts: %w", err)
	}
	sort.Strings(paths)
	return paths, facts, nil
}

func parseGitNumStat(output []byte) ([]string, SubjectFacts, error) {
	entries := bytes.Split(output, []byte{0})
	paths := make([]string, 0, len(entries))
	facts := SubjectFacts{}
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		fields := bytes.SplitN(entry, []byte{'\t'}, 3)
		if len(fields) < 3 {
			continue
		}
		path, consumed := numStatPath(entries, index, fields[2])
		index += consumed
		paths = append(paths, path)
		if err := addNumStatCounts(&facts, fields[0], fields[1]); err != nil {
			return nil, SubjectFacts{}, err
		}
	}
	return paths, facts, nil
}

func numStatPath(entries [][]byte, index int, encoded []byte) (string, int) {
	if len(encoded) > 0 || index+2 >= len(entries) {
		return string(encoded), 0
	}
	return string(entries[index+2]), 2
}

func addNumStatCounts(facts *SubjectFacts, additionsField, deletionsField []byte) error {
	if bytes.Equal(additionsField, []byte("-")) || bytes.Equal(deletionsField, []byte("-")) {
		facts.BinaryFiles++
		return nil
	}
	additions, err := strconv.Atoi(string(additionsField))
	if err != nil {
		return fmt.Errorf("invalid addition count %q", additionsField)
	}
	deletions, err := strconv.Atoi(string(deletionsField))
	if err != nil {
		return fmt.Errorf("invalid deletion count %q", deletionsField)
	}
	facts.Additions += additions
	facts.Deletions += deletions
	return nil
}
