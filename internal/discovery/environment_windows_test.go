//go:build windows

package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentUsesTrustedWindowsPath(t *testing.T) {
	t.Setenv("Path", `C:\untrusted\bin`)
	entries := environmentFor("codex")
	for _, entry := range entries {
		if strings.HasPrefix(strings.ToLower(entry), "path=") {
			if entry == `Path=C:\untrusted\bin` || entry == `PATH=C:\untrusted\bin` {
				t.Fatalf("environment retained untrusted PATH: %v", entries)
			}
			return
		}
	}
	t.Fatalf("environment omitted trusted Windows PATH: %v", entries)
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

func TestTrustedExecutableIncludesWindowsNPMPath(t *testing.T) {
	appData := t.TempDir()
	npm := filepath.Join(appData, "npm")
	if err := os.MkdirAll(npm, 0o755); err != nil {
		t.Fatal(err)
	}
	wanted := filepath.Join(npm, "reviewer-harness.exe")
	if err := os.WriteFile(wanted, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", appData)
	t.Setenv("PATH", npm)
	resolved, err := trustedExecutable("reviewer-harness")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wanted {
		t.Fatalf("trusted executable = %q, want %q", resolved, wanted)
	}
}
