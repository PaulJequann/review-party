//go:build !windows

package discovery

import (
	"path/filepath"
	"runtime"
)

func trustedExecutableRoots() []string {
	roots := []string{"/usr/local/bin", "/usr/bin", "/bin"}
	if runtime.GOOS == "darwin" {
		roots = append(roots, "/opt/homebrew/bin")
	}
	return roots
}

func executableCandidates(directory, name string) []string {
	return []string{filepath.Join(directory, name)}
}
