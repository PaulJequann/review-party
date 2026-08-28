//go:build windows

package discovery

import "testing"

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
