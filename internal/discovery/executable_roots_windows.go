//go:build windows

package discovery

import (
	"os"
	"path/filepath"
	"strings"
)

func trustedExecutableRoots() []string {
	roots := make([]string, 0, 3)
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		roots = append(roots, filepath.Join(systemRoot, "System32"))
	}
	for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if root := os.Getenv(name); root != "" {
			roots = append(roots, root)
		}
	}
	return roots
}

func executableCandidates(directory, name string) []string {
	path := filepath.Join(directory, name)
	if strings.Contains(filepath.Base(name), ".") {
		return []string{path}
	}
	return []string{path + ".exe", path + ".com", path + ".bat", path + ".cmd", path}
}
