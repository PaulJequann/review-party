package configurationhub

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	responsiveLayoutMinWidth = 100
	responsiveLayoutGap      = 2
	responsiveLeftMinWidth   = 38
	responsiveLeftMaxWidth   = 48
)

type responsiveLayout struct {
	wide       bool
	leftWidth  int
	rightWidth int
	gap        int
}

type responsivePane struct {
	content string
}

type responsivePanes struct {
	left  responsivePane
	right responsivePane
}

func newResponsiveLayout(width int) responsiveLayout {
	width = max(width, 1)
	if width < responsiveLayoutMinWidth {
		return responsiveLayout{leftWidth: width, rightWidth: width}
	}
	leftWidth, rightWidth := responsiveColumnWidths(width)
	return responsiveLayout{
		wide:       true,
		leftWidth:  leftWidth,
		rightWidth: rightWidth,
		gap:        responsiveLayoutGap,
	}
}

func responsiveColumnWidths(width int) (int, int) {
	width = max(width, responsiveLeftMinWidth+responsiveLayoutGap+1)
	leftWidth := width / 3
	leftWidth = max(leftWidth, responsiveLeftMinWidth)
	leftWidth = min(leftWidth, responsiveLeftMaxWidth)
	rightWidth := width - leftWidth - responsiveLayoutGap
	if rightWidth < 1 {
		rightWidth = 1
		leftWidth = max(width-rightWidth-responsiveLayoutGap, 1)
	}
	return leftWidth, rightWidth
}

func (layout responsiveLayout) render(panes responsivePanes) string {
	left := panes.left.padded(layout.leftWidth)
	if panes.right.content == "" {
		return left
	}
	right := panes.right.padded(layout.rightWidth)
	if !layout.wide {
		return lipgloss.JoinVertical(lipgloss.Left, left, "", right)
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		left,
		strings.Repeat(" ", layout.gap),
		right,
	)
}

func (pane responsivePane) padded(width int) string {
	width = max(width, 1)
	content := strings.TrimRight(pane.content, "\n")
	return lipgloss.NewStyle().Width(width).Render(content)
}
