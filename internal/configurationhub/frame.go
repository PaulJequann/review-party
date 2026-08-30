package configurationhub

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type frameHeightBudget struct {
	header    int
	actionBar int
	footer    int
	viewport  int
}

func newFrameHeightBudget(terminalHeight int, header, actionBar, footer string) frameHeightBudget {
	headerHeight := lipgloss.Height(header)
	actionBarHeight := lipgloss.Height(actionBar)
	footerHeight := lipgloss.Height(footer)
	viewportHeight := terminalHeight - headerHeight - actionBarHeight - footerHeight
	return frameHeightBudget{
		header:    headerHeight,
		actionBar: actionBarHeight,
		footer:    footerHeight,
		viewport:  max(viewportHeight, 1),
	}
}

func (budget frameHeightBudget) total() int {
	return budget.header + budget.actionBar + budget.footer + budget.viewport
}

func refreshHubModel(value tea.Model) tea.Model {
	model, ok := value.(Model)
	if !ok {
		return value
	}
	model.refreshViewport()
	return model
}

func (model Model) frameHeader(options renderOptions) string {
	return model.renderHeader(options) + "\n"
}

func (model Model) frameActionBar(actionBar string) string {
	return "\n" + actionBar + "\n"
}

func (model Model) renderViewportFrame(body string, options renderOptions, actionBars ...string) string {
	actionBar := model.renderActionBar(options)
	if len(actionBars) > 0 {
		actionBar = actionBars[0]
	}
	return strings.Join([]string{
		model.frameHeader(options),
		body,
		model.frameActionBar(actionBar),
		model.renderFooter(options),
	}, "\n")
}

func (model Model) frameBudget(options renderOptions) frameHeightBudget {
	actionBar := model.renderActionBar(options)
	return newFrameHeightBudget(
		max(model.height, 1),
		model.frameHeader(options),
		model.frameActionBar(actionBar),
		model.renderFooter(options),
	)
}

func (model *Model) refreshViewport() {
	model.width = max(model.width, 1)
	model.height = max(model.height, 1)
	model.viewport.SetWidth(model.width)
	budget := model.frameBudget(renderOptions{styled: true})
	model.viewport.SetHeight(budget.viewport)

	oldOffset := model.viewport.YOffset()
	content := model.renderViewportBody(renderOptions{styled: true}, "")
	model.viewport.SetContent(content)
	model.viewport.SetYOffset(oldOffset)
	model.ensureFocusedLine()
}

func scrollIndicator(totalLines, offset, viewportHeight int) string {
	above := max(offset, 0)
	remaining := max(totalLines-offset-viewportHeight, 0)
	switch {
	case above > 0 && remaining > 0:
		return fmt.Sprintf("↑ %d above  ↓ %d more lines (j/k)", above, remaining)
	case remaining > 0:
		return fmt.Sprintf("↓ %d more lines (j/k)", remaining)
	case above > 0:
		return fmt.Sprintf("↑ %d lines (j/k)", above)
	default:
		return ""
	}
}

func hasScrollIndicator(totalLines, offset, viewportHeight int) bool {
	return scrollIndicator(totalLines, offset, viewportHeight) != ""
}

func (model *Model) ensureFocusedLine() {
	line, ok := model.focusedLine()
	if !ok {
		return
	}
	visibleHeight := model.scrollableViewportHeight()
	offset := model.viewport.YOffset()
	if line < offset {
		model.viewport.SetYOffset(line)
		return
	}
	if line >= offset+visibleHeight {
		model.viewport.SetYOffset(line - visibleHeight + 1)
	}
}

func (model Model) focusedLine() (int, bool) {
	switch model.view {
	case viewMenu:
		if model.query != "" || model.searching {
			return 0, false
		}
		return model.menuCursorLine(), true
	case viewBrowser:
		return model.browserCursorLine(), true
	case viewForm, viewPlanPreview:
		return 0, false
	}
	return 0, false
}

func (model Model) scrollableViewportHeight() int {
	height := model.viewport.Height()
	if hasScrollIndicator(model.viewport.TotalLineCount(), model.viewport.YOffset(), height) {
		height = max(height-1, 1)
	}
	return height
}

func (model Model) renderViewportWindow() string {
	content := model.viewport.View()
	indicator := scrollIndicator(model.viewport.TotalLineCount(), model.viewport.YOffset(), model.viewport.Height())
	if indicator == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}
	lines[len(lines)-1] = renderBottomBorder(max(model.width, minBoxWidth), indicator, lipgloss.RoundedBorder())
	return strings.Join(lines, "\n")
}
