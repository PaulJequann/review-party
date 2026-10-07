//go:build unix

package hostrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsSymlinkAndWideRoots(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	link := filepath.Join(base, "link")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	wide := filepath.Join(base, "wide")
	if err := os.Mkdir(wide, 0o755); err != nil {
		t.Fatal(err)
	}
	for root, want := range map[string]string{link: "symlink", wide: "mode"} {
		_, err := Open(Options{Root: root})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Open(%s): %v; want an error mentioning %q", root, err, want)
		}
	}
	if names := rootNames(t, target); len(names) != 0 {
		t.Fatalf("a rejected root was written to: %v", names)
	}
}
