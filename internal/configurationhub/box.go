package configurationhub

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const minBoxWidth = 6

// renderBox renders a rounded-border box with a pre-styled title embedded in
// the top border and an optional hint embedded in the bottom border.
func renderBox(styledTitle, content string, width int, footer string) string {
	if width < minBoxWidth {
		width = minBoxWidth
	}
	border := lipgloss.RoundedBorder()

	// The title consumes the corner, prefix, and separator cells. Keep one
	// border cell available even when a narrow terminal cannot fit the title.
	titleWidth := lipgloss.Width(styledTitle)
	fillWidth := max(width-5-titleWidth, 1)
	topBorder := borderStyle.Render(border.TopLeft+border.Top+" ") + styledTitle + " " +
		borderStyle.Render(strings.Repeat(border.Top, fillWidth)+border.TopRight)

	contentWidth := max(width-4, 1)
	content = strings.TrimSuffix(content, "\n")
	paddedContent := lipgloss.NewStyle().Width(contentWidth).Render(content)
	lines := strings.Split(paddedContent, "\n")
	for index, line := range lines {
		lines[index] = borderStyle.Render(border.Left) + " " + line + " " + borderStyle.Render(border.Right)
	}

	return topBorder + "\n" + strings.Join(lines, "\n") + "\n" + renderBottomBorder(width, footer, border)
}

func renderBottomBorder(width int, footer string, border lipgloss.Border) string {
	if footer == "" {
		return borderStyle.Render(border.BottomLeft + strings.Repeat(border.Bottom, max(width-2, 1)) + border.BottomRight)
	}

	const leadDashes = 4
	maxFooterWidth := max(width-leadDashes-5, 1)
	footer, _ = cutText(footer, maxFooterWidth)
	footerRendered := metaStyle.Render(footer)
	trailingFill := max(width-lipgloss.Width(footerRendered)-8, 1)
	return borderStyle.Render(border.BottomLeft+strings.Repeat(border.Bottom, leadDashes)+" ") + footerRendered + " " +
		borderStyle.Render(strings.Repeat(border.Bottom, trailingFill)+border.BottomRight)
}

func cutText(value string, width int) (string, string) {
	if width <= 0 || value == "" {
		return value, ""
	}

	var builder strings.Builder
	currentWidth := 0
	for index, character := range value {
		characterWidth := lipgloss.Width(string(character))
		if currentWidth+characterWidth > width && builder.Len() > 0 {
			return builder.String(), value[index:]
		}
		builder.WriteRune(character)
		currentWidth += characterWidth
	}
	return builder.String(), ""
}
