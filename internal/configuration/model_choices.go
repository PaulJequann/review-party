package configuration

import "fmt"

// ModelChoiceCheckStatus describes the result of an immediate model-choice
// check. It is advisory and never changes the saved Profile.
type ModelChoiceCheckStatus string

const (
	ModelChoicesUnchecked   ModelChoiceCheckStatus = "unchecked"
	ModelChoicesKnown       ModelChoiceCheckStatus = "known"
	ModelChoicesUnknown     ModelChoiceCheckStatus = "unknown"
	ModelChoicesUnavailable ModelChoiceCheckStatus = "unavailable"
)

// ModelChoiceCheck records the result of an immediate model-choice check.
type ModelChoiceCheck struct {
	Status ModelChoiceCheckStatus
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
	if profile.Model == "" {
		return ""
	}
	switch check.Status {
	case "", ModelChoicesUnchecked, ModelChoicesKnown:
		return ""
	case ModelChoicesUnavailable:
		return "model choice could not be checked against configured choices"
	}
	return fmt.Sprintf("model %q was not reported by cached, configured, or packaged choices for Reviewer %q; confirm it explicitly", profile.Model, profile.Reviewer)
}
