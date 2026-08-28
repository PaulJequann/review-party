//go:build !windows

package discovery

import (
	"os"
	"path/filepath"
	"runtime"
)

func isExecutableFile(_ string, info os.FileInfo) bool { return info.Mode()&0111 != 0 }

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
