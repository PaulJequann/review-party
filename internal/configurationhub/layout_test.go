package configurationhub

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestHubGoldenFramesFitTerminal(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 80, height: 24},
		{width: 100, height: 30},
		{width: 160, height: 50},
	} {
		t.Run(fmt.Sprintf("%dx%d menu", size.width, size.height), func(t *testing.T) {
			model := hubModelAtSize(t, size.width, size.height)
			assertFrameFitsTerminal(t, model, size.width, size.height)
		})
		t.Run(fmt.Sprintf("%dx%d browser", size.width, size.height), func(t *testing.T) {
			model := hubModelAtSize(t, size.width, size.height)
			model.area = 1
			updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = requireHubModel(t, updated)
			assertFrameFitsTerminal(t, model, size.width, size.height)
			if !strings.Contains(model.Render(), "Hub › Profiles") {
				t.Fatalf("browser breadcrumb missing:\n%s", model.Render())
			}
		})
	}
}

func TestResponsiveLayoutUsesLockedColumnWidths(t *testing.T) {
	for _, width := range []int{80, 99} {
		t.Run(fmt.Sprintf("stacked-%d", width), func(t *testing.T) {
			layout := newResponsiveLayout(width)
			if layout.wide {
				t.Fatal("narrow layout unexpectedly marked wide")
			}
		})
	}
	for _, width := range []int{100, 120, 160} {
		t.Run(fmt.Sprintf("split-%d", width), func(t *testing.T) {
			assertWideLayout(t, width)
		})
	}
}

func assertWideLayout(t *testing.T, width int) {
	t.Helper()
	layout := newResponsiveLayout(width)
	if !layout.wide {
		t.Fatal("wide layout unexpectedly stacked")
	}
	if layout.leftWidth < responsiveLeftMinWidth || layout.leftWidth > responsiveLeftMaxWidth {
		t.Fatalf("left pane width = %d, want %d..%d", layout.leftWidth, responsiveLeftMinWidth, responsiveLeftMaxWidth)
	}
	if got := layout.leftWidth + layout.gap + layout.rightWidth; got != width {
		t.Fatalf("column widths total = %d, want terminal width %d", got, width)
	}
	if got := lipgloss.Width(layout.render(responsivePanes{
		left: responsivePane{content: "left"}, right: responsivePane{content: "right"},
	})); got != width {
		t.Fatalf("rendered layout width = %d, want terminal width %d", got, width)
	}
}

func TestHubHeaderWrapsLongRepositoryWithoutClipping(t *testing.T) {
	repository := "/tmp/review-party/scratch/repository-with-a-deliberately-long-name"
	model := New(Snapshot{Repository: repository})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = requireHubModel(t, updated)
	assertFrameFitsTerminal(t, model, 80, 24)
	plain := strings.ReplaceAll(stripANSI(model.View().Content), "\n", "")
	if !strings.Contains(plain, repository) {
		t.Fatalf("wrapped header omitted repository path:\n%s", stripANSI(model.View().Content))
	}
}

func TestHubHeightBudgetCoversEverySupportedTerminalHeight(t *testing.T) {
	for height := 20; height <= 60; height++ {
		t.Run(fmt.Sprintf("height-%d", height), func(t *testing.T) {
			model := hubModelAtSize(t, 100, height)
			budget := model.frameBudget(renderOptions{styled: true})
			if model.viewport.Height() != budget.viewport {
				t.Fatalf("viewport height = %d, budget = %d", model.viewport.Height(), budget.viewport)
			}
			if budget.total() != height {
				t.Fatalf("budget total = %d, want terminal height %d", budget.total(), height)
			}
			if got := lipgloss.Height(model.View().Content); got != height {
				t.Fatalf("frame height = %d, want terminal height %d", got, height)
			}
		})
	}
}

func TestHubScrollIndicatorUsesViewportBoxFooter(t *testing.T) {
	model := New(Snapshot{
		Repository: "/repo",
		Items: []Item{
			{Scope: "global", Kind: itemProfile, Name: "one", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "two", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "three", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "four", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "five", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "six", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "seven", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "eight", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "nine", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "ten", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "eleven", Detail: "codex / small"},
			{Scope: "global", Kind: itemProfile, Name: "twelve", Detail: "codex / small"},
		},
	})
	model.width = 80
	model.height = 20
	model.area = 1
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = requireHubModel(t, updated)
	frame := stripANSI(model.View().Content)
	if !strings.Contains(frame, "more lines (j/k)") {
		t.Fatalf("scroll indicator missing:\n%s", frame)
	}
}

func TestHubHelpOverlayListsFocusedBindings(t *testing.T) {
	model := New(Snapshot{Repository: "/repo"})
	model.area = 1
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = requireHubModel(t, updated)
	updated, _ = model.Update(tea.KeyPressMsg{Code: '?'})
	model = requireHubModel(t, updated)
	got := stripANSI(model.View().Content)
	for _, want := range []string{"Help", "toggle help", "navigate", "new profile", "copy profile", "filter", "back", "quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("help overlay missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "n new") {
		t.Fatal("help overlay replaced the short action bar")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: '?'})
	model = requireHubModel(t, updated)
	if model.showHelp {
		t.Fatal("help overlay did not close")
	}
}

func hubModelAtSize(t *testing.T, width, height int) Model {
	t.Helper()
	model := New(Snapshot{
		Repository: "/repo",
		Items: []Item{
			{Scope: "global", Kind: itemProfile, Name: "quality", Detail: "codex / large"},
			{Scope: "repository", Kind: itemProfile, Name: "docs", Detail: "grok / fast"},
			{Scope: "global", Kind: itemParty, Name: "baseline", Detail: "2 Profiles"},
		},
		Overview: []string{"Global: 1 Profile, 1 Party", "Repository: 1 Profile, 0 Parties", "Resolved review count: 2"},
		Warnings: []string{"repository selection is not authored"},
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return requireHubModel(t, updated)
}

func assertFrameFitsTerminal(t *testing.T, model Model, width, height int) {
	t.Helper()
	lines := strings.Split(stripANSI(model.View().Content), "\n")
	if len(lines) != height {
		t.Fatalf("frame has %d lines, want %d:\n%s", len(lines), height, strings.Join(lines, "\n"))
	}
	for lineNumber, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d width = %d, want <= %d: %q", lineNumber+1, got, width, line)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "› ") {
		t.Fatal("cursor is not visible in frame")
	}
}
