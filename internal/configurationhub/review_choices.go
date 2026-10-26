package configurationhub

import (
	"fmt"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

const emptyReviewChoices = "No valid Profiles or Parties to add. Create a Profile in Profiles, then return to Repository Reviews."
const addReviewDescription = "Adds to this repository's selection and retains existing entries. The scope identifies the saved definition. Review the full selection and concurrency before publishing."

func reviewChoiceOptions(choices []configuration.SelectedDefinition) []huh.Option[configuration.SelectedDefinition] {
	options := make([]huh.Option[configuration.SelectedDefinition], 0, len(choices))
	for _, choice := range choices {
		label := fmt.Sprintf("[%s] %s %s", choice.Scope, choice.Kind, choice.Name)
		options = append(options, huh.NewOption(label, choice))
	}
	return options
}
