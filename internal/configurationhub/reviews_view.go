package configurationhub

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"reviewparty/internal/configuration"
)

// ReviewFile is one configuration file's Review selection as the Hub shows and
// edits it. Selection is nil when the file declares none.
type ReviewFile struct {
	Target    configuration.Scope
	Path      string
	Present   bool
	Selection *configuration.ReviewSelection
}

// ReviewsView lists every configuration file the Hub can edit, in display
// order, and names the file whose Review selection is in force. InForce is
// empty when no file declares one.
type ReviewsView struct {
	InForce configuration.Scope
	Files   []ReviewFile
}

func configurationTitle(scope configuration.Scope) string {
	switch scope {
	case configuration.ScopeGlobal:
		return "Global Configuration"
	case configuration.ScopeRepository:
		return "Repository Configuration"
	}
	return string(scope) + " Configuration"
}

func newReviewsView(loaded configuration.Loaded, targets []configuration.Scope) ReviewsView {
	view := ReviewsView{InForce: inForceTarget(loaded.ReviewSelection())}
	for _, target := range targets {
		document := loaded.Global
		if target == configuration.ScopeRepository {
			document = loaded.Repository
		}
		view.Files = append(view.Files, ReviewFile{Target: target, Path: document.Path, Present: document.Present, Selection: document.Document.Reviews})
	}
	return view
}

func inForceTarget(value configuration.Value[configuration.ReviewSelection]) configuration.Scope {
	switch value.Source {
	case configuration.SourceGlobal:
		return configuration.ScopeGlobal
	case configuration.SourceRepository:
		return configuration.ScopeRepository
	case configuration.SourcePackaged, configuration.SourceExplicit:
		return ""
	}
	return ""
}

func (view ReviewsView) file(target configuration.Scope) (ReviewFile, bool) {
	for _, file := range view.Files {
		if file.Target == target {
			return file, true
		}
	}
	return ReviewFile{}, false
}

func (view ReviewsView) inForceCount() int {
	file, ok := view.file(view.InForce)
	if !ok || file.Selection == nil {
		return 0
	}
	return len(file.Selection.Global) + len(file.Selection.Repository)
}

// headerState is the one description of a file's Review state. The TUI
// section header, the Overview, and the accessible file prompt all show it.
func (view ReviewsView) headerState(file ReviewFile) string {
	repository := file.Target == configuration.ScopeRepository
	switch {
	case !file.Present && repository:
		return "no file; inherits Global Configuration"
	case !file.Present:
		return "no file"
	case file.Selection == nil && repository:
		return "inherits Global Configuration"
	case file.Selection == nil:
		return "no Review selection"
	case view.InForce == file.Target:
		return "● in force · " + file.summary()
	}
	return "replaced here by Repository Configuration · " + file.summary()
}

func (file ReviewFile) summary() string {
	return fmt.Sprintf("%d selected · concurrency %d", len(file.Selection.Global)+len(file.Selection.Repository), file.Selection.ConcurrencyLimit)
}

func (file ReviewFile) declares() bool {
	return file.Selection != nil
}

func (file ReviewFile) hasItems() bool {
	return file.declares() && len(file.Selection.Global)+len(file.Selection.Repository) > 0
}

// operations lists what an edit of this file can do, in prompt order.
func (file ReviewFile) operations() []reviewOperation {
	operations := []reviewOperation{reviewAdd}
	if file.hasItems() {
		operations = append(operations, reviewRemove, reviewMove)
	}
	operations = append(operations, reviewConcurrency)
	if file.Target == configuration.ScopeRepository && file.declares() {
		operations = append(operations, reviewInherit)
	}
	return operations
}

// rowOperations narrows a file's operations to the cursor row: items are
// removed or moved from their own row, and a file inherits from its header.
func (snapshot Snapshot) rowOperations(row Item) []reviewOperation {
	file, ok := snapshot.Reviews.file(row.Target)
	if !ok {
		return nil
	}
	var operations []reviewOperation
	for _, operation := range file.operations() {
		switch operation {
		case reviewRemove, reviewMove:
			if row.Kind == itemReview {
				operations = append(operations, operation)
			}
		case reviewInherit:
			if row.Kind == itemReviewHeader {
				operations = append(operations, operation)
			}
		case reviewAdd, reviewConcurrency:
			operations = append(operations, operation)
		}
	}
	return operations
}

// ChoicesFor filters the add choices to definitions the target file may
// reference. Global Configuration cannot reference Repository definitions.
func (snapshot Snapshot) ChoicesFor(target configuration.Scope) []configuration.SelectedDefinition {
	var choices []configuration.SelectedDefinition
	for _, choice := range snapshot.ReviewChoices {
		if target == configuration.ScopeGlobal && choice.Scope != configuration.ScopeGlobal {
			continue
		}
		choices = append(choices, choice)
	}
	return choices
}

func (item Item) entry() reviewEntry {
	return reviewEntry{group: configuration.Scope(item.Scope), index: item.Index, expect: item.Selection}
}

type reviewKey struct {
	key       string
	operation reviewOperation
	label     string
}

// reviewKeys binds browser keys to Review operations. The key dispatcher,
// the action bar, and the help overlay read this one table.
var reviewKeys = []reviewKey{
	{key: "a", operation: reviewAdd, label: "add"},
	{key: "r", operation: reviewRemove, label: "remove"},
	{key: "m", operation: reviewMove, label: "move"},
	{key: "c", operation: reviewConcurrency, label: "concurrency"},
	{key: "i", operation: reviewInherit, label: "inherit Global"},
}

func reviewKeyOperation(key string) (reviewOperation, bool) {
	for _, binding := range reviewKeys {
		if binding.key == key {
			return binding.operation, true
		}
	}
	return 0, false
}

// cursorReviewKeys lists the key bindings the cursor row allows.
func (model Model) cursorReviewKeys() []reviewKey {
	row, ok := model.cursorRow()
	if !ok {
		return nil
	}
	allowed := model.snapshot.rowOperations(row)
	var keys []reviewKey
	for _, binding := range reviewKeys {
		if slices.Contains(allowed, binding.operation) {
			keys = append(keys, binding)
		}
	}
	return keys
}

// reviewRows returns the browser rows for the Reviews area: every file header,
// then that file's items that match the filter.
func (model Model) reviewRows() []Item {
	var rows []Item
	for _, item := range model.snapshot.Items {
		switch item.Kind {
		case itemReviewHeader:
			rows = append(rows, item)
		case itemReview:
			if matches(item, model.query) {
				rows = append(rows, item)
			}
		case itemProfile, itemParty:
		}
	}
	return rows
}

func (model Model) browserRows() []Item {
	if model.browserArea == AreaReviews {
		return model.reviewRows()
	}
	return model.visibleItems(model.browserKind())
}

func (model Model) cursorRow() (Item, bool) {
	rows := model.browserRows()
	if len(rows) == 0 {
		return Item{}, false
	}
	return rows[min(model.cursor, len(rows)-1)], true
}

// renderReviewList renders the stacked per-file sections and reports the
// line the cursor row starts on. A cursor of -1 marks no row.
func renderReviewList(rows []Item, cursor, width int, options renderOptions) (string, int) {
	var content strings.Builder
	cursorLine, line := 1, 1
	var previous Item
	for index, row := range rows {
		if label, ok := reviewGroupLabel(previous, row); ok {
			line += writeReviewLine(&content, renderReviewGroupLabel(label, options), width)
		}
		if index == cursor {
			cursorLine = line
		}
		line += writeReviewLine(&content, renderReviewRow(row, index == cursor, width, options), width)
		previous = row
	}
	if len(rows) == 0 {
		content.WriteString("No configuration file is open.\n")
	}
	return content.String(), cursorLine
}

func writeReviewLine(content *strings.Builder, text string, width int) int {
	content.WriteString(text)
	content.WriteString("\n")
	return wrappedLineCount(text, width)
}

// reviewGroupLabel names the selection group a Repository Configuration row
// starts. The Global file holds one group, so it gets no label.
func reviewGroupLabel(previous, row Item) (string, bool) {
	if row.Kind != itemReview || row.Target != configuration.ScopeRepository {
		return "", false
	}
	if previous.Kind == itemReview && previous.Scope == row.Scope {
		return "", false
	}
	if row.Scope == ItemScope(configuration.ScopeGlobal) {
		return "Global Profiles and Parties", true
	}
	return "Repository Profiles and Parties", true
}

func renderReviewGroupLabel(label string, options renderOptions) string {
	if options.styled {
		return "    " + metaStyle.Render(label)
	}
	return "    " + label
}

// renderReviewRow puts a file header's state on its own indented lines so a
// narrow pane wraps it under the title rather than back to the margin.
func renderReviewRow(row Item, focused bool, width int, options renderOptions) string {
	cursor := "  "
	if focused {
		cursor = "› "
	}
	if row.Kind != itemReviewHeader {
		return cursor + "  " + renderInventoryItem(row, options)
	}
	const indent = "    "
	title := row.Name
	state := indent + strings.ReplaceAll(lipgloss.Wrap(row.Detail, max(width-len(indent), 1), " "), "\n", "\n"+indent)
	if options.styled {
		title = sectionTitleStyle.Render(title)
		state = metaStyle.Render(state)
	}
	return cursor + title + "\n" + state
}

func renderRowDetail(snapshot Snapshot, row Item, options renderOptions) string {
	if row.Kind != itemReviewHeader {
		return renderInventoryItem(row, options)
	}
	title, path := row.Name, ""
	if file, ok := snapshot.Reviews.file(row.Target); ok && file.Path != "" {
		path = file.Path
	}
	if options.styled {
		title, path = sectionTitleStyle.Render(title), metaStyle.Render(path)
	}
	if path == "" {
		return title + "\n" + row.Detail
	}
	return title + "\n" + path + "\n" + row.Detail
}
