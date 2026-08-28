//go:build windows

package discovery

import (
	"os"
	"path/filepath"
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
