package engine

import (
	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func profileInventorySummary(definition configuration.Definition[configuration.Profile]) model.ProfileSummary {
	summary := model.ProfileSummary{Name: definition.Name, Source: definition.Source, Path: definition.Path}
	if definition.Err != nil {
		summary.Error = definition.Err.Error()
		return summary
	}
	summary.Source = definition.Value.Source
	return summary
}
