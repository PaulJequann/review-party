package configuration

import (
	"fmt"
	"path/filepath"
	"strings"
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
	owner       *Manager
	changes     []Change
	scopes      []Scope
	paths       []string
	warnings    []string
	valid       bool
	reason      string
	publication publicationPlan
}

type stagedDocument struct {
	scope                  Scope
	path                   string
	anchor                 string
	document               Document
	baseline               fileState
	reviewSelectionChanged bool
}

type fileState struct {
	existed bool
	payload []byte
}

// Valid reports whether the staged changes passed validation.
func (plan Plan) Valid() bool {
	return plan.state != nil && plan.state.valid
}

// Unchanged reports whether a valid Plan writes nothing.
func (plan Plan) Unchanged() bool {
	return plan.Valid() && len(plan.Changes()) == 0
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

// Warnings returns advisory validation messages for the staged changes.
func (plan Plan) Warnings() []string {
	if plan.state == nil {
		return nil
	}
	return append([]string(nil), plan.state.warnings...)
}

// WithWarnings returns a copy of the Plan with caller-supplied advisory
// messages. It does not change the staged publication.
func (plan Plan) WithWarnings(warnings ...string) Plan {
	if plan.state == nil {
		return plan
	}
	state := *plan.state
	state.warnings = append([]string(nil), state.warnings...)
	for _, warning := range warnings {
		if strings.TrimSpace(warning) != "" {
			state.warnings = append(state.warnings, warning)
		}
	}
	return Plan{state: &state}
}

// Plan stages typed intents against loaded documents and validates each
// complete resulting document. Planning writes nothing and creates no files.
func (manager *Manager) Plan(repository Repository, intents []Intent) (Plan, error) {
	loaded, err := manager.Load(repository)
	if err != nil {
		return Plan{}, err
	}
	staged := map[Scope]*stagedDocument{}
	plan := Plan{state: &planState{owner: manager}}
	for _, intent := range intents {
		change, err := manager.stageIntent(&plan, staged, loaded, repository, intent)
		if err != nil {
			plan.state.reason = err.Error()
			return plan, nil
		}
		if change != nil {
			plan.state.changes = append(plan.state.changes, *change)
			markReviewSelectionChange(staged, intent)
		}
	}
	if err := manager.validateStaged(&plan, staged, loaded, repository); err != nil {
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
	case ScopeGlobal, ScopeRepository:
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

func markReviewSelectionChange(staged map[Scope]*stagedDocument, intent Intent) {
	if _, ok := intent.(SetReviewSelection); !ok {
		return
	}
	staged[ScopeRepository].reviewSelectionChanged = true
}

func (manager *Manager) validateStaged(plan *Plan, staged map[Scope]*stagedDocument, loaded Loaded, repository Repository) error {
	for _, scope := range []Scope{ScopeGlobal, ScopeRepository} {
		document, ok := staged[scope]
		if !ok {
			continue
		}
		if err := validateDocument(document.document, document.scope, manager); err != nil {
			plan.state.reason = invalid(document.scope, document.path, err).Error()
			return err
		}
		if document.reviewSelectionChanged {
			if err := validateReviewSelectionReferences(*document.document.Reviews, repository, manager); err != nil {
				plan.state.reason = invalid(document.scope, document.path, err).Error()
				return err
			}
		}
		payload, err := renderDocument(document.document)
		if err != nil {
			plan.state.reason = err.Error()
			return err
		}
		write := pendingWrite{
			scope: document.scope, anchor: document.anchor, path: document.path,
			payload: payload, backup: append([]byte(nil), document.baseline.payload...), existed: document.baseline.existed,
		}
		addPlanFiles(plan.state, document.scope, []pendingWrite{write})
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
			loaded.Global = document
		}
	}
	return loaded
}

func loadedScopeFor(loaded Loaded, scope Scope) LoadedDocument {
	if scope == ScopeRepository {
		return loaded.Repository
	}
	return loaded.Global
}

func newFilePlan(manager *Manager, scope Scope, change Change, writes []pendingWrite) Plan {
	state := &planState{owner: manager, valid: true, changes: []Change{change}}
	addPlanFiles(state, scope, writes)
	return Plan{state: state}
}

func newProfilePlan(manager *Manager, scope Scope, change Change, publication pendingProfilePublication) Plan {
	state := &planState{
		owner: manager, valid: true, scopes: []Scope{scope}, changes: []Change{change}, publication: publicationPlan{profiles: []pendingProfilePublication{publication}},
		paths: []string{filepath.Join(publication.directory, "profile.json"), filepath.Join(publication.directory, "instructions.md")},
	}
	return Plan{state: state}
}

// mergePlans joins valid Plans into one that publishes all of them or none.
// The first invalid Plan is returned unchanged.
func mergePlans(manager *Manager, plans []Plan) Plan {
	merged := &planState{owner: manager, valid: true}
	for _, plan := range plans {
		if !plan.Valid() {
			return plan
		}
		for _, scope := range plan.state.scopes {
			addScopeOnce(&merged.scopes, scope)
		}
		merged.changes = append(merged.changes, plan.state.changes...)
		merged.paths = append(merged.paths, plan.state.paths...)
		merged.warnings = append(merged.warnings, plan.state.warnings...)
		merged.publication.files = append(merged.publication.files, plan.state.publication.files...)
		merged.publication.profiles = append(merged.publication.profiles, plan.state.publication.profiles...)
	}
	return Plan{state: merged}
}

func addPlanFiles(state *planState, scope Scope, writes []pendingWrite) {
	addScopeOnce(&state.scopes, scope)
	state.publication.files = append(state.publication.files, writes...)
	for _, write := range writes {
		state.paths = append(state.paths, write.path)
	}
}

func addScopeOnce(scopes *[]Scope, scope Scope) {
	for _, existing := range *scopes {
		if existing == scope {
			return
		}
	}
	*scopes = append(*scopes, scope)
}
