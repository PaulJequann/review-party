package configuration

import "fmt"

// ModelChoiceCheck records the immediate choices available when a Profile
// draft was reviewed. It is advisory and never changes the saved Profile.
type ModelChoiceCheck struct {
	Checked bool
	Choices []string
}

// ProfileModelChoiceSources returns the configured and packaged model choices
// for a Reviewer. Missing policy is represented by empty configured choices.
type ProfileModelChoiceSources struct {
	Configured []string
	Packaged   []string
}

// ProfileModelChoices returns the configured and packaged model choices for a
// Reviewer. Missing policy is not an error because packaged choices may exist.
func (manager *Manager) ProfileModelChoices(repository Repository, reviewer string) (ProfileModelChoiceSources, error) {
	effective, err := manager.Resolve(Request{Repository: repository})
	if err != nil {
		return ProfileModelChoiceSources{}, err
	}
	sources := ProfileModelChoiceSources{}
	settings, found := effective.ReviewerPolicy(reviewer)
	if found {
		sources.Configured = append([]string{settings.Model.Value}, settings.AllowedModels.Value...)
	}
	if packaged := manager.PackagedReviewerModel(reviewer); packaged != "" {
		sources.Packaged = []string{packaged}
	}
	return sources, nil
}

func (check ModelChoiceCheck) warning(profile Profile) string {
	if !check.Checked || profile.Model == "" {
		return ""
	}
	for _, choice := range check.Choices {
		if choice == profile.Model {
			return ""
		}
	}
	return fmt.Sprintf("model %q was not reported by cached, configured, or packaged choices for Reviewer %q; confirm it explicitly", profile.Model, profile.Reviewer)
}
