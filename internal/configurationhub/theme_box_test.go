package configurationhub

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestRenderBoxGoldenWidths(t *testing.T) {
	for _, width := range []int{20, 40, 80} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			got := strings.Split(stripANSI(renderBox("Title", "alpha\nbeta\n", width, "hint")), "\n")
			contentWidth := width - 4
			want := []string{
				"╭─ Title " + strings.Repeat("─", width-10) + "╮",
				"│ alpha" + strings.Repeat(" ", contentWidth-lipgloss.Width("alpha")) + " │",
				"│ beta" + strings.Repeat(" ", contentWidth-lipgloss.Width("beta")) + " │",
				"╰──── hint " + strings.Repeat("─", width-12) + "╯",
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("renderBox(%d) = %q, want %q", width, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			for lineNumber, line := range got {
				if lipgloss.Width(line) != width {
					t.Errorf("line %d width = %d, want %d: %q", lineNumber, lipgloss.Width(line), width, line)
				}
			}
		})
	}
}

func TestRenderBoxDoesNotClipContentAndGuardsMinimumWidth(t *testing.T) {
	longContent := strings.Repeat("x", 24)
	if got := stripANSI(renderBox("Title", longContent, 20, "hint")); strings.Count(strings.ReplaceAll(got, "\n", ""), "x") != len(longContent) {
		t.Fatalf("renderBox clipped content: %q", got)
	}

	for _, width := range []int{-10, 0, 1, 5} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			lines := strings.Split(stripANSI(renderBox("T", "content", width, "")), "\n")
			if len(lines) < 3 {
				t.Fatalf("renderBox(%d) returned %d lines, want at least 3: %q", width, len(lines), strings.Join(lines, "\n"))
			}
			if lipgloss.Width(lines[0]) < 6 || lipgloss.Width(lines[len(lines)-1]) < 6 {
				t.Fatalf("renderBox(%d) did not hold minimum border width: %q", width, strings.Join(lines, "\n"))
			}
		})
	}
}

func TestHubRenderIsPlainText(t *testing.T) {
	model := New(Snapshot{
		Repository: "/repo",
		Items: []Item{
			{Scope: "global", Kind: itemProfile, Name: "quality", Detail: "codex / large"},
			{Scope: "repository", Kind: itemProfile, Name: "quality", Detail: "invalid: missing model"},
		},
		Warnings: []string{"repository selection is not authored"},
	})
	model.area = 1
	got := model.Render()
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("Render returned ANSI escapes: %q", got)
	}
	for _, want := range []string{
		"Review Party Configuration Hub",
		"[repository] /repo",
		"[global] quality",
		"[repository] quality",
		"⏎ open",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render missing %q:\n%s", want, got)
		}
	}
	model.area = 0
	if got := model.Render(); !strings.Contains(got, "▲ repository selection is not authored") {
		t.Fatalf("Render missing warning:\n%s", got)
	}

	model.area = 1
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)
	if got := model.Render(); strings.ContainsRune(got, '\x1b') {
		t.Fatalf("form Render returned ANSI escapes: %q", got)
	}
}

func TestHubDraftIndicatorAndActionBar(t *testing.T) {
	model := New(Snapshot{Repository: "/repo"})
	model.drafts.copyName = "quality"
	got := stripANSI(model.View().Content)
	if !strings.Contains(got, "Review Changes") || !strings.Contains(got, "⏸") {
		t.Fatalf("draft indicator missing:\n%s", got)
	}
	if !strings.Contains(got, "↑/↓ navigate  ⏎ open  / search  esc clear  q quit") {
		t.Fatalf("action bar missing:\n%s", got)
	}
}

func TestHubHeaderDoesNotDuplicateWhenViewportChanges(t *testing.T) {
	model := New(Snapshot{
		Repository: "/repo",
		Items: []Item{
			{Scope: "global", Kind: itemProfile, Name: "one", Detail: "codex / small"},
			{Scope: "repository", Kind: itemProfile, Name: "two", Detail: "grok / fast"},
		},
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	model = requireHubModel(t, updated)
	assertSingleHubHeader(t, model.View().Content)
	if strings.Contains(model.viewport.View(), "Review Party Configuration Hub") {
		t.Fatal("viewport content contains the fixed Hub header")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = requireHubModel(t, updated)
	assertSingleHubHeader(t, model.View().Content)
}

func assertSingleHubHeader(t *testing.T, content string) {
	t.Helper()
	if count := strings.Count(stripANSI(content), "Review Party Configuration Hub"); count != 1 {
		t.Fatalf("Hub header occurs %d times, want 1:\n%s", count, stripANSI(content))
	}
}

func TestRawColorConstructionStaysInTheme(t *testing.T) {
	sourceDir := configurationHubSourceDir(t)
	for _, entry := range configurationHubSourceFiles(t, sourceDir) {
		if entry.Name() == "theme.go" {
			continue
		}
		assertNoRawLipglossColor(t, filepath.Join(sourceDir, entry.Name()), entry.Name())
	}
}

func configurationHubSourceDir(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(sourceFile)
}

func configurationHubSourceFiles(t *testing.T, sourceDir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !isConfigurationHubSource(entry) {
			continue
		}
		files = append(files, entry)
	}
	return files
}

func isConfigurationHubSource(entry os.DirEntry) bool {
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
		return false
	}
	return !strings.HasSuffix(entry.Name(), "_test.go")
}

func assertNoRawLipglossColor(t *testing.T, path, name string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"lipgloss." + "Color("} {
		if strings.Contains(string(contents), marker) {
			t.Errorf("%s constructs a raw/adaptive lipgloss color; use theme.go: %s", name, marker)
		}
	}
}
