package configuration

import (
	"fmt"
)

// Intent is one typed configuration change request. Intents describe semantic
// changes; they never carry dotted JSON paths or raw documents. The package
// owns how each intent maps to a document field so callers only choose a
// supported operation and its typed values.
type Intent interface {
	intentScope() Scope
	intentField() string
	applyIntent(*Document)
	readIntent(Document) (string, bool)
}

// SetDefaultReviewer selects or clears (empty Reviewer) the default reviewer in one scope.
type SetDefaultReviewer struct {
	Target   Scope
	Reviewer string
}

// SetDefaultProfile selects or clears (empty Profile) the default profile in one scope.
type SetDefaultProfile struct {
	Target  Scope
	Profile string
}

// SetReviewerEnabled enables or disables one reviewer in one scope.
type SetReviewerEnabled struct {
	Target   Scope
	Reviewer string
	Enabled  bool
}

// SetReviewerModel selects or clears (empty Model) one reviewer's model in one scope.
type SetReviewerModel struct {
	Target   Scope
	Reviewer string
	Model    string
}

// SetReviewerAllowedModels restricts or clears (nil Models) one reviewer's model allowlist in one scope.
type SetReviewerAllowedModels struct {
	Target   Scope
	Reviewer string
	Models   []string
}

// SetStateDirectory selects or clears (empty Directory) the managed state location.
// Managed state is always a Personal Configuration choice.
type SetStateDirectory struct {
	Directory string
}

func reviewerField(reviewer, field string) string {
	return "reviewers." + reviewer + "." + field
}

func (document *Document) reviewerPolicy(id string) ReviewerPolicy {
	if document.Reviewers == nil {
		document.Reviewers = map[string]ReviewerPolicy{}
	}
	return document.Reviewers[id]
}

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
// Confirm. Callers can render the exported preview fields before deciding
// whether to confirm the plan.
type Plan struct {
	Changes  []Change
	Scopes   []Scope
	Paths    []string
	Warnings []string
	Valid    bool
	Reason   string

	manager *Manager
	staged  []stagedDocument
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
	plan := Plan{manager: manager}
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

// Confirm publishes this plan atomically. Callers should render the plan and
// obtain any user or automation confirmation before invoking this method.
func (plan Plan) Confirm() error {
	if plan.manager == nil {
		return fmt.Errorf("cannot confirm a change plan without its configuration manager")
	}
	return plan.manager.publish(plan)
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
