package configuration

import "fmt"

// ModelChoiceCheck records the immediate choices available when a Profile
// draft was reviewed. It is advisory and never changes the saved Profile.
type ModelChoiceCheck struct {
	Checked bool
	Choices []string
}

// ProfileModelChoices returns the configured and packaged model choices for a
// Reviewer. The boolean reports whether the Reviewer has an effective policy.
func (manager *Manager) ProfileModelChoices(repository Repository, reviewer string) ([]string, bool, error) {
	effective, err := manager.Resolve(Request{Repository: repository})
	if err != nil {
		return nil, false, err
	}
	settings, found := effective.ReviewerPolicy(reviewer)
	if !found {
		return nil, false, nil
	}
	choices := append([]string{settings.Model.Value}, settings.AllowedModels.Value...)
	if packaged := manager.PackagedReviewerModel(reviewer); packaged != "" {
		choices = append(choices, packaged)
	}
	return choices, true, nil
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
