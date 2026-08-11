package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type compiledProfile struct {
	revision           ProfileRevision
	snapshot           ProfileSnapshot
	reviewer           reviewerRegistration
	reviewerWasDefault bool
	buildPrompt        func(ReviewSubject) string
}

type profileDefinition struct {
	name                 string
	description          string
	purpose              string
	materialityThreshold string
	defaultReviewer      string
	pass                 ReviewPassRevision
	requiredCapabilities []Capability
}

type UnknownProfileError struct {
	Name      string
	Available []string
}

func (failure UnknownProfileError) Error() string {
	return fmt.Sprintf("unknown review profile %q; expected %s", failure.Name, strings.Join(failure.Available, ", "))
}

type UnsupportedCapabilitiesError struct {
	Profile  string
	Reviewer string
	Missing  []Capability
}

func (failure UnsupportedCapabilitiesError) Error() string {
	missing := make([]string, len(failure.Missing))
	for index, capability := range failure.Missing {
		missing[index] = string(capability)
	}
	return fmt.Sprintf("review profile %q requires capabilities unavailable from reviewer %q: %s", failure.Profile, failure.Reviewer, strings.Join(missing, ", "))
}

func compileProfileDefinition(catalog reviewerCatalog, selection ProfileSelection, deadline time.Duration, definition profileDefinition) (compiledProfile, error) {
	registration, reviewerWasDefault, err := resolveProfileReviewer(catalog, definition, selection)
	if err != nil {
		return compiledProfile{}, err
	}
	missing := missingCapabilities(definition.requiredCapabilities, registration.capabilities)
	if len(missing) > 0 {
		return compiledProfile{}, UnsupportedCapabilitiesError{Profile: selection.Profile, Reviewer: registration.candidate.ID, Missing: missing}
	}
	registration, err = resolveReviewerSelection(registration, selection)
	if err != nil {
		return compiledProfile{}, err
	}
	candidate := registration.candidate
	provenance := candidate.provenance()
	revision := ProfileRevision{
		Name:                 definition.name,
		Description:          definition.description,
		Purpose:              definition.purpose,
		MaterialityThreshold: definition.materialityThreshold,
		ReviewerID:           candidate.ID,
		Model:                candidate.Model,
		Effort:               candidate.Effort,
		Reviewer:             provenance,
		Passes:               []ReviewPassRevision{definition.pass},
		RequiredCapabilities: canonicalCapabilities(definition.requiredCapabilities),
		AttemptLimit:         1,
		ExecutionDeadline:    deadline.String(),
		ResultContract:       canonicalReviewResultContract.Revision(),
	}
	revision.Revision = profileRevisionIdentity(revision)

	return compiledProfile{
		revision:           revision,
		reviewer:           registration,
		reviewerWasDefault: reviewerWasDefault,
	}, nil
}

func resolveReviewerSelection(registration reviewerRegistration, selection ProfileSelection) (reviewerRegistration, error) {
	registration, err := resolveReviewerModel(registration, selection.Model)
	if err != nil {
		return reviewerRegistration{}, err
	}
	if selection.Effort != "" {
		registration.candidate.Effort = selection.Effort
	}
	if registration.validateCandidate != nil {
		if err := registration.validateCandidate(registration.candidate); err != nil {
			return reviewerRegistration{}, err
		}
	}
	return registration, nil
}

func resolveProfileReviewer(catalog reviewerCatalog, definition profileDefinition, selection ProfileSelection) (reviewerRegistration, bool, error) {
	reviewerWasDefault := selection.Reviewer == ""
	reviewer := selection.Reviewer
	if reviewer == "" {
		reviewer = catalog.defaultReviewer
	}
	if reviewer == "" {
		reviewer = definition.defaultReviewer
	}
	registration, err := catalog.resolve(reviewer)
	if err != nil {
		return reviewerRegistration{}, false, err
	}
	return registration, reviewerWasDefault, nil
}

func resolveReviewerModel(registration reviewerRegistration, model string) (reviewerRegistration, error) {
	if model != "" {
		if registration.modelAllowlistConfigured && !containsModel(registration.allowedModels, model) {
			return reviewerRegistration{}, ReviewerModelNotAllowedError{Reviewer: registration.candidate.ID, Model: model, Allowed: registration.allowedModels}
		}
		registration.candidate.Model = model
	}
	if registration.candidate.Model == "" {
		return reviewerRegistration{}, ReviewerModelRequiredError{Reviewer: registration.candidate.ID}
	}
	return registration, nil
}

func profileRevisionIdentity(revision ProfileRevision) string {
	revision.Revision = ""
	payload, err := json.Marshal(revision)
	if err != nil {
		panic(fmt.Sprintf("encode Profile Revision identity: %v", err))
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func builtInProfileDefinitions() []profileDefinition {
	capabilities := restrictedReviewCapabilities()
	return []profileDefinition{
		{
			name:                 "bugs",
			description:          "Material bug review",
			purpose:              "Find material defects in the Review Subject.",
			materialityThreshold: "A concrete actionable regression in behavior or an applicable Project Rule.",
			defaultReviewer:      defaultReviewer,
			pass:                 ReviewPassRevision{Name: "bug-review", Required: true, Purpose: "Evaluate material correctness and delivery-risk defects.", PromptRevision: "bugs-v4"},
			requiredCapabilities: capabilities,
		},
		{
			name:                 "documentation",
			description:          "Documentation accuracy review",
			purpose:              "Evaluate documentation accuracy, omissions, consistency, and project language.",
			materialityThreshold: "Documentation that would materially mislead a Caller or maintainer.",
			defaultReviewer:      defaultReviewer,
			pass:                 ReviewPassRevision{Name: "documentation-review", Required: true, Purpose: "Evaluate material documentation defects.", PromptRevision: "documentation-v1"},
			requiredCapabilities: capabilities,
		},
	}
}

func findProfileDefinition(name string) (profileDefinition, error) {
	for _, definition := range builtInProfileDefinitions() {
		if definition.name == name {
			definition.requiredCapabilities = append([]Capability(nil), definition.requiredCapabilities...)
			return definition, nil
		}
	}
	return profileDefinition{}, UnknownProfileError{Name: name, Available: SupportedProfiles()}
}

func SupportedProfiles() []string {
	definitions := builtInProfileDefinitions()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.name)
	}
	sort.Strings(names)
	return names
}

func missingCapabilities(required, available []Capability) []Capability {
	provided := make(map[Capability]struct{}, len(available))
	for _, capability := range available {
		provided[capability] = struct{}{}
	}
	missing := make([]Capability, 0)
	for _, capability := range required {
		if _, exists := provided[capability]; !exists {
			missing = append(missing, capability)
		}
	}
	return canonicalCapabilities(missing)
}

func canonicalCapabilities(capabilities []Capability) []Capability {
	canonical := append([]Capability(nil), capabilities...)
	sort.Slice(canonical, func(left, right int) bool { return canonical[left] < canonical[right] })
	return canonical
}

func (profile compiledProfile) prompt(subject ReviewSubject) string {
	return profile.buildPrompt(subject)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return "(none)"
	}
	result := lines[0]
	for _, line := range lines[1:] {
		result += "\n" + line
	}
	return result
}
