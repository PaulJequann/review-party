package configuration

import (
	"fmt"
	"strings"
)

// Mutation is one typed configuration change request. Mutations describe
// semantic changes; they never carry dotted JSON paths or raw documents.
type Mutation interface {
	TargetScope() Scope
	Field() string
	Apply(document *Document)
	Read(document Document) (string, bool)
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

func (mutation SetDefaultReviewer) TargetScope() Scope { return mutation.Target }
func (mutation SetDefaultReviewer) Field() string      { return "defaults.reviewer" }

func (mutation SetDefaultReviewer) Apply(document *Document) {
	document.Defaults.Reviewer = mutation.Reviewer
}

func (mutation SetDefaultReviewer) Read(document Document) (string, bool) {
	return authoredString(configurationText(document.Defaults.Reviewer))
}

func (mutation SetDefaultProfile) TargetScope() Scope { return mutation.Target }
func (mutation SetDefaultProfile) Field() string      { return "defaults.profile" }

func (mutation SetDefaultProfile) Apply(document *Document) {
	document.Defaults.Profile = mutation.Profile
}

func (mutation SetDefaultProfile) Read(document Document) (string, bool) {
	return authoredString(configurationText(document.Defaults.Profile))
}

func (mutation SetReviewerEnabled) TargetScope() Scope { return mutation.Target }
func (mutation SetReviewerEnabled) Field() string      { return reviewerField(mutation.Reviewer, "enabled") }

func (mutation SetReviewerEnabled) Apply(document *Document) {
	policy := document.reviewerPolicy(mutation.Reviewer)
	enabled := mutation.Enabled
	policy.Enabled = &enabled
	document.Reviewers[mutation.Reviewer] = policy
}

func (mutation SetReviewerEnabled) Read(document Document) (string, bool) {
	policy, exists := document.Reviewers[mutation.Reviewer]
	if !exists || policy.Enabled == nil {
		return "", false
	}
	return fmt.Sprintf("%t", *policy.Enabled), true
}

func (mutation SetReviewerModel) TargetScope() Scope { return mutation.Target }
func (mutation SetReviewerModel) Field() string      { return reviewerField(mutation.Reviewer, "model") }

func (mutation SetReviewerModel) Apply(document *Document) {
	policy := document.reviewerPolicy(mutation.Reviewer)
	policy.Model = mutation.Model
	document.Reviewers[mutation.Reviewer] = policy
}

func (mutation SetReviewerModel) Read(document Document) (string, bool) {
	return authoredString(configurationText(document.Reviewers[mutation.Reviewer].Model))
}

func (mutation SetReviewerAllowedModels) TargetScope() Scope { return mutation.Target }
func (mutation SetReviewerAllowedModels) Field() string {
	return reviewerField(mutation.Reviewer, "allowed_models")
}

func (mutation SetReviewerAllowedModels) Apply(document *Document) {
	policy := document.reviewerPolicy(mutation.Reviewer)
	if mutation.Models == nil {
		policy.AllowedModels = nil
	} else {
		policy.AllowedModels = append([]string(nil), mutation.Models...)
	}
	document.Reviewers[mutation.Reviewer] = policy
}

func (mutation SetReviewerAllowedModels) Read(document Document) (string, bool) {
	models := document.Reviewers[mutation.Reviewer].AllowedModels
	if models == nil {
		return "", false
	}
	return strings.Join(models, ","), true
}

func (mutation SetStateDirectory) TargetScope() Scope { return ScopePersonal }
func (mutation SetStateDirectory) Field() string      { return "state_directory" }

func (mutation SetStateDirectory) Apply(document *Document) {
	document.StateDirectory = mutation.Directory
}

func (mutation SetStateDirectory) Read(document Document) (string, bool) {
	return authoredString(configurationText(document.StateDirectory))
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

// Plan is a staged, validated set of changes. Nothing is written until Publish.
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

// Plan stages mutations against loaded documents and validates each complete
// resulting document. Planning writes nothing and creates no files.
func (manager *Manager) Plan(repository Repository, mutations []Mutation) (Plan, error) {
	loaded, err := manager.Load(repository)
	if err != nil {
		return Plan{}, err
	}
	staged := map[Scope]*stagedDocument{}
	plan := Plan{}
	for _, mutation := range mutations {
		change, err := manager.stageMutation(&plan, staged, loaded, repository, mutation)
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

func (manager *Manager) stageMutation(plan *Plan, staged map[Scope]*stagedDocument, loaded Loaded, repository Repository, mutation Mutation) (*Change, error) {
	switch mutation.TargetScope() {
	case ScopePersonal, ScopeRepository:
	default:
		return nil, fmt.Errorf("unknown configuration scope %q", mutation.TargetScope())
	}
	current := loadedScopeFor(loaded, mutation.TargetScope())
	target, ok := staged[mutation.TargetScope()]
	if !ok {
		target = &stagedDocument{scope: mutation.TargetScope(), path: current.Path, document: current.Document}
		staged[mutation.TargetScope()] = target
	}
	before, hadBefore := mutation.Read(target.document)
	mutation.Apply(&target.document)
	after, hadAfter := mutation.Read(target.document)
	path, err := manager.ConfigPath(mutation.TargetScope(), repository)
	if err != nil {
		return nil, err
	}
	target.path = path
	if before == after && hadBefore == hadAfter {
		return nil, nil
	}
	return &Change{Field: mutation.Field(), Scope: mutation.TargetScope(), Path: path, Before: before, After: after, HadBefore: hadBefore, HadAfter: hadAfter}, nil
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
