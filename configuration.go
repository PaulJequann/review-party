package reviewparty

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

type userConfiguration struct {
	Version         int                           `json:"version"`
	DefaultReviewer string                        `json:"default_reviewer,omitempty"`
	Reviewers       map[string]userReviewerPolicy `json:"reviewers,omitempty"`
}

func (configuration *userConfiguration) UnmarshalJSON(payload []byte) error {
	type plainConfiguration userConfiguration
	var decoded plainConfiguration
	if err := decodeStrictObject(payload, &decoded, "user configuration", "default_reviewer", "reviewers"); err != nil {
		return err
	}
	*configuration = userConfiguration(decoded)
	return nil
}

type userReviewerPolicy struct {
	Enabled       *bool                    `json:"enabled,omitempty"`
	Model         configuredReviewerModel  `json:"model,omitempty"`
	AllowedModels configuredModelAllowlist `json:"allowed_models,omitempty"`
}

type configuredReviewerModel struct {
	value   string
	present bool
}

func (model *configuredReviewerModel) UnmarshalJSON(payload []byte) error {
	var value string
	if err := json.Unmarshal(payload, &value); err != nil {
		return err
	}
	if value == "" {
		return errors.New("model must not be empty")
	}
	model.value = value
	model.present = true
	return nil
}

func (policy *userReviewerPolicy) UnmarshalJSON(payload []byte) error {
	type plainPolicy userReviewerPolicy
	var decoded plainPolicy
	if err := decodeStrictObject(payload, &decoded, "reviewer policy", "enabled", "model"); err != nil {
		return err
	}
	*policy = userReviewerPolicy(decoded)
	return nil
}

func decodeStrictObject(payload []byte, destination any, objectName string, nonNullFields ...string) error {
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return fmt.Errorf("%s must be an object, not null", objectName)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return err
	}
	for _, name := range nonNullFields {
		if value, exists := fields[name]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s must not be null", name)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return rejectTrailingJSON(decoder)
}

type configuredModelAllowlist struct {
	models  []string
	present bool
}

func (allowlist *configuredModelAllowlist) UnmarshalJSON(payload []byte) error {
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return errors.New("allowed_models must be an array, not null")
	}
	var models []string
	if err := json.Unmarshal(payload, &models); err != nil {
		return err
	}
	allowlist.models = models
	allowlist.present = true
	return nil
}

type InvalidUserConfigurationError struct {
	Path   string
	Reason string
}

func (failure InvalidUserConfigurationError) Error() string {
	return fmt.Sprintf("invalid user configuration %q: %s", failure.Path, failure.Reason)
}

type ReviewerModelRequiredError struct {
	Reviewer string
}

func (failure ReviewerModelRequiredError) Error() string {
	return fmt.Sprintf("reviewer %q requires a model in user configuration or an explicit selection", failure.Reviewer)
}

type ReviewerModelNotAllowedError struct {
	Reviewer string
	Model    string
	Allowed  []string
}

func (failure ReviewerModelNotAllowedError) Error() string {
	return fmt.Sprintf("model %q is not allowed for reviewer %q; expected %v", failure.Model, failure.Reviewer, failure.Allowed)
}

func loadUserConfiguration(path string) (userConfiguration, error) {
	if path == "" {
		return userConfiguration{}, nil
	}
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return userConfiguration{}, nil
	}
	if err != nil {
		return userConfiguration{}, InvalidUserConfigurationError{Path: path, Reason: err.Error()}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var configuration userConfiguration
	if err := decoder.Decode(&configuration); err != nil {
		return userConfiguration{}, InvalidUserConfigurationError{Path: path, Reason: err.Error()}
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return userConfiguration{}, InvalidUserConfigurationError{Path: path, Reason: err.Error()}
	}
	if configuration.Version != 1 {
		return userConfiguration{}, InvalidUserConfigurationError{Path: path, Reason: fmt.Sprintf("unsupported version %d", configuration.Version)}
	}
	return configuration, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values")
}

func configureReviewerCatalog(catalog reviewerCatalog, configuration userConfiguration, path string) (reviewerCatalog, error) {
	configured := cloneReviewerCatalog(catalog)
	if err := applyReviewerPolicies(&configured, configuration.Reviewers); err != nil {
		return reviewerCatalog{}, invalidConfiguration(path, err)
	}
	if err := applyDefaultReviewer(&configured, configuration.DefaultReviewer); err != nil {
		return reviewerCatalog{}, invalidConfiguration(path, err)
	}
	if err := validateEffectiveDefault(configured); err != nil {
		return reviewerCatalog{}, invalidConfiguration(path, err)
	}
	return configured, nil
}

func applyReviewerPolicies(catalog *reviewerCatalog, policies map[string]userReviewerPolicy) error {
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

func applyReviewerPolicy(registration reviewerRegistration, policy userReviewerPolicy) (reviewerRegistration, error) {
	if policy.Enabled != nil {
		registration.disabled = !*policy.Enabled
	}
	if policy.AllowedModels.present {
		registration.allowedModels = canonicalModels(policy.AllowedModels.models)
		registration.modelAllowlistConfigured = true
	}
	if policy.Model.present {
		registration.candidate.Model = policy.Model.value
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

func invalidConfiguration(path string, err error) InvalidUserConfigurationError {
	return InvalidUserConfigurationError{Path: path, Reason: err.Error()}
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
