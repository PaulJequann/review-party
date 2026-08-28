//go:build windows

package discovery

import (
	"os"
	"path/filepath"
	"strings"
)

func trustedExecutableRoots() []string {
	roots := windowsSystemExecutableRoots()
	return append(roots, windowsUserExecutableRoots()...)
}

func windowsSystemExecutableRoots() []string {
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

func windowsUserExecutableRoots() []string {
	roots := make([]string, 0, 5)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "AppData", "Roaming", "npm"))
	}
	if root := os.Getenv("XDG_BIN_HOME"); root != "" {
		roots = append(roots, root)
	}
	if root := os.Getenv("APPDATA"); root != "" {
		roots = append(roots, filepath.Join(root, "npm"))
	}
	if root := os.Getenv("LOCALAPPDATA"); root != "" {
		roots = append(roots, filepath.Join(root, "Programs"))
	}
	return roots
}

func isExecutableFile(path string, _ os.FileInfo) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exe", ".com", ".bat", ".cmd":
		return true
	default:
		return false
	}
}

func executableCandidates(directory, name string) []string {
	path := filepath.Join(directory, name)
	if strings.Contains(filepath.Base(name), ".") {
		return []string{path}
	}
	return []string{path + ".exe", path + ".com", path + ".bat", path + ".cmd", path}
}
