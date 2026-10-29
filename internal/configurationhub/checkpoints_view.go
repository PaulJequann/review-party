package configurationhub

import (
	"fmt"
	"slices"
	"strings"

	"reviewparty/internal/configuration"
)

// CheckpointField is what one file's checkpoints field says: nothing, which
// inherits; an empty object, which declares none; or Checkpoints.
type CheckpointField int

const (
	CheckpointsAbsent CheckpointField = iota
	CheckpointsNone
	CheckpointsDeclared
)

// CheckpointFile is one configuration file's Checkpoints as the Hub shows and
// edits them.
type CheckpointFile struct {
	Target   configuration.Scope
	Path     string
	Present  bool
	Field    CheckpointField
	Declared map[configuration.CheckpointName]configuration.Checkpoint
}

// CheckpointsView lists every configuration file the Hub can edit, in display
// order, and names the file whose Checkpoints are in force here. InForce is
// empty when none are. Enrolled reports whether the Repository Configuration
// file exists, which is what puts Global Checkpoints in force.
type CheckpointsView struct {
	InForce  configuration.Scope
	Enrolled bool
	Files    []CheckpointFile
}

func newCheckpointsView(loaded configuration.Loaded, targets []configuration.Scope) CheckpointsView {
	view := CheckpointsView{InForce: inForceTarget(loaded.Checkpoints().Source), Enrolled: loaded.Repository.Present}
	for _, target := range targets {
		document := loadedDocument(loaded, target)
		checkpoints := document.Document.Checkpoints
		field := CheckpointsDeclared
		switch {
		case checkpoints == nil:
			field = CheckpointsAbsent
		case len(checkpoints) == 0:
			field = CheckpointsNone
		}
		view.Files = append(view.Files, CheckpointFile{Target: target, Path: document.Path, Present: document.Present, Field: field, Declared: checkpoints})
	}
	return view
}

func (view CheckpointsView) file(target configuration.Scope) (CheckpointFile, bool) {
	for _, file := range view.Files {
		if file.Target == target {
			return file, true
		}
	}
	return CheckpointFile{}, false
}

func (view CheckpointsView) inForceCount() int {
	file, ok := view.file(view.InForce)
	if !ok {
		return 0
	}
	return len(file.Declared)
}

func (view CheckpointsView) enrollment() configuration.Enrollment {
	global, _ := view.file(configuration.ScopeGlobal)
	return configuration.Enrollment{Enrolled: view.Enrolled, GlobalCheckpoints: global.Declared}
}

func (view CheckpointsView) headerState(file CheckpointFile) string {
	repository := file.Target == configuration.ScopeRepository
	_, hasRepository := view.file(configuration.ScopeRepository)
	switch {
	case repository && !file.Present && len(view.enrollment().GlobalCheckpoints) > 0:
		return "no file: not enrolled; the Global Checkpoints apply once enrolled"
	case repository && !file.Present:
		return "no file: not enrolled"
	case repository && file.Field == CheckpointsAbsent:
		return "inherits the Global Checkpoints"
	case repository && file.Field == CheckpointsNone:
		return "opted out: declares no Checkpoints, so the Global Checkpoints do not apply"
	case file.Field != CheckpointsDeclared:
		return "declares no Checkpoints"
	case view.InForce == file.Target && repository && len(view.enrollment().GlobalCheckpoints) > 0:
		return "● in force · " + file.names() + "; replaces the Global Checkpoints"
	case view.InForce == file.Target:
		return "● in force · " + file.names()
	case !hasRepository:
		return "applies to every enrolled repository that declares no Checkpoints · " + file.names()
	case !view.Enrolled:
		return "not in force here: this repository is not enrolled · " + file.names()
	}
	return "replaced here by Repository Configuration · " + file.names()
}

func (file CheckpointFile) names() string {
	var names []string
	for _, name := range configuration.SortedCheckpointNames(file.Declared) {
		names = append(names, string(name))
	}
	return strings.Join(names, ", ")
}

func (view CheckpointsView) operations(file CheckpointFile) []checkpointOperation {
	operations := []checkpointOperation{checkpointSet}
	if file.Field == CheckpointsDeclared {
		operations = append(operations, checkpointRemove)
	}
	if file.Target != configuration.ScopeRepository {
		return operations
	}
	if !file.Present {
		if view.enrollment().Offered() {
			operations = append(operations, checkpointEnroll)
		}
		return operations
	}
	if file.Field != CheckpointsAbsent {
		operations = append(operations, checkpointInherit)
	}
	if file.Field != CheckpointsNone {
		operations = append(operations, checkpointOptOut)
	}
	return operations
}

func (view CheckpointsView) rowOperations(row Item) []checkpointOperation {
	file, ok := view.file(row.Target)
	if !ok {
		return nil
	}
	var operations []checkpointOperation
	for _, operation := range view.operations(file) {
		switch operation {
		case checkpointSet:
			operations = append(operations, operation)
		case checkpointRemove:
			if row.Kind == itemCheckpoint && row.Declared {
				operations = append(operations, operation)
			}
		case checkpointEnroll, checkpointInherit, checkpointOptOut:
			if row.Kind == itemCheckpointHeader {
				operations = append(operations, operation)
			}
		}
	}
	return operations
}

func (view CheckpointsView) prefill(target configuration.Scope, name configuration.CheckpointName, fallback configuration.Checkpoint) configuration.Checkpoint {
	for _, scope := range []configuration.Scope{target, configuration.ScopeGlobal} {
		if file, ok := view.file(scope); ok {
			if checkpoint, declared := file.Declared[name]; declared {
				return checkpoint
			}
		}
	}
	return fallback
}

func (view CheckpointsView) setDraft(target configuration.Scope, name configuration.CheckpointName, fallback configuration.Checkpoint) checkpointFormDraft {
	return checkpointFormDraft{target: target, operation: checkpointSet, checkpointDraft: newCheckpointDraft(name, view.prefill(target, name, fallback))}
}

func appendHubCheckpointFile(snapshot *Snapshot, file CheckpointFile) {
	snapshot.Items = append(snapshot.Items, Item{
		Target: file.Target, Scope: ItemScope(file.Target), Kind: itemCheckpointHeader,
		Name: configurationTitle(file.Target), Detail: snapshot.Checkpoints.headerState(file),
	})
	for _, name := range configuration.CheckpointNames() {
		checkpoint, declared := file.Declared[name]
		detail := "not declared"
		if declared {
			detail = strings.TrimPrefix(checkpoint.Summary(name), string(name)+": ")
		}
		snapshot.Items = append(snapshot.Items, Item{Target: file.Target, Scope: ItemScope(file.Target), Kind: itemCheckpoint, Name: string(name), Detail: detail, Declared: declared})
	}
}

type checkpointKey struct {
	keys      []string
	help      string
	operation checkpointOperation
	label     string
}

var checkpointKeys = []checkpointKey{
	{keys: []string{"e", "enter"}, help: "e/⏎", operation: checkpointSet, label: "set"},
	{keys: []string{"r"}, help: "r", operation: checkpointRemove, label: "remove"},
	{keys: []string{"i"}, help: "i", operation: checkpointEnroll, label: "enroll"},
	{keys: []string{"i"}, help: "i", operation: checkpointInherit, label: "inherit Global"},
	{keys: []string{"o"}, help: "o", operation: checkpointOptOut, label: "opt out"},
}

func checkpointKeyOperation(key string, offered []checkpointOperation) (operation checkpointOperation, bound bool, refusal string) {
	var labels []string
	for _, binding := range checkpointKeys {
		if !slices.Contains(binding.keys, key) {
			continue
		}
		if slices.Contains(offered, binding.operation) {
			return binding.operation, true, ""
		}
		labels = append(labels, binding.operation.label())
	}
	if len(labels) == 0 {
		return 0, false, ""
	}
	return 0, true, fmt.Sprintf("%s does not apply to this row.", strings.Join(labels, " or "))
}

func (model Model) cursorCheckpointKeys() []checkpointKey {
	row, ok := model.cursorRow()
	if !ok {
		return nil
	}
	offered := model.snapshot.Checkpoints.rowOperations(row)
	var keys []checkpointKey
	for _, binding := range checkpointKeys {
		if slices.Contains(offered, binding.operation) {
			keys = append(keys, binding)
		}
	}
	return keys
}
