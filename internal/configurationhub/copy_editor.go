package configurationhub

import (
	"errors"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

func (e *editor) copyProfile() error {
	options := e.repositoryProfileOptions()
	if len(options) == 0 {
		return errors.New("no Repository Profiles exist to copy")
	}
	drafts := e.draftSet()
	if err := e.form(huh.NewSelect[string]().Title("Repository Profile to copy to Global Configuration").Options(options...).Value(&drafts.copyName)); err != nil {
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
