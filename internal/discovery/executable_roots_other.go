//go:build !windows

package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func isExecutableFile(_ string, info os.FileInfo) bool { return info.Mode()&0111 != 0 }

type trustedRoot string

func trustedExecutableRoots() []string {
	roots := systemExecutableRoots()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return roots
	}
	return append(roots, userExecutableRoots(trustedRoot(home))...)
}

func systemExecutableRoots() []string {
	roots := []string{"/usr/local/bin", "/usr/bin", "/bin"}
	if runtime.GOOS == "darwin" {
		return append(roots, "/opt/homebrew/bin")
	}
	return roots
}

func userExecutableRoots(home trustedRoot) []string {
	roots := configuredUserExecutableRoots(home)
	return append(roots, pathUserExecutableRoots(home)...)
}

func configuredUserExecutableRoots(home trustedRoot) []string {
	roots := make([]string, 0, len(userExecutableLocations(home)))
	for _, root := range userExecutableLocations(home) {
		if string(root) != "" && pathWithinDirectory(string(home), string(root)) {
			roots = append(roots, string(root))
		}
	}
	return roots
}

func pathUserExecutableRoots(home trustedRoot) []string {
	roots := make([]string, 0)
	for _, root := range filepath.SplitList(os.Getenv("PATH")) {
		if isKnownUserExecutableRoot(home, trustedRoot(root)) {
			roots = append(roots, root)
		}
	}
	return roots
}

func userExecutableLocations(home trustedRoot) []trustedRoot {
	root := string(home)
	return []trustedRoot{
		trustedRoot(os.Getenv("XDG_BIN_HOME")),
		trustedRoot(filepath.Join(root, ".local", "bin")),
		trustedRoot(filepath.Join(root, ".cargo", "bin")),
		trustedRoot(filepath.Join(root, ".npm-global", "bin")),
		trustedRoot(filepath.Join(root, ".local", "share", "mise", "shims")),
		trustedRoot(filepath.Join(root, ".asdf", "shims")),
		trustedRoot(filepath.Join(root, ".volta", "bin")),
		trustedRoot(filepath.Join(root, ".bun", "bin")),
		trustedRoot(filepath.Join(root, "go", "bin")),
	}
}

func isKnownUserExecutableRoot(home, root trustedRoot) bool {
	root = trustedRoot(filepath.Clean(string(root)))
	if !hasPathValue(root) {
		return false
	}
	if !pathWithinDirectory(string(home), string(root)) {
		return false
	}
	relative, err := filepath.Rel(string(home), string(root))
	if err != nil {
		return false
	}
	if isKnownUserExecutablePrefix(trustedRoot(relative)) {
		return true
	}
	return isNVMExecutableRoot(trustedRoot(relative))
}

func isKnownUserExecutablePrefix(relative trustedRoot) bool {
	for _, prefix := range []string{
		filepath.Join(".local", "bin"), filepath.Join(".cargo", "bin"), filepath.Join(".npm-global", "bin"),
		filepath.Join(".local", "share", "mise", "shims"), filepath.Join(".asdf", "shims"),
		filepath.Join(".volta", "bin"), filepath.Join(".bun", "bin"), filepath.Join("go", "bin"),
	} {
		if string(relative) == prefix || strings.HasPrefix(string(relative), prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func isNVMExecutableRoot(relative trustedRoot) bool {
	if !strings.HasPrefix(string(relative), filepath.Join(".nvm", "versions")+string(filepath.Separator)) {
		return false
	}
	return filepath.Base(string(relative)) == "bin"
}

func hasPathValue(path trustedRoot) bool {
	return path != "" && path != "."
}

func executableCandidates(directory, name string) []string {
	return []string{filepath.Join(directory, name)}
}
