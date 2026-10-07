package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reviewparty/internal/configuration"
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
	deadline           time.Duration
	reviewerWasDefault bool
	buildPrompt        func(model.ReviewSubject) string
}

type profileCompileRequest struct {
	profile      configuration.Profile
	effective    configuration.Effective
	selection    model.ProfileSelection
	deadline     time.Duration
	attemptLimit int
}

type profileDefinition struct {
	name                 string
	description          string
	purpose              string
	materialityThreshold string
	pass                 model.ReviewPassRevision
	requiredCapabilities []model.Capability
}

type UnknownProfileError struct {
	Name      string
	Scope     configuration.Scope
	Available []string
}

func (failure UnknownProfileError) Error() string {
	subject := fmt.Sprintf("unknown review profile %q", failure.Name)
	if failure.Scope != "" {
		subject += fmt.Sprintf(" in %s Configuration", failure.Scope)
	}
	if len(failure.Available) == 0 {
		return subject + "; no Profiles are configured"
	}
	return subject + "; expected " + strings.Join(failure.Available, ", ")
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
		deadline:           deadline,
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

var packagedTemplateRevisions = map[string]string{
	"bugs":          "bugs-v4",
	"code-quality":  "code-quality-v1",
	"documentation": "documentation-v1",
	"test-audit":    "test-audit-v1",
}

func packagedTemplateIDs() []string {
	names := make([]string, 0, len(packagedTemplateRevisions))
	for name := range packagedTemplateRevisions {
		names = append(names, name)
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
