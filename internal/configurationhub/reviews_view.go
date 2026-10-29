package configurationhub

import (
	"fmt"
	"slices"

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
	view := ReviewsView{InForce: inForceTarget(loaded.ReviewSelection().Source)}
	for _, target := range targets {
		document := loadedDocument(loaded, target)
		view.Files = append(view.Files, ReviewFile{Target: target, Path: document.Path, Present: document.Present, Selection: document.Document.Reviews})
	}
	return view
}

func loadedDocument(loaded configuration.Loaded, target configuration.Scope) configuration.LoadedDocument {
	if target == configuration.ScopeRepository {
		return loaded.Repository
	}
	return loaded.Global
}

func inForceTarget(source configuration.Source) configuration.Scope {
	switch source {
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
