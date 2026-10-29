package configurationhub

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// sectionRows returns the browser rows of an area listed as one section per
// configuration file: every file header, then that file's rows that match the
// filter.
func (model Model) sectionRows(spec areaSpec) []Item {
	var rows []Item
	for _, item := range model.snapshot.Items {
		if item.Kind == spec.header || item.Kind == spec.kind && matches(item, model.query) {
			rows = append(rows, item)
		}
	}
	return rows
}

func (model Model) browserRows() []Item {
	spec, _ := areaSpecFor(model.browserArea)
	if spec.header != "" {
		return model.sectionRows(spec)
	}
	return model.visibleItems(spec.kind)
}

func (model Model) cursorRow() (Item, bool) {
	rows := model.browserRows()
	if len(rows) == 0 {
		return Item{}, false
	}
	return rows[min(model.cursor, len(rows)-1)], true
}

// renderFileSections renders the stacked per-file sections and reports the
// line the cursor row starts on. A cursor of -1 marks no row.
func renderFileSections(rows []Item, cursor, width int, options renderOptions) (string, int) {
	var content strings.Builder
	cursorLine, line := 1, 1
	var previous Item
	for index, row := range rows {
		if label, ok := reviewGroupLabel(previous, row); ok {
			line += writeSectionLine(&content, renderReviewGroupLabel(label, options), width)
		}
		if index == cursor {
			cursorLine = line
		}
		line += writeSectionLine(&content, renderSectionRow(row, index == cursor, width, options), width)
		previous = row
	}
	if len(rows) == 0 {
		content.WriteString("No configuration file is open.\n")
	}
	return content.String(), cursorLine
}

func writeSectionLine(content *strings.Builder, text string, width int) int {
	content.WriteString(text)
	content.WriteString("\n")
	return wrappedLineCount(text, width)
}

// renderSectionRow indents a file header's state and every row's wrapped
// lines so a narrow pane wraps them under the title rather than back to the
// margin.
func renderSectionRow(row Item, focused bool, width int, options renderOptions) string {
	cursor := "  "
	if focused {
		cursor = "› "
	}
	if !row.header() {
		return cursor + "  " + hangingIndent(renderInventoryItem(row, options), width)
	}
	title := row.Name
	state := sectionIndent + hangingIndent(row.Detail, width)
	if options.styled {
		title = sectionTitleStyle.Render(title)
		state = metaStyle.Render(state)
	}
	return cursor + title + "\n" + state
}

const sectionIndent = "    "

func hangingIndent(text string, width int) string {
	return strings.ReplaceAll(lipgloss.Wrap(text, max(width-len(sectionIndent), 1), " "), "\n", "\n"+sectionIndent)
}

func renderRowDetail(snapshot Snapshot, row Item, options renderOptions) string {
	if !row.header() {
		return renderInventoryItem(row, options)
	}
	title, path := row.Name, headerPath(snapshot, row)
	if options.styled {
		title, path = sectionTitleStyle.Render(title), metaStyle.Render(path)
	}
	if path == "" {
		return title + "\n" + row.Detail
	}
	return title + "\n" + path + "\n" + row.Detail
}

// headerPath is the file a header row describes. An absent file has no path.
func headerPath(snapshot Snapshot, row Item) string {
	switch row.Kind {
	case itemReviewHeader:
		file, _ := snapshot.Reviews.file(row.Target)
		return file.Path
	case itemCheckpointHeader:
		file, _ := snapshot.Checkpoints.file(row.Target)
		return file.Path
	case itemProfile, itemParty, itemReview, itemCheckpoint:
	}
	return ""
}
