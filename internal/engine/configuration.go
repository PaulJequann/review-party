package engine

import (
	"fmt"
	"sort"

	"reviewparty/internal/configuration"
)

// InvalidConfigurationError reports a Personal or Repository Configuration
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

// configureReviewerCatalog applies an authored Personal Configuration document
// to the packaged reviewer catalog and validates the effective default.
func configureReviewerCatalog(catalog reviewerCatalog, document configuration.Document, path string) (reviewerCatalog, error) {
	configured := cloneReviewerCatalog(catalog)
	if err := applyReviewerPolicies(&configured, document.Reviewers); err != nil {
		return reviewerCatalog{}, InvalidConfigurationError{Path: path, Reason: err.Error()}
	}
	if err := applyDefaultReviewer(&configured, document.Defaults.Reviewer); err != nil {
		return reviewerCatalog{}, InvalidConfigurationError{Path: path, Reason: err.Error()}
	}
	if err := validateEffectiveDefault(configured); err != nil {
		return reviewerCatalog{}, InvalidConfigurationError{Path: path, Reason: err.Error()}
	}
	return configured, nil
}

func applyReviewerPolicies(catalog *reviewerCatalog, policies map[string]configuration.ReviewerPolicy) error {
	for id, policy := range policies {
		registration, exists := catalog.registrations[id]
		if !exists {
			return fmt.Errorf("unknown reviewer %q", id)
		}
		configured, err := applyReviewerPolicy(registration, policy)
		if err != nil {
			return err
		}
		catalog.registrations[id] = configured
	}
	return nil
}

func applyReviewerPolicy(registration reviewerRegistration, policy configuration.ReviewerPolicy) (reviewerRegistration, error) {
	if policy.Enabled != nil {
		registration.disabled = !*policy.Enabled
	}
	if policy.AllowedModels != nil {
		registration.allowedModels = canonicalModels(policy.AllowedModels)
		registration.modelAllowlistConfigured = true
	}
	if policy.Model != "" {
		registration.candidate.Model = policy.Model
	}
	if registration.disabled {
		return registration, nil
	}
	return registration, validateConfiguredModel(registration)
}

func applyDefaultReviewer(catalog *reviewerCatalog, reviewer string) error {
	if reviewer == "" {
		return nil
	}
	registration, exists := catalog.registrations[reviewer]
	if !exists {
		return fmt.Errorf("unknown default reviewer %q", reviewer)
	}
	if registration.disabled {
		return fmt.Errorf("default reviewer %q is disabled", reviewer)
	}
	catalog.defaultReviewer = reviewer
	return nil
}

func validateEffectiveDefault(catalog reviewerCatalog) error {
	reviewer := catalog.defaultReviewer
	if reviewer == "" {
		reviewer = defaultReviewer
	}
	registration, exists := catalog.registrations[reviewer]
	if !exists {
		return fmt.Errorf("unknown effective default reviewer %q", reviewer)
	}
	if registration.disabled {
		return fmt.Errorf("effective default reviewer %q is disabled", reviewer)
	}
	if registration.candidate.Model == "" {
		return fmt.Errorf("effective default reviewer %q requires a model", reviewer)
	}
	return nil
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

func validateConfiguredModel(registration reviewerRegistration) error {
	if registration.candidate.Model == "" || !registration.modelAllowlistConfigured {
		return nil
	}
	if !containsModel(registration.allowedModels, registration.candidate.Model) {
		return ReviewerModelNotAllowedError{Reviewer: registration.candidate.ID, Model: registration.candidate.Model, Allowed: registration.allowedModels}
	}
	return nil
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
