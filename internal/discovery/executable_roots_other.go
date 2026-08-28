//go:build !windows

package discovery

import "runtime"

func trustedExecutableRoots() []string {
	roots := []string{"/usr/local/bin", "/usr/bin", "/bin"}
	if runtime.GOOS == "darwin" {
		roots = append(roots, "/opt/homebrew/bin")
	}
	return roots
}
