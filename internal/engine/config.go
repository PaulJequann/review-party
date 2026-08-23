package engine

import (
	"errors"
	"fmt"
	"path/filepath"

	"reviewparty/internal/configuration"
)

// packagedDefaultProfileName is the effective default Profile when nothing is authored.
const packagedDefaultProfileName = "bugs"

// newConfigurationManager constructs the Configuration Manager with Review
// Party's packaged reviewer knowledge so documents validate against known
// reviewers. An explicit path overrides the canonical Personal Configuration
// file location.
func newConfigurationManager(personalConfigPath string) *configuration.Manager {
	options := configuration.Options{
		Reviewers:               supportedReviewerIDs(),
		PackagedDefaultReviewer: defaultReviewer,
		PackagedDefaultProfile:  packagedDefaultProfileName,
		ValidateProfileName:     validateProfileName,
	}
	if personalConfigPath != "" {
		options.PersonalRoot = filepath.Dir(personalConfigPath)
		options.PersonalConfigPath = personalConfigPath
	}
	return configuration.NewManager(options)
}

func supportedReviewerIDs() []string {
	return defaultReviewerCatalog().ids()
}

// personalConfigurationRoot derives the personal root from an optional
// explicit configuration file path (empty means the canonical root).
func personalConfigurationRoot(personalConfigPath string) string {
	if personalConfigPath == "" {
		return ""
	}
	return filepath.Dir(personalConfigPath)
}

func selectProfileFromScopes(catalog reviewerCatalog, request profileSelection, personal, repository configuration.Document) (profileSelection, error) {
	name := firstNonempty(request.name, repository.Defaults.Profile, personal.Defaults.Profile, packagedDefaultProfileName)
	if err := validateProfileName(name); err != nil {
		return profileSelection{}, fmt.Errorf("profile name %q: %w", name, err)
	}
	reviewer := firstNonempty(request.reviewer, repository.Defaults.Reviewer, personal.Defaults.Reviewer, catalog.defaultReviewer, defaultReviewer)
	if _, err := catalog.resolve(reviewer); err != nil {
		return profileSelection{}, err
	}
	return profileSelection{name: name, reviewer: reviewer}, nil
}

// profileSelection carries an explicit or defaulted Profile and Reviewer choice.
type profileSelection struct {
	name     string
	reviewer string
}

func validateProfileName(name string) error {
	if len(name) == 0 || len(name) > 64 {
		return errors.New("must contain 1 to 64 characters")
	}
	for index, character := range name {
		if !validProfileNameCharacter(character, index) {
			return errors.New("must match [a-z0-9][a-z0-9-]*")
		}
	}
	return nil
}

func validProfileNameCharacter(character rune, index int) bool {
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
