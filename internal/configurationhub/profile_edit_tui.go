package configurationhub

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

// openProfileEditSelectForm offers every scoped Profile for execution editing.
func (model *Model) openProfileEditSelectForm() tea.Cmd {
	options := model.profileOptions("", true)
	if len(options) == 0 {
		model.status = "No Profiles exist to edit."
		return model.openProfileFieldsForm()
	}
	return model.openForm(formProfileEdit, []huh.Field{
		huh.NewSelect[string]().Title("Profile to edit").Options(options...).Value(&model.session.editProfile),
	})
}

// startProfileEdit loads one scoped Profile and enters the execution flow.
func (model *Model) startProfileEdit(selection string) (tea.Model, tea.Cmd) {
	scope, name := configuration.ParseScopedReference(selection)
	if scope == "" || name == "" {
		parts := strings.SplitN(selection, ":", 2)
		if len(parts) == 2 {
			scope, name = configuration.Scope(parts[0]), parts[1]
		}
	}
	if scope == "" || name == "" {
		model.status = fmt.Sprintf("unknown Profile selection %q", selection)
		return model.withForm(model.openProfileEditSelectForm())
	}
	model.formKind = formProfileEditLoading
	model.view = viewForm
	model.form = formAdapter{}
	if model.runtime == nil || model.runtime.commands.loadProfile == nil {
		model.status = "No profile loading command is available."
		return model.withForm(model.openProfileEditSelectForm())
	}
	return model.withForm(model.runtime.commands.loadProfile(string(scope), name))
}

// receiveProfileLoaded prefills the execution draft and reuses discovery.
func (model *Model) receiveProfileLoaded(message profileLoadedMsg) (tea.Model, tea.Cmd) {
	if model.formKind != formProfileEditLoading {
		return *model, nil
	}
	if message.err != nil {
		model.status = fmt.Sprintf("Loading Profile failed: %v", message.err)
		return model.withForm(model.openProfileEditSelectForm())
	}
	if !message.found {
		model.status = fmt.Sprintf("Profile %q was not found in %s Configuration", message.name, message.scope)
		return model.withForm(model.openProfileEditSelectForm())
	}
	profile := message.profile
	model.session.profile = profileFormState{
		draft: configuration.ProfileDraft{
			Target: configuration.Scope(message.scope), Name: message.name,
			Reviewer: profile.Reviewer, Model: profile.Model,
			ReasoningEffort: profile.ReasoningEffort, AttemptDeadline: profile.AttemptDeadline,
			TemplateID: profile.TemplateID, TemplateRevision: profile.TemplateRevision,
			Instructions: profile.Instructions,
		},
		target:  message.scope,
		editing: true,
	}
	model.syncFormDraft()
	return model.withForm(model.openProfileEditFieldsForm())
}

// openProfileEditFieldsForm locks scope and name, leaving execution editable.
func (model *Model) openProfileEditFieldsForm() tea.Cmd {
	state := &model.session.profile
	state.target = string(state.draft.Target)
	state.accessors = make(map[string]*profileFieldAccessor, 1)
	fields := []huh.Field{
		huh.NewNote().Title("Editing Profile").Description("[" + state.target + "] " + state.draft.Name + ". Model and effort choices follow with harness detection."),
	}
	reviewerSpec, _ := profileFieldSpecFor("reviewer")
	reviewer := &profileFieldAccessor{draft: &state.draft, spec: reviewerSpec, state: state}
	state.accessors[reviewerSpec.name] = reviewer
	if model.runtime == nil || model.runtime.choices.service == nil {
		reviewer.input = huh.NewInput().Title(reviewerSpec.title).Accessor(reviewer)
		fields = append(fields, reviewer.input)
		return model.openForm(formProfileFields, fields)
	}
	options := make([]huh.Option[string], 0)
	for _, id := range model.runtime.choices.service.Reviewers() {
		options = append(options, huh.NewOption(id+" (availability checked after selection)", id))
	}
	fields = append(fields, huh.NewSelect[string]().Title(reviewerSpec.title).Options(options...).Accessor(reviewer))
	return model.openForm(formProfileFields, fields)
}

// completeProfileEditSelectForm routes the chosen Profile into loading.
func (model *Model) completeProfileEditSelectForm() (tea.Model, tea.Cmd) {
	selection := model.session.editProfile
	model.syncFormDraft()
	return model.startProfileEdit(selection)
}

// browserEdit starts execution editing for the highlighted Profile row.
func (model Model) browserEdit() (tea.Model, tea.Cmd) {
	if model.browserArea != AreaProfiles {
		return model, nil
	}
	items := model.visibleItems(itemProfile)
	var profiles []Item
	for _, item := range items {
		if item.Kind == itemProfile {
			profiles = append(profiles, item)
		}
	}
	if len(profiles) == 0 || model.cursor >= len(profiles) {
		model.status = "No Profile row is selected."
		return model, nil
	}
	item := profiles[min(model.cursor, len(profiles)-1)]
	model.ensureSession()
	model.session = newFormSession(model.drafts)
	model.view = viewForm
	model.status = ""
	selection := string(item.Scope) + ":" + item.Name
	model.session.editProfile = selection
	return model.startProfileEdit(selection)
}

// isProfileEdit reports whether the active profile flow updates an existing Profile.
func (model *Model) isProfileEdit() bool {
	return model.session != nil && model.session.profile.editing
}
