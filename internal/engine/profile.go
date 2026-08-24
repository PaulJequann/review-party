package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reviewparty/internal/model"
	"reviewparty/internal/result"
	"sort"
	"strings"
	"time"
)

type compiledProfile struct {
	revision           model.ProfileRevision
	snapshot           model.ProfileSnapshot
	reviewer           reviewerRegistration
	reviewerWasDefault bool
	buildPrompt        func(model.ReviewSubject) string
}

type profileDefinition struct {
	name                 string
	description          string
	purpose              string
	materialityThreshold string
	defaultReviewer      string
	pass                 model.ReviewPassRevision
	requiredCapabilities []model.Capability
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
	Missing  []model.Capability
}

func (failure UnsupportedCapabilitiesError) Error() string {
	missing := make([]string, len(failure.Missing))
	for index, capability := range failure.Missing {
		missing[index] = string(capability)
	}
	return fmt.Sprintf("review profile %q requires capabilities unavailable from reviewer %q: %s", failure.Profile, failure.Reviewer, strings.Join(missing, ", "))
}

func compileProfileDefinition(catalog reviewerCatalog, selection model.ProfileSelection, deadline time.Duration, definition profileDefinition) (compiledProfile, error) {
	registration, reviewerWasDefault, err := resolveProfileReviewer(catalog, definition, selection)
	if err != nil {
		return compiledProfile{}, err
	}
	registration, err = validateReviewerSelection(registration, selection, definition.requiredCapabilities, definition.name)
	if err != nil {
		return compiledProfile{}, err
	}
	candidate := registration.candidate
	provenance := candidate.provenance()
	revision := model.ProfileRevision{
		Name:                 definition.name,
		Description:          definition.description,
		Purpose:              definition.purpose,
		MaterialityThreshold: definition.materialityThreshold,
		ReviewerID:           candidate.ID,
		Model:                candidate.Model,
		Effort:               candidate.Effort,
		Reviewer:             provenance,
		Passes:               []model.ReviewPassRevision{definition.pass},
		RequiredCapabilities: canonicalCapabilities(definition.requiredCapabilities),
		AttemptLimit:         1,
		ExecutionDeadline:    deadline.String(),
		ResultContract:       result.CanonicalReviewResultContract.Revision(),
	}
	revision.Revision = profileRevisionIdentity(revision)

	return compiledProfile{
		revision:           revision,
		reviewer:           registration,
		reviewerWasDefault: reviewerWasDefault,
	}, nil
}

func validateReviewerSelection(registration reviewerRegistration, selection model.ProfileSelection, required []model.Capability, profileName string) (reviewerRegistration, error) {
	missing := missingCapabilities(required, registration.capabilities)
	if len(missing) > 0 {
		return reviewerRegistration{}, UnsupportedCapabilitiesError{Profile: profileName, Reviewer: registration.candidate.ID, Missing: missing}
	}
	return resolveReviewerSelection(registration, selection)
}

func resolveReviewerSelection(registration reviewerRegistration, selection model.ProfileSelection) (reviewerRegistration, error) {
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

func resolveProfileReviewer(catalog reviewerCatalog, definition profileDefinition, selection model.ProfileSelection) (reviewerRegistration, bool, error) {
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

func profileRevisionIdentity(revision model.ProfileRevision) string {
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
			pass:                 model.ReviewPassRevision{Name: "bug-review", Required: true, Purpose: "Evaluate material correctness and delivery-risk defects.", PromptRevision: "bugs-v4"},
			requiredCapabilities: capabilities,
		},
		{
			name:                 "code-quality",
			description:          "Maintainability and structural quality review",
			purpose:              "Find material maintainability regressions and concrete opportunities to simplify the implementation.",
			materialityThreshold: "A concrete structural regression or high-conviction simplification that materially affects maintainability, change safety, or local architecture.",
			defaultReviewer:      defaultReviewer,
			pass:                 model.ReviewPassRevision{Name: "code-quality-review", Required: true, Purpose: "Evaluate material structural and maintainability defects.", PromptRevision: "code-quality-v1"},
			requiredCapabilities: capabilities,
		},
		{
			name:                 "documentation",
			description:          "Documentation accuracy review",
			purpose:              "Evaluate documentation accuracy, omissions, consistency, and project language.",
			materialityThreshold: "Documentation that would materially mislead a Caller or maintainer.",
			defaultReviewer:      defaultReviewer,
			pass:                 model.ReviewPassRevision{Name: "documentation-review", Required: true, Purpose: "Evaluate material documentation defects.", PromptRevision: "documentation-v1"},
			requiredCapabilities: capabilities,
		},
	}
}

func findProfileDefinition(name string) (profileDefinition, error) {
	for _, definition := range builtInProfileDefinitions() {
		if definition.name == name {
			definition.requiredCapabilities = append([]model.Capability(nil), definition.requiredCapabilities...)
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

func missingCapabilities(required, available []model.Capability) []model.Capability {
	provided := make(map[model.Capability]struct{}, len(available))
	for _, capability := range available {
		provided[capability] = struct{}{}
	}
	missing := make([]model.Capability, 0)
	for _, capability := range required {
		if _, exists := provided[capability]; !exists {
			missing = append(missing, capability)
		}
	}
	return canonicalCapabilities(missing)
}

func canonicalCapabilities(capabilities []model.Capability) []model.Capability {
	canonical := append([]model.Capability(nil), capabilities...)
	sort.Slice(canonical, func(left, right int) bool { return canonical[left] < canonical[right] })
	return canonical
}

func (profile compiledProfile) prompt(subject model.ReviewSubject) string {
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
