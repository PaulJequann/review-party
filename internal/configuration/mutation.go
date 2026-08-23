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

// Plan is a staged, validated set of changes. Nothing is written until
// Manager.Publish. Callers must check Valid after planning and can render the
// exported preview fields before deciding whether to publish the plan.
// Manager.Plan reports invalid intents, including nil, through Valid and Reason
// rather than its error result; Manager.Publish refuses an invalid Plan.
// Warnings is stable but remains empty until a later Configuration Hub slice
// defines it; callers must ignore it until then.
type Plan struct {
	Changes  []Change
	Scopes   []Scope
	Paths    []string
	Warnings []string
	Valid    bool
	Reason   string

	staged []stagedDocument
}

type stagedDocument struct {
	scope    Scope
	path     string
	document Document
}

// Plan stages typed intents against loaded documents and validates each
// complete resulting document. Planning writes nothing and creates no files.
func (manager *Manager) Plan(repository Repository, intents []Intent) (Plan, error) {
	loaded, err := manager.Load(repository)
	if err != nil {
		return Plan{}, err
	}
	staged := map[Scope]*stagedDocument{}
	plan := Plan{}
	for _, intent := range intents {
		change, err := manager.stageIntent(&plan, staged, loaded, repository, intent)
		if err != nil {
			plan.Valid = false
			plan.Reason = err.Error()
			return plan, nil
		}
		if change != nil {
			plan.Changes = append(plan.Changes, *change)
		}
	}
	if err := manager.validateStaged(&plan, staged); err != nil {
		return plan, nil
	}
	plan.Valid = true
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
	target, ok := staged[scope]
	if !ok {
		target = &stagedDocument{scope: scope, path: current.Path, document: current.Document}
		staged[scope] = target
	}
	before, hadBefore := intent.readIntent(target.document)
	intent.applyIntent(&target.document)
	after, hadAfter := intent.readIntent(target.document)
	path, err := manager.ConfigPath(scope, repository)
	if err != nil {
		return nil, err
	}
	target.path = path
	if before == after && hadBefore == hadAfter {
		return nil, nil
	}
	return &Change{Field: intent.intentField(), Scope: scope, Path: path, Before: before, After: after, HadBefore: hadBefore, HadAfter: hadAfter}, nil
}

func (manager *Manager) validateStaged(plan *Plan, staged map[Scope]*stagedDocument) error {
	for _, scope := range []Scope{ScopePersonal, ScopeRepository} {
		document, ok := staged[scope]
		if !ok {
			continue
		}
		if err := validateDocument(document.document, document.scope, manager); err != nil {
			plan.Valid = false
			plan.Reason = invalid(document.scope, document.path, err).Error()
			return err
		}
		plan.staged = append(plan.staged, *document)
		plan.Paths = append(plan.Paths, document.path)
		addScopeOnce(&plan.Scopes, document.scope)
	}
	return nil
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
