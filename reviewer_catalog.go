package reviewparty

import (
	"fmt"
	"sort"
	"strings"
)

const defaultReviewer = "grok"

type reviewerRegistration struct {
	candidate reviewerCandidate
	executor  attemptExecutor
}

type reviewerCatalog struct {
	registrations map[string]reviewerRegistration
}

func defaultReviewerCatalog() reviewerCatalog {
	return newReviewerCatalog([]reviewerRegistration{
		{candidate: reviewerCandidate{ID: "grok", Model: "grok-4.5", Effort: "high", Harness: "grok-build-cli", Transport: "direct-cli"}, executor: newDirectExecutor(grokAdapter{})},
		{candidate: reviewerCandidate{ID: "opencode", Model: "zai-coding-plan/glm-5.2", Effort: "default", Harness: "opencode-cli", Transport: "direct-cli"}, executor: newDirectExecutor(openCodeAdapter{})},
		{candidate: reviewerCandidate{ID: "copilot", Model: "auto", Effort: "auto", Harness: "github-copilot-cli", Transport: "direct-cli"}, executor: newDirectExecutor(copilotAdapter{})},
	})
}

func newReviewerCatalog(registrations []reviewerRegistration) reviewerCatalog {
	catalog := reviewerCatalog{registrations: make(map[string]reviewerRegistration, len(registrations))}
	for _, registration := range registrations {
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
		return reviewerRegistration{}, fmt.Errorf("unknown reviewer %q; expected %s", id, strings.Join(catalog.ids(), ", "))
	}
	return registration, nil
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
