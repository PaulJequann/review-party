//go:build windows

package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentIncludesWindowsPathRegardlessOfCase(t *testing.T) {
	t.Setenv("Path", `C:\review-party\bin`)
	entries := environmentFor("codex")
	for _, entry := range entries {
		if entry == `Path=C:\review-party\bin` || entry == `PATH=C:\review-party\bin` {
			return
		}
	}
	t.Fatalf("environment omitted Windows Path: %v", entries)
}

func TestTrustedExecutableAcceptsWindowsExtensionWithoutUnixExecuteBits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness.exe")
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := validateExecutablePath(path)
	if err != nil {
		t.Fatalf("validateExecutablePath(%q): %v", path, err)
	}
	if resolved != path {
		t.Fatalf("resolved path = %q, want %q", resolved, path)
	}
}
