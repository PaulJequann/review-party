package reviewparty

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
)

func captureWorkingChangeFacts(repositoryRoot, base string, untrackedPaths []string) (SubjectFacts, error) {
	tracked, err := gitOutput(repositoryRoot, "diff", "--numstat", "-z", base, "--")
	if err != nil {
		return SubjectFacts{}, fmt.Errorf("measure tracked working changes: %w", err)
	}
	facts, err := parseGitNumStat(tracked)
	if err != nil {
		return SubjectFacts{}, fmt.Errorf("parse tracked working-change facts: %w", err)
	}
	for _, path := range untrackedPaths {
		stat, statErr := gitDiffNoIndex(repositoryRoot, []string{"--numstat", "-z"}, os.DevNull, path)
		if statErr != nil {
			return SubjectFacts{}, fmt.Errorf("measure untracked file %q: %w", path, statErr)
		}
		untracked, parseErr := parseGitNumStat(stat)
		if parseErr != nil {
			return SubjectFacts{}, fmt.Errorf("parse untracked file %q facts: %w", path, parseErr)
		}
		facts.Additions += untracked.Additions
		facts.Deletions += untracked.Deletions
		facts.BinaryFiles += untracked.BinaryFiles
	}
	return facts, nil
}

func parseGitNumStat(output []byte) (SubjectFacts, error) {
	facts := SubjectFacts{}
	for _, entry := range bytes.Split(output, []byte{0}) {
		fields := bytes.SplitN(entry, []byte{'\t'}, 3)
		if len(fields) < 3 {
			continue
		}
		if bytes.Equal(fields[0], []byte("-")) || bytes.Equal(fields[1], []byte("-")) {
			facts.BinaryFiles++
			continue
		}
		additions, err := strconv.Atoi(string(fields[0]))
		if err != nil {
			return SubjectFacts{}, fmt.Errorf("invalid addition count %q", fields[0])
		}
		deletions, err := strconv.Atoi(string(fields[1]))
		if err != nil {
			return SubjectFacts{}, fmt.Errorf("invalid deletion count %q", fields[1])
		}
		facts.Additions += additions
		facts.Deletions += deletions
	}
	return facts, nil
}
