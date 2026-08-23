package configuration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
)

// SchemaVersion is the single configuration schema version used by Personal
// and Repository documents.
const SchemaVersion = 1

// Defaults holds the selection defaults authored in one scope.
type Defaults struct {
	Reviewer string `json:"reviewer,omitempty"`
	Profile  string `json:"profile,omitempty"`
}

func (defaults *Defaults) UnmarshalJSON(payload []byte) error {
	if _, err := decodeObjectFields(payload, "defaults", "reviewer", "profile"); err != nil {
		return err
	}
	type plainDefaults Defaults
	var decoded plainDefaults
	if err := strictDecode(payload, &decoded); err != nil {
		return err
	}
	*defaults = Defaults(decoded)
	return nil
}

func (defaults Defaults) empty() bool {
	return defaults.Reviewer == "" && defaults.Profile == ""
}

// ReviewerPolicy is one reviewer's authored policy inside one scope. A nil
// Enabled leaves enablement to lower precedence; an empty Model means no
// model is authored; a nil AllowedModels slice means no restriction.
type ReviewerPolicy struct {
	Enabled       *bool    `json:"enabled,omitempty"`
	Model         string   `json:"model,omitempty"`
	AllowedModels []string `json:"allowed_models,omitempty"`
}

func (policy *ReviewerPolicy) UnmarshalJSON(payload []byte) error {
	fields, err := decodeObjectFields(payload, "reviewer policy", "enabled", "model", "allowed_models")
	if err != nil {
		return err
	}
	type plainPolicy ReviewerPolicy
	var decoded plainPolicy
	if err := strictDecode(payload, &decoded); err != nil {
		return err
	}
	if _, present := fields["model"]; present && decoded.Model == "" {
		return errors.New("model must not be empty")
	}
	for _, model := range decoded.AllowedModels {
		if model == "" {
			return errors.New("allowed_models must not contain an empty model")
		}
	}
	*policy = ReviewerPolicy(decoded)
	return nil
}

// RetryPolicy mirrors the evaluation retry defaults authored in configuration.
type RetryPolicy struct {
	MaxAttempts    int    `json:"max_attempts,omitempty"`
	InitialBackoff string `json:"initial_backoff,omitempty"`
	MaxBackoff     string `json:"max_backoff,omitempty"`
}

// EvalPolicy holds advanced evaluation defaults. Evaluation settings are a
// Personal Configuration concern; Repository scope does not accept them.
type EvalPolicy struct {
	RetryPolicy      RetryPolicy `json:"retry_policy,omitempty"`
	ConcurrencyLimit int         `json:"concurrency_limit,omitempty"`
}

func (policy *EvalPolicy) UnmarshalJSON(payload []byte) error {
	if _, err := decodeObjectFields(payload, "eval policy", "retry_policy", "concurrency_limit"); err != nil {
		return err
	}
	type plainEvalPolicy EvalPolicy
	var decoded plainEvalPolicy
	if err := strictDecode(payload, &decoded); err != nil {
		return err
	}
	*policy = EvalPolicy(decoded)
	return nil
}

// Document is the unified configuration document shared by Personal and
// Repository scopes. Scope validation decides which fields each scope permits.
type Document struct {
	SchemaVersion  int                       `json:"schema_version"`
	StateDirectory string                    `json:"state_directory,omitempty"`
	Defaults       Defaults                  `json:"defaults,omitempty"`
	Reviewers      map[string]ReviewerPolicy `json:"reviewers,omitempty"`
	Eval           *EvalPolicy               `json:"eval,omitempty"`
}

func (document *Document) UnmarshalJSON(payload []byte) error {
	if _, err := decodeObjectFields(payload, "configuration", "schema_version", "state_directory", "defaults", "reviewers", "eval"); err != nil {
		return err
	}
	type plainDocument Document
	var decoded plainDocument
	if err := strictDecode(payload, &decoded); err != nil {
		return err
	}
	*document = Document(decoded)
	return nil
}

// InvalidDocumentError reports a malformed or semantically invalid
// configuration document with the file that caused it.
type InvalidDocumentError struct {
	Scope  Scope
	Path   string
	Reason string
}

func (failure InvalidDocumentError) Error() string {
	return fmt.Sprintf("invalid %s configuration %q: %s", failure.Scope, failure.Path, failure.Reason)
}

func invalid(scope Scope, path string, err error) InvalidDocumentError {
	return InvalidDocumentError{Scope: scope, Path: path, Reason: err.Error()}
}

// decodeObjectFields rejects null and non-object payloads, then rejects JSON
// null in each named field so authored emptiness stays explicit.
func decodeObjectFields(payload []byte, objectName string, nonNullFields ...string) (map[string]json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return nil, fmt.Errorf("%s must be an object, not null", objectName)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, fmt.Errorf("%s must be an object: %w", objectName, err)
	}
	for _, name := range nonNullFields {
		if value, exists := fields[name]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("%s must not be null", name)
		}
	}
	return fields, nil
}

func strictDecode(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	err := decoder.Decode(&extra)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err == nil:
		return errors.New("multiple JSON values")
	default:
		return err
	}
}

// validateDocument enforces one schema across both scopes while restricting
// which fields each scope may author.
func validateDocument(document Document, scope Scope, manager *Manager) error {
	if document.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d; expected %d", document.SchemaVersion, SchemaVersion)
	}
	if err := validateScopeFields(document, scope); err != nil {
		return err
	}
	if err := validateDefaults(document.Defaults, manager); err != nil {
		return err
	}
	if err := validateReviewers(document.Reviewers, manager); err != nil {
		return err
	}
	return validateDocumentEval(document.Eval)
}

func validateScopeFields(document Document, scope Scope) error {
	if scope == ScopeRepository && document.StateDirectory != "" {
		return errors.New("state_directory is a Personal Configuration field")
	}
	if scope == ScopeRepository && document.Eval != nil {
		return errors.New("eval is a Personal Configuration field")
	}
	if document.StateDirectory != "" && !filepath.IsAbs(document.StateDirectory) {
		return errors.New("state_directory must be an absolute path")
	}
	return nil
}

func validateDefaults(defaults Defaults, manager *Manager) error {
	if err := validateDefault("reviewer", defaults.Reviewer, manager.validateReviewer); err != nil {
		return err
	}
	return validateDefault("profile", defaults.Profile, manager.validateProfileName)
}

func validateDefault(field, value string, validate func(string) error) error {
	if value == "" {
		return nil
	}
	err := validate(value)
	if err == nil {
		return nil
	}
	return fmt.Errorf("defaults.%s: %w", field, err)
}

func validateReviewers(policies map[string]ReviewerPolicy, manager *Manager) error {
	for _, id := range sortedReviewerIDs(policies) {
		if err := manager.validateReviewer(id); err != nil {
			return fmt.Errorf("reviewers.%s: %w", id, err)
		}
	}
	return nil
}

func validateDocumentEval(policy *EvalPolicy) error {
	if policy == nil {
		return nil
	}
	return validateEvalPolicy(*policy)
}

func validateEvalPolicy(policy EvalPolicy) error {
	if policy.ConcurrencyLimit < 0 {
		return errors.New("eval concurrency_limit must not be negative")
	}
	if policy.RetryPolicy.MaxAttempts < 0 {
		return errors.New("eval retry_policy max_attempts must not be negative")
	}
	return nil
}

func containsModel(models []string, model string) bool {
	for _, candidate := range models {
		if candidate == model {
			return true
		}
	}
	return false
}

// formattedDocument mirrors Document with explicit optional objects so the
// rendered JSON has semantic field order and omits redundant defaults.
type formattedDocument struct {
	SchemaVersion  int                       `json:"schema_version"`
	StateDirectory string                    `json:"state_directory,omitempty"`
	Defaults       *Defaults                 `json:"defaults,omitempty"`
	Reviewers      map[string]ReviewerPolicy `json:"reviewers,omitempty"`
	Eval           *formattedEval            `json:"eval,omitempty"`
}

// formattedEval renders evaluation defaults without a redundant empty
// retry_policy object.
type formattedEval struct {
	RetryPolicy      *RetryPolicy `json:"retry_policy,omitempty"`
	ConcurrencyLimit int          `json:"concurrency_limit,omitempty"`
}

// renderDocument produces stable, readable JSON with two-space indentation,
// a trailing newline, semantic field ordering, expanded nested objects, and
// omitted redundant defaults.
func renderDocument(document Document) ([]byte, error) {
	var eval *formattedEval
	if document.Eval != nil {
		eval = &formattedEval{ConcurrencyLimit: document.Eval.ConcurrencyLimit}
		if document.Eval.RetryPolicy != (RetryPolicy{}) {
			retry := document.Eval.RetryPolicy
			eval.RetryPolicy = &retry
		}
	}
	formatted := formattedDocument{
		SchemaVersion:  document.SchemaVersion,
		StateDirectory: document.StateDirectory,
		Reviewers:      document.Reviewers,
		Eval:           eval,
	}
	if !document.Defaults.empty() {
		defaults := document.Defaults
		formatted.Defaults = &defaults
	}
	payload, err := json.MarshalIndent(formatted, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode configuration: %w", err)
	}
	return append(payload, '\n'), nil
}

func sortedReviewerIDs(policies map[string]ReviewerPolicy) []string {
	ids := make([]string, 0, len(policies))
	for id := range policies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
