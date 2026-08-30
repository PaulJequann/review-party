package configurationhub

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func renderSizedBox(title, content string, width int, footer string) string {
	width = max(width, minBoxWidth)
	content = strings.TrimRight(content, "\n")
	content = lipgloss.Wrap(content, max(width-4, 1), " ")
	return renderBox(title, content, width, footer)
}
