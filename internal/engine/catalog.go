package engine

import (
	"fmt"
	"sort"
	"strings"

	"reviewparty/internal/configuration"
)

const defaultReviewer = "grok"

type reviewerRegistration struct {
	candidate                reviewerCandidate
	capabilities             []Capability
	validateCandidate        func(reviewerCandidate) error
	enabled                  configuration.Value[bool]
	allowedModels            []string
	modelAllowlistConfigured bool
	executor                 attemptExecutor
}

type reviewerCatalog struct {
	registrations   map[string]reviewerRegistration
	defaultReviewer string
}

func defaultReviewerCatalog() reviewerCatalog {
	capabilities := restrictedReviewCapabilities()
	return newReviewerCatalog([]reviewerRegistration{
		{candidate: reviewerCandidate{ID: "grok", Model: "grok-4.5", Effort: "high", Harness: "grok-build-cli", Transport: "direct-cli"}, capabilities: capabilities, executor: newDirectExecutor(grokAdapter{})},
		{candidate: reviewerCandidate{ID: "opencode", Effort: "default", Harness: "opencode-cli", Transport: "direct-cli"}, capabilities: capabilities, validateCandidate: validateOpenCodeCandidate, executor: newDirectExecutor(openCodeAdapter{})},
		{candidate: reviewerCandidate{ID: "copilot", Model: "auto", Effort: "auto", Harness: "github-copilot-cli", Transport: "direct-cli"}, capabilities: capabilities, validateCandidate: validateCopilotCandidate, executor: newDirectExecutor(copilotAdapter{})},
		{candidate: reviewerCandidate{ID: "codex", Model: "gpt-5.6-luna", Effort: "high", Harness: "codex-cli", Transport: "direct-cli"}, capabilities: capabilities, validateCandidate: validateCodexCandidate, executor: newDirectExecutor(codexAdapter{})},
	})
}

func newReviewerCatalog(registrations []reviewerRegistration) reviewerCatalog {
	catalog := reviewerCatalog{registrations: make(map[string]reviewerRegistration, len(registrations))}
	for _, registration := range registrations {
		registration.capabilities = append([]Capability(nil), registration.capabilities...)
		registration.allowedModels = append([]string(nil), registration.allowedModels...)
		catalog.registrations[registration.candidate.ID] = registration
	}
	return catalog
}

func catalogWithExecutors(executors map[string]attemptExecutor) reviewerCatalog {
	defaults := defaultReviewerCatalog()
	registrations := make([]reviewerRegistration, 0, len(executors))
	for id, executor := range executors {
		registration, exists := defaults.registrations[id]
		if !exists {
			registration.candidate = reviewerCandidate{ID: id}
		}
		registration.executor = executor
		registrations = append(registrations, registration)
	}
	return newReviewerCatalog(registrations)
}

func (catalog reviewerCatalog) resolve(id string) (reviewerRegistration, error) {
	registration, exists := catalog.registrations[id]
	if !exists || registration.executor == nil {
		return reviewerRegistration{}, UnknownReviewerError{Name: id, Available: catalog.ids()}
	}
	if registration.isDisabled() {
		return reviewerRegistration{}, DisabledReviewerError{Name: id, Source: string(registration.enabled.Source), Path: registration.enabled.Path}
	}
	registration.capabilities = append([]Capability(nil), registration.capabilities...)
	registration.allowedModels = append([]string(nil), registration.allowedModels...)
	return registration, nil
}

func (registration reviewerRegistration) isDisabled() bool {
	return registration.enabled.Authored && !registration.enabled.Value
}

func (catalog reviewerCatalog) ids() []string {
	ids := make([]string, 0, len(catalog.registrations))
	for id := range catalog.registrations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func SupportedReviewers() []string {
	return defaultReviewerCatalog().ids()
}

type UnknownReviewerError struct {
	Name      string
	Available []string
}

type DisabledReviewerError struct {
	Name   string
	Source string
	Path   string
}

type ReviewerEffortNotSupportedError struct {
	Reviewer string
	Model    string
	Effort   string
}

func (failure ReviewerEffortNotSupportedError) Error() string {
	return fmt.Sprintf("reviewer %q does not support explicit effort %q with model %q", failure.Reviewer, failure.Effort, failure.Model)
}

func (failure DisabledReviewerError) Error() string {
	return fmt.Sprintf("reviewer %q is disabled by %s configuration %q", failure.Name, failure.Source, failure.Path)
}

func (failure UnknownReviewerError) Error() string {
	return fmt.Sprintf("unknown reviewer %q; expected %s", failure.Name, strings.Join(failure.Available, ", "))
}

func restrictedReviewCapabilities() []Capability {
	return []Capability{
		CapabilityRepositoryRead,
		CapabilityRepositorySearch,
		CapabilityRepositoryMutationDenied,
		CapabilityShellDenied,
		CapabilityWebDenied,
	}
}
