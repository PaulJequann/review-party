package configuration

import "fmt"

// ModelChoiceCheck records the immediate choices available when a Profile
// draft was reviewed. It is advisory and never changes the saved Profile.
type ModelChoiceCheck struct {
	Checked bool
	Choices []string
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
