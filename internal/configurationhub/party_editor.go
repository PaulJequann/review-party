package configurationhub

import (
	"strings"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

type partyFormDraft struct {
	scope, name, description, profiles, limit string
	profileRefs                               []string
}

func (e *editor) createParty() error {
	draft := &e.draftSet().party
	if err := e.form(e.partyFields(draft)...); err != nil {
		return err
	}
	party, err := partyDraftFromForm(*draft)
	if err != nil {
		return err
	}
	plan, err := e.manager.PlanPartyCreation(e.Repository, party)
	if err != nil {
		return err
	}
	published, err := e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	if published {
		e.draftSet().party = partyFormDraft{}
	}
	return err
}

// partyFields asks only for what the draft does not already hold. A Global
// Party may reference only Global Profiles, so a seeded Global draft offers
// no others. Members are chosen before the scope field exists because a huh
// Select writes its first option into an empty bound value.
func (e *editor) partyFields(draft *partyFormDraft) []huh.Field {
	members := e.profileReferenceOptions()
	if draft.scope == string(configuration.ScopeGlobal) {
		members = globalProfileOptions(members)
	}
	fields := make([]huh.Field, 0, 5)
	if draft.scope == "" {
		fields = append(fields, huh.NewSelect[string]().Title("Configuration scope").Options(scopeOptions()...).Value(&draft.scope))
	}
	if draft.name == "" {
		fields = append(fields, huh.NewInput().Title("Party name").Value(&draft.name))
	}
	return append(fields,
		huh.NewInput().Title("Description").Value(&draft.description),
		huh.NewMultiSelect[string]().Title("Profiles").Options(members...).Value(&draft.profileRefs),
		huh.NewInput().Title("Concurrency limit").Value(&draft.limit),
	)
}

func globalProfileOptions(options []huh.Option[string]) []huh.Option[string] {
	global := make([]huh.Option[string], 0, len(options))
	for _, option := range options {
		if strings.HasPrefix(option.Value, string(configuration.ScopeGlobal)+":") {
			global = append(global, option)
		}
	}
	return global
}
