package engine

import (
	"errors"
	"fmt"
	"path/filepath"

	"reviewparty/internal/configuration"
)

const (
	// packagedDefaultProfileName is the effective default Profile when nothing is authored.
	packagedDefaultProfileName = "bugs"
	// defaultPartyName is the effective default Party when nothing is authored.
	defaultPartyName = "standard"
)

// newConfigurationManager constructs the Configuration Manager with Review
// Party's packaged reviewer knowledge so documents validate against known
// reviewers. An explicit path overrides the canonical Personal Configuration
// file location.
func newConfigurationManager(personalConfigPath string) *configuration.Manager {
	options := reviewPartyConfigurationOptions()
	if personalConfigPath != "" {
		options.PersonalRoot = filepath.Dir(personalConfigPath)
		options.PersonalConfigPath = personalConfigPath
	}
	return configuration.NewManager(options)
}

func reviewPartyConfigurationOptions() configuration.Options {
	return configuration.Options{
		Reviewers:               supportedReviewerIDs(),
		PackagedReviewerModels:  packagedReviewerModels(),
		PackagedDefaultReviewer: defaultReviewer,
		PackagedDefaultProfile:  packagedDefaultProfileName,
		PackagedDefaultParty:    defaultPartyName,
		ValidateName:            validateAuthoredName,
	}
}

func supportedReviewerIDs() []string {
	return defaultReviewerCatalog().ids()
}

func packagedReviewerModels() map[string]string {
	models := map[string]string{}
	for id, registration := range defaultReviewerCatalog().registrations {
		if registration.candidate.Model != "" {
			models[id] = registration.candidate.Model
		}
	}
	return models
}

func selectProfileFromEffective(effective configuration.Effective, explicitReviewer string) (profileSelection, error) {
	name := effective.DefaultProfile.Value
	if err := validateAuthoredName(name); err != nil {
		return profileSelection{}, fmt.Errorf("profile name %q: %w", name, err)
	}
	return profileSelection{name: name, reviewer: firstNonempty(explicitReviewer, effective.DefaultReviewer.Value)}, nil
}

// profileSelection carries an explicit or defaulted Profile and Reviewer choice.
type profileSelection struct {
	name     string
	reviewer string
}

func validateAuthoredName(name string) error {
	if len(name) == 0 || len(name) > 64 {
		return errors.New("must contain 1 to 64 characters")
	}
	for index, character := range name {
		if !validAuthoredNameCharacter(character, index) {
			return errors.New("must match [a-z0-9][a-z0-9-]*")
		}
	}
	return nil
}

func validAuthoredNameCharacter(character rune, index int) bool {
	if character >= 'a' && character <= 'z' {
		return true
	}
	if character >= '0' && character <= '9' {
		return true
	}
	return character == '-' && index > 0
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
