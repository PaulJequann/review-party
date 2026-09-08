package configurationhub

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

// The editor flows must reach the read model only through refresh: search and
// the menu loop rebuilt snapshots by hand and could drop or stale them.
func TestEditorSearchGoesThroughRefreshSeam(t *testing.T) {
	root := t.TempDir()
	repository := configuration.Repository(t.TempDir())
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: root, Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	publishRepositoryProfile(t, manager, repository, "findme")
	editor := &editor{
		manager: manager,
		RunOptions: RunOptions{
			Repository: repository, Accessible: true,
			Input: newLineInput("findme\n"), Output: &bytes.Buffer{},
		},
	}
	// Simulate a snapshot captured before "findme" existed; search must refresh
	// it or the result list stays empty.
	editor.snapshot = Snapshot{Repository: string(repository)}
	if err := editor.search(); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !hubSnapshotHasProfile(editor.snapshot, "findme", "repository") {
		t.Fatalf("search did not refresh the editor snapshot: %#v", editor.snapshot.Items)
	}
	buffer, asserted := editor.Output.(*bytes.Buffer)
	if !asserted {
		t.Fatal("editor output is not a bytes.Buffer")
	}
	output := buffer.String()
	if !strings.Contains(output, "findme") {
		t.Fatalf("search output missing published profile: %q", output)
	}
}

// Every editor-initiated snapshot rebuild must flow through refresh().
func TestEditorSnapshotRebuildsUseRefreshSeam(t *testing.T) {
	editorsSource := readSourceForAudit(t, "editors.go")
	modelSource := readSourceForAudit(t, "model.go")
	// editors.go: the single call inside refresh() itself.
	if got := strings.Count(editorsSource, "buildSnapshot(e.manager"); got != 1 {
		t.Fatalf("editors.go direct buildSnapshot calls = %d, want 1 (inside refresh)", got)
	}
	// model.go: the editor loop must route through refresh(), not rebuild.
	if strings.Contains(modelSource, "buildSnapshot(e.manager") {
		t.Fatal("model.go rebuilds editor snapshots outside the refresh seam")
	}
}

func readSourceForAudit(t *testing.T, filename string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source directory")
	}
	dir := filepath.Dir(thisFile)
	source, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	return string(source)
}
