package configurationhub

import (
	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

func (e *editor) copyProfile() error {
	drafts := e.draftSet()
	if err := e.form(huh.NewSelect[string]().Title("Repository Profile to copy to Global Configuration").Options(e.repositoryProfileOptions()...).Value(&drafts.copyName)); err != nil {
		return err
	}
	plan, err := e.manager.PlanProfileCopy(e.Repository, configuration.ScopeRepository, configuration.ScopeGlobal, drafts.copyName)
	if err != nil {
		return err
	}
	published, err := e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	if published {
		drafts.copyName = ""
	}
	return err
}
