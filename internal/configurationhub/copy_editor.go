package configurationhub

import (
	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

func (e *editor) copyProfile() error {
	if err := e.form(huh.NewInput().Title("Repository Profile to copy to Global Configuration").Value(&e.drafts.copyName)); err != nil {
		return err
	}
	plan, err := e.manager.PlanProfileCopy(e.Repository, configuration.ScopeRepository, configuration.ScopeGlobal, e.drafts.copyName)
	if err != nil {
		return err
	}
	published, err := e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	if published {
		e.drafts.copyName = ""
	}
	return err
}
