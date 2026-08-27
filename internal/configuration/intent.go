package configuration

import (
	"encoding/json"
	"fmt"
	"strings"
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
// Managed state is always a Global Configuration choice.
type SetStateDirectory struct {
	Directory string
}

// SetReviewSelection replaces the complete repository-owned default roll-up.
type SetReviewSelection struct {
	Selection ReviewSelection
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

// intentScope, intentField, applyIntent, and readIntent are deliberately
// package-private. Callers submit typed values through Intent; only this
// package knows how those values map to document fields or preview values.
func (intent SetDefaultReviewer) intentScope() Scope { return intent.Target }
func (intent SetDefaultReviewer) intentField() string {
	return "defaults.reviewer"
}
func (intent SetDefaultReviewer) applyIntent(document *Document) {
	document.Defaults.Reviewer = intent.Reviewer
}
func (intent SetDefaultReviewer) readIntent(document Document) (string, bool) {
	return authoredString(configurationText(document.Defaults.Reviewer))
}

func (intent SetReviewerEnabled) intentScope() Scope { return intent.Target }
func (intent SetReviewerEnabled) intentField() string {
	return reviewerField(intent.Reviewer, "enabled")
}
func (intent SetReviewerEnabled) applyIntent(document *Document) {
	policy := document.reviewerPolicy(intent.Reviewer)
	enabled := intent.Enabled
	policy.Enabled = &enabled
	document.Reviewers[intent.Reviewer] = policy
}
func (intent SetReviewerEnabled) readIntent(document Document) (string, bool) {
	policy, exists := document.Reviewers[intent.Reviewer]
	if !exists || policy.Enabled == nil {
		return "", false
	}
	return fmt.Sprintf("%t", *policy.Enabled), true
}

func (intent SetReviewerModel) intentScope() Scope { return intent.Target }
func (intent SetReviewerModel) intentField() string {
	return reviewerField(intent.Reviewer, "model")
}
func (intent SetReviewerModel) applyIntent(document *Document) {
	policy := document.reviewerPolicy(intent.Reviewer)
	policy.Model = intent.Model
	document.Reviewers[intent.Reviewer] = policy
}
func (intent SetReviewerModel) readIntent(document Document) (string, bool) {
	return authoredString(configurationText(document.Reviewers[intent.Reviewer].Model))
}

func (intent SetReviewerAllowedModels) intentScope() Scope { return intent.Target }
func (intent SetReviewerAllowedModels) intentField() string {
	return reviewerField(intent.Reviewer, "allowed_models")
}
func (intent SetReviewerAllowedModels) applyIntent(document *Document) {
	policy := document.reviewerPolicy(intent.Reviewer)
	if intent.Models == nil {
		policy.AllowedModels = nil
	} else {
		policy.AllowedModels = append([]string(nil), intent.Models...)
	}
	document.Reviewers[intent.Reviewer] = policy
}
func (intent SetReviewerAllowedModels) readIntent(document Document) (string, bool) {
	models := document.Reviewers[intent.Reviewer].AllowedModels
	if models == nil {
		return "", false
	}
	return strings.Join(models, ","), true
}

func (intent SetStateDirectory) intentScope() Scope { return ScopeGlobal }
func (intent SetStateDirectory) intentField() string {
	return "state_directory"
}
func (intent SetStateDirectory) applyIntent(document *Document) {
	document.StateDirectory = intent.Directory
}
func (intent SetStateDirectory) readIntent(document Document) (string, bool) {
	return authoredString(configurationText(document.StateDirectory))
}

func (intent SetReviewSelection) intentScope() Scope  { return ScopeRepository }
func (intent SetReviewSelection) intentField() string { return "reviews" }
func (intent SetReviewSelection) applyIntent(document *Document) {
	selection := intent.Selection
	selection = cloneReviewSelection(selection)
	document.Reviews = &selection
}
func (intent SetReviewSelection) readIntent(document Document) (string, bool) {
	if document.Reviews == nil {
		return "", false
	}
	payload, err := json.Marshal(document.Reviews)
	if err != nil {
		panic(fmt.Sprintf("encode review selection preview: %v", err))
	}
	return string(payload), true
}
