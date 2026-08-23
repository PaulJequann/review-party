package configuration

import (
	"fmt"
	"strings"
)

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

func (intent SetDefaultProfile) intentScope() Scope { return intent.Target }
func (intent SetDefaultProfile) intentField() string {
	return "defaults.profile"
}
func (intent SetDefaultProfile) applyIntent(document *Document) {
	document.Defaults.Profile = intent.Profile
}
func (intent SetDefaultProfile) readIntent(document Document) (string, bool) {
	return authoredString(configurationText(document.Defaults.Profile))
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

func (intent SetStateDirectory) intentScope() Scope { return ScopePersonal }
func (intent SetStateDirectory) intentField() string {
	return "state_directory"
}
func (intent SetStateDirectory) applyIntent(document *Document) {
	document.StateDirectory = intent.Directory
}
func (intent SetStateDirectory) readIntent(document Document) (string, bool) {
	return authoredString(configurationText(document.StateDirectory))
}
