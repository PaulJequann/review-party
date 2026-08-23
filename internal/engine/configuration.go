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

// configureReviewerCatalog applies Effective Configuration to the packaged
// reviewer catalog and validates the effective default.
func configureReviewerCatalog(catalog reviewerCatalog, effective configuration.Effective) (reviewerCatalog, error) {
	configured, err := applyEffectiveReviewerPolicies(catalog, effective)
	if err != nil {
		return reviewerCatalog{}, err
	}
	if effective.DefaultReviewer.Authored {
		if err := applyDefaultReviewer(&configured, effective.DefaultReviewer.Value); err != nil {
			return reviewerCatalog{}, InvalidConfigurationError{Path: effective.DefaultReviewer.Path, Reason: err.Error()}
		}
	}
	if err := validateEffectiveDefault(configured); err != nil {
		return reviewerCatalog{}, InvalidConfigurationError{Path: effective.DefaultReviewer.Path, Reason: err.Error()}
	}
	return configured, nil
}

// applyEffectiveReviewerPolicies applies repository and Personal reviewer
// settings without requiring the caller to know their precedence. It returns
// the exact authored path when a policy cannot be applied.
func applyEffectiveReviewerPolicies(catalog reviewerCatalog, effective configuration.Effective) (reviewerCatalog, error) {
	configured := cloneReviewerCatalog(catalog)
	for id := range effective.Reviewers {
		registration, exists := configured.registrations[id]
		if !exists {
			continue
		}
		settings, _ := effective.ReviewerPolicy(id)
		updated, err := applyReviewerPolicy(registration, settings)
		if err != nil {
			return reviewerCatalog{}, err
		}
		configured.registrations[id] = updated
	}
	return configured, nil
}

func applyReviewerPolicy(registration reviewerRegistration, settings configuration.ReviewerSettings) (reviewerRegistration, error) {
	if settings.Enabled.Authored {
		registration.disabled = !settings.Enabled.Value
		registration.disabledSource = string(settings.Enabled.Source)
		registration.disabledPath = settings.Enabled.Path
	}
	if settings.AllowedModels.Authored {
		registration.allowedModels = canonicalModels(settings.AllowedModels.Value)
		registration.modelAllowlistConfigured = true
	}
	if settings.Model.Authored {
		registration.candidate.Model = settings.Model.Value
	}
	if packagedModelIsDisallowed(registration, settings) {
		reason := ReviewerModelNotAllowedError{Reviewer: registration.candidate.ID, Model: registration.candidate.Model, Allowed: registration.allowedModels}
		return reviewerRegistration{}, InvalidConfigurationError{Path: settings.AllowedModels.Path, Reason: reason.Error()}
	}
	return registration, nil
}

func packagedModelIsDisallowed(registration reviewerRegistration, settings configuration.ReviewerSettings) bool {
	if registration.disabled || registration.candidate.Model == "" {
		return false
	}
	if settings.Model.Authored || !settings.AllowedModels.Authored {
		return false
	}
	return !containsModel(registration.allowedModels, registration.candidate.Model)
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
