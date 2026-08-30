package configurationhub

import (
	"fmt"
	"strconv"
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
	if err := e.form(
		huh.NewSelect[string]().Title("Configuration scope").Options(scopeOptions()...).Value(&draft.scope),
		huh.NewInput().Title("Party name").Value(&draft.name),
		huh.NewInput().Title("Description").Value(&draft.description),
		huh.NewMultiSelect[string]().Title("Profiles").Options(e.profileReferenceOptions()...).Value(&draft.profileRefs),
		huh.NewInput().Title("Concurrency limit").Value(&draft.limit),
	); err != nil {
		return err
	}
	limit, err := strconv.Atoi(strings.TrimSpace(draft.limit))
	if err != nil {
		return fmt.Errorf("invalid concurrency limit: %w", err)
	}
	refs := make([]configuration.ProfileReference, 0)
	for _, raw := range strings.Fields(draft.profiles) {
		refScope, name := configuration.ParseScopedReference(raw)
		if refScope == "" {
			refScope = configuration.Scope(draft.scope)
		}
		refs = append(refs, configuration.ProfileReference{Scope: refScope, Profile: name})
	}
	plan, err := e.manager.PlanPartyCreation(e.Repository, configuration.PartyDraft{Target: configuration.Scope(draft.scope), Name: draft.name, Description: draft.description, ConcurrencyLimit: limit, Profiles: refs})
	if err != nil {
		return err
	}
	published, err := e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	if published {
		e.draftSet().party = partyFormDraft{}
	}
	return err
}
