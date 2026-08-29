package engine

import (
	"fmt"
	"sort"

	"reviewparty/internal/configuration"
)

// InvalidConfigurationError reports a Global or Repository Configuration
// document that could not produce a usable effective configuration.
type InvalidConfigurationError struct {
	Path   string
	Reason string
}

func (failure InvalidConfigurationError) Error() string {
	return fmt.Sprintf("invalid configuration %q: %s", failure.Path, failure.Reason)
}

type ReviewerModelRequiredError struct {
	Reviewer string
}

func (failure ReviewerModelRequiredError) Error() string {
	return fmt.Sprintf("reviewer %q requires a model in configuration or an explicit selection", failure.Reviewer)
}

type ReviewerModelNotAllowedError struct {
	Reviewer string
	Model    string
	Allowed  []string
}

func (failure ReviewerModelNotAllowedError) Error() string {
	return fmt.Sprintf("model %q is not allowed for reviewer %q; expected %v", failure.Model, failure.Reviewer, failure.Allowed)
}

// applyEffectiveReviewerPolicies applies validated repository and Global
// reviewer settings without requiring the caller to know their precedence.
func applyEffectiveReviewerPolicies(catalog reviewerCatalog, effective configuration.Effective) reviewerCatalog {
	configured := cloneReviewerCatalog(catalog)
	for _, id := range effective.ReviewerIDs() {
		registration, exists := configured.registrations[id]
		if !exists {
			continue
		}
		settings, _ := effective.ReviewerPolicy(id)
		configured.registrations[id] = applyReviewerPolicy(registration, settings)
	}
	return configured
}

func applyReviewerPolicy(registration reviewerRegistration, settings configuration.ReviewerSettings) reviewerRegistration {
	if settings.Enabled.Authored {
		registration.enabled = settings.Enabled
	}
	if settings.AllowedModels.Authored {
		registration.allowedModels = canonicalModels(settings.AllowedModels.Value)
		registration.modelAllowlistConfigured = true
	}
	if settings.Model.Authored {
		registration.candidate.Model = settings.Model.Value
	}
	return registration
}

func cloneReviewerCatalog(catalog reviewerCatalog) reviewerCatalog {
	registrations := make([]reviewerRegistration, 0, len(catalog.registrations))
	for _, registration := range catalog.registrations {
		registrations = append(registrations, registration)
	}
	clone := newReviewerCatalog(registrations)
	clone.defaultReviewer = catalog.defaultReviewer
	return clone
}

func canonicalModels(models []string) []string {
	unique := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model != "" {
			unique[model] = struct{}{}
		}
	}
	canonical := make([]string, 0, len(unique))
	for model := range unique {
		canonical = append(canonical, model)
	}
	sort.Strings(canonical)
	return canonical
}

func containsModel(models []string, model string) bool {
	for _, allowed := range models {
		if allowed == model {
			return true
		}
	}
	return false
}
