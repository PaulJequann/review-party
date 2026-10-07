package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"reviewparty/internal/configuration"
)

// newConfigurationManager constructs the Configuration Manager with Review
// Party's packaged reviewer knowledge so documents validate against known
// reviewers. An explicit path overrides the canonical Global Configuration
// file location.
func newConfigurationManager(globalConfigPath string) *configuration.Manager {
	options := reviewPartyConfigurationOptions()
	if globalConfigPath != "" {
		options.GlobalRoot = filepath.Dir(globalConfigPath)
		options.GlobalConfigPath = globalConfigPath
	}
	return configuration.NewManager(options)
}

// ReviewPartyConfigurationOptions returns the engine-independent configuration
// knowledge used by Review Party's command and execution compositions.
func ReviewPartyConfigurationOptions() configuration.Options {
	return reviewPartyConfigurationOptions()
}

func reviewPartyConfigurationOptions() configuration.Options {
	skills := configuration.SkillTemplates(callerHomeSkillRoots())
	return configuration.Options{
		Reviewers:               supportedReviewerIDs(),
		PackagedReviewerModels:  packagedReviewerModels(),
		PackagedDefaultReviewer: defaultReviewer,
		Templates:               append(packagedReviewProfileTemplates(), skills.Templates...),
		SkippedTemplates:        skills.Skipped,
		ValidateName:            validateAuthoredName,
	}
}

func callerHomeSkillRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".claude", "skills")}
}

func packagedReviewProfileTemplates() []configuration.Template {
	templates := make([]configuration.Template, 0, len(packagedTemplateIDs()))
	for _, id := range packagedTemplateIDs() {
		instructions, err := packagedProfileFiles.ReadFile("profiles/" + id + ".md")
		if err != nil {
			panic(fmt.Sprintf("read packaged Review Profile Template %q: %v", id, err))
		}
		packaged := packagedTemplates[id]
		templates = append(templates, configuration.Template{ID: id, Revision: packaged.revision, Baseline: packaged.baseline, Instructions: string(instructions)})
	}
	return templates
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
