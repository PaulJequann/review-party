package configuration

import (
	"fmt"
)

// Change planning lives in this file. Typed intents and their field mapping
// live in intent.go; Change, Plan staging, and validation live here.

// Change records one semantic staged change to one field of one file.
type Change struct {
	Field     string
	Scope     Scope
	Path      string
	Before    string
	After     string
	HadBefore bool
	HadAfter  bool
}

// Plan is an opaque, staged set of changes created by Manager.Plan. Nothing is
// written until Manager.Publish. Its methods expose a read-only preview;
// callers cannot construct or mutate publishable state.
type Plan struct {
	state *planState
}

type planState struct {
	changes []Change
	scopes  []Scope
	paths   []string
	valid   bool
	reason  string
	staged  []stagedDocument
}

type stagedDocument struct {
	scope    Scope
	path     string
	anchor   string
	document Document
	baseline fileState
}

type fileState struct {
	existed bool
	payload []byte
}

// Valid reports whether the staged changes passed validation.
func (plan Plan) Valid() bool {
	return plan.state != nil && plan.state.valid
}

// Reason explains why a plan is invalid.
func (plan Plan) Reason() string {
	if plan.state == nil {
		return "plan was not created by Manager.Plan"
	}
	return plan.state.reason
}

// Changes returns a copy of the semantic changes for preview.
func (plan Plan) Changes() []Change {
	if plan.state == nil {
		return nil
	}
	return append([]Change(nil), plan.state.changes...)
}

// Scopes returns a copy of the affected scopes for preview.
func (plan Plan) Scopes() []Scope {
	if plan.state == nil {
		return nil
	}
	return append([]Scope(nil), plan.state.scopes...)
}

// Paths returns a copy of the affected paths for preview.
func (plan Plan) Paths() []string {
	if plan.state == nil {
		return nil
	}
	return append([]string(nil), plan.state.paths...)
}

// Plan stages typed intents against loaded documents and validates each
// complete resulting document. Planning writes nothing and creates no files.
func (manager *Manager) Plan(repository Repository, intents []Intent) (Plan, error) {
	loaded, err := manager.Load(repository)
	if err != nil {
		return Plan{}, err
	}
	staged := map[Scope]*stagedDocument{}
	plan := Plan{state: &planState{}}
	for _, intent := range intents {
		change, err := manager.stageIntent(&plan, staged, loaded, repository, intent)
		if err != nil {
			plan.state.reason = err.Error()
			return plan, nil
		}
		if change != nil {
			plan.state.changes = append(plan.state.changes, *change)
		}
	}
	if err := manager.validateStaged(&plan, staged, loaded); err != nil {
		return plan, nil
	}
	plan.state.valid = true
	return plan, nil
}

func (manager *Manager) stageIntent(plan *Plan, staged map[Scope]*stagedDocument, loaded Loaded, repository Repository, intent Intent) (*Change, error) {
	if intent == nil {
		return nil, fmt.Errorf("configuration intent must not be nil")
	}
	scope := intent.intentScope()
	switch scope {
	case ScopePersonal, ScopeRepository:
	default:
		return nil, fmt.Errorf("unknown configuration scope %q", scope)
	}
	current := loadedScopeFor(loaded, scope)
	target, alreadyStaged := staged[scope]
	if !alreadyStaged {
		target = &stagedDocument{
			scope: scope, path: current.Path, document: current.Document,
			baseline: fileState{existed: current.Present, payload: append([]byte(nil), current.payload...)},
		}
	}
	before, hadBefore := intent.readIntent(target.document)
	intent.applyIntent(&target.document)
	after, hadAfter := intent.readIntent(target.document)
	if before == after && hadBefore == hadAfter {
		return nil, nil
	}
	path, anchor, err := manager.configPathAndAnchor(scope, repository)
	if err != nil {
		return nil, err
	}
	target.path = path
	target.anchor = anchor
	if !alreadyStaged {
		staged[scope] = target
	}
	return &Change{Field: intent.intentField(), Scope: scope, Path: path, Before: before, After: after, HadBefore: hadBefore, HadAfter: hadAfter}, nil
}

func (manager *Manager) validateStaged(plan *Plan, staged map[Scope]*stagedDocument, loaded Loaded) error {
	for _, scope := range []Scope{ScopePersonal, ScopeRepository} {
		document, ok := staged[scope]
		if !ok {
			continue
		}
		if err := validateDocument(document.document, document.scope, manager); err != nil {
			plan.state.reason = invalid(document.scope, document.path, err).Error()
			return err
		}
		plan.state.staged = append(plan.state.staged, *document)
		plan.state.paths = append(plan.state.paths, document.path)
		addScopeOnce(&plan.state.scopes, document.scope)
	}
	if _, err := manager.resolveEffectiveReviewers(withStagedDocuments(loaded, staged)); err != nil {
		plan.state.reason = err.Error()
		return err
	}
	return nil
}

func withStagedDocuments(loaded Loaded, staged map[Scope]*stagedDocument) Loaded {
	for scope, stagedDocument := range staged {
		document := LoadedDocument{
			Scope: scope, Path: stagedDocument.path, Present: true, Document: stagedDocument.document,
		}
		if scope == ScopeRepository {
			loaded.Repository = document
		} else {
			loaded.Personal = document
		}
	}
	return loaded
}

func loadedScopeFor(loaded Loaded, scope Scope) LoadedDocument {
	if scope == ScopeRepository {
		return loaded.Repository
	}
	return loaded.Personal
}

func addScopeOnce(scopes *[]Scope, scope Scope) {
	for _, existing := range *scopes {
		if existing == scope {
			return
		}
	}
	*scopes = append(*scopes, scope)
}
