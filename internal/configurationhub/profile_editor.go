package configurationhub

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

func (e *editor) createProfile() error {
	_, err := e.publishNewProfile("")
	return err
}

// publishNewProfile completes the Profile draft and returns the published
// Profile. A non-empty suggestedTemplate is preselected, not fixed, when the
// Caller chooses the instruction source.
func (e *editor) publishNewProfile(suggestedTemplate string) (configuration.ProfileReference, error) {
	if err := e.ensureProfileFlow(); err != nil {
		return configuration.ProfileReference{}, err
	}
	draft := &e.draftSet().profile
	// Field order matches the interactive forms: execution fields first, then
	// the instruction source once the rest of the draft is known.
	if err := e.editProfileFields(draft); err != nil {
		return configuration.ProfileReference{}, err
	}
	if profileNeedsSource(*draft) {
		if err := e.chooseProfileSource(draft, suggestedTemplate); err != nil {
			return configuration.ProfileReference{}, err
		}
	}
	return e.completeProfile(draft)
}

func profileNeedsSource(draft configuration.ProfileDraft) bool {
	if draft.TemplateID != "" {
		return false
	}
	return strings.TrimSpace(draft.Instructions) == ""
}

func (e *editor) completeProfile(draft *configuration.ProfileDraft) (configuration.ProfileReference, error) {
	for {
		published, err := e.attemptProfile(draft)
		if err != nil {
			return configuration.ProfileReference{}, err
		}
		if published {
			reference := configuration.ProfileReference{Scope: draft.Target, Profile: draft.Name}
			*draft = configuration.ProfileDraft{}
			return reference, nil
		}
	}
}

// attemptProfile plans and reviews the draft once. A planning failure or a
// declined review leads into one revision before the next attempt.
func (e *editor) attemptProfile(draft *configuration.ProfileDraft) (bool, error) {
	// Re-ask only the fields still empty, matching the interactive forms'
	// reopen-with-prefilled-values behavior on revision.
	if err := e.editProfileFields(draft); err != nil {
		return false, err
	}
	plan, err := e.planProfile(*draft)
	if err != nil {
		return false, e.reviseProfileAfterError(draft, err)
	}
	published, err := e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	if published || err != nil {
		return published, err
	}
	return false, e.reviseProfile(draft)
}

func (e *editor) ensureProfileFlow() error {
	drafts := e.draftSet()
	if drafts.profile != (configuration.ProfileDraft{}) {
		return nil
	}
	var scope string
	if err := e.form(huh.NewSelect[string]().Title("Configuration scope").Options(scopeOptions()...).Value(&scope)); err != nil {
		return err
	}
	drafts.profile.Target = configuration.Scope(scope)
	return nil
}

func (e *editor) editProfileFields(draft *configuration.ProfileDraft) error {
	if e.Discovery != nil {
		return e.editProfileFieldsWithChoices(draft)
	}
	for _, spec := range profileFieldSpecs {
		if strings.TrimSpace(spec.value(*draft)) != "" {
			continue
		}
		if err := e.editProfileField(draft, spec); err != nil {
			return err
		}
	}
	return nil
}

func (e *editor) planProfile(draft configuration.ProfileDraft) (configuration.Plan, error) {
	plan, err := e.manager.PlanProfileCreation(e.Repository, draft)
	if err != nil {
		return configuration.Plan{}, err
	}
	if !plan.Valid() {
		return configuration.Plan{}, fmt.Errorf("invalid configuration plan: %s", plan.Reason())
	}
	if e.ModelChoiceCheck != nil {
		check := e.ModelChoiceCheck(draft.Reviewer, draft.Model)
		plan = plan.WithWarnings(check.Warning(draft.Reviewer, draft.Model))
	}
	return plan, nil
}

func (e *editor) reviseProfileAfterError(draft *configuration.ProfileDraft, cause error) error {
	if _, err := fmt.Fprintf(e.Output, "Profile planning failed: %v\n", cause); err != nil {
		return err
	}
	return e.reviseProfile(draft)
}

type profileFieldSpec struct {
	name  string
	title string
	value func(configuration.ProfileDraft) string
	set   func(*configuration.ProfileDraft, string)
}

const profileInstructionsField = "instructions"

var profileFieldSpecs = [...]profileFieldSpec{
	{
		name:  "name",
		title: "Profile name",
		value: func(draft configuration.ProfileDraft) string { return draft.Name },
		set:   func(draft *configuration.ProfileDraft, value string) { draft.Name = value },
	},
	{
		name:  "reviewer",
		title: "Reviewer",
		value: func(draft configuration.ProfileDraft) string { return draft.Reviewer },
		set: func(draft *configuration.ProfileDraft, value string) {
			if draft.Reviewer != value {
				draft.Reviewer = value
				draft.Model = ""
				draft.ReasoningEffort = ""
				draft.AttemptDeadline = ""
			}
		},
	},
	{
		name:  "model",
		title: "Model",
		value: func(draft configuration.ProfileDraft) string { return draft.Model },
		set: func(draft *configuration.ProfileDraft, value string) {
			if draft.Model != value {
				draft.Model = value
				draft.ReasoningEffort = ""
				draft.AttemptDeadline = ""
			}
		},
	},
	{
		name:  "effort",
		title: "Reasoning effort",
		value: func(draft configuration.ProfileDraft) string { return draft.ReasoningEffort },
		set:   func(draft *configuration.ProfileDraft, value string) { draft.ReasoningEffort = value },
	},
	{
		name:  "deadline",
		title: "Attempt deadline (for example 8m)",
		value: func(draft configuration.ProfileDraft) string { return draft.AttemptDeadline },
		set:   func(draft *configuration.ProfileDraft, value string) { draft.AttemptDeadline = value },
	},
}

func profileTemplateOptions(templates []configuration.Template) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(templates))
	for _, template := range templates {
		options = append(options, huh.NewOption(template.ID+" ("+template.Revision+")", template.ID))
	}
	return options
}

func (e *editor) chooseProfileSource(draft *configuration.ProfileDraft, suggestedTemplate string) error {
	var source string
	if err := e.form(huh.NewSelect[string]().Title("Instruction source").Options(huh.NewOption("Template", "template"), huh.NewOption("Blank", "blank")).Value(&source)); err != nil {
		return err
	}
	switch source {
	case "template":
		return e.chooseProfileTemplate(draft, suggestedTemplate)
	case "blank":
		return e.chooseBlankProfileInstructions(draft)
	default:
		return fmt.Errorf("unknown instruction source %q", source)
	}
}

func (e *editor) chooseProfileTemplate(draft *configuration.ProfileDraft, suggestedTemplate string) error {
	templates := profileTemplateOptions(e.manager.Templates())
	if len(templates) == 0 {
		return fmt.Errorf("no Review Profile Templates are available; choose blank instructions")
	}
	templateID := suggestedTemplate
	if err := e.form(huh.NewSelect[string]().Title("Template").Options(templates...).Value(&templateID)); err != nil {
		return err
	}
	template, found := e.manager.Template(templateID)
	if !found {
		return fmt.Errorf("unknown Review Profile Template %q", templateID)
	}
	draft.TemplateID = template.ID
	draft.TemplateRevision = template.Revision
	draft.Instructions = ""
	return e.maybeEditProfileInstructions(draft, template.Instructions)
}

func (e *editor) chooseBlankProfileInstructions(draft *configuration.ProfileDraft) error {
	var instructions string
	if err := e.form(huh.NewText().Title("Instructions").Value(&instructions)); err != nil {
		draft.TemplateID = ""
		draft.TemplateRevision = ""
		draft.Instructions = instructions
		return err
	}
	draft.TemplateID = ""
	draft.TemplateRevision = ""
	draft.Instructions = instructions
	return e.maybeEditProfileInstructions(draft, instructions)
}

func (e *editor) maybeEditProfileInstructions(draft *configuration.ProfileDraft, instructions string) error {
	var useEditor bool
	if err := e.form(huh.NewConfirm().Title("Edit instructions with $EDITOR?").Value(&useEditor)); err != nil {
		return err
	}
	if !useEditor {
		return nil
	}
	edited, err := editInstructions(e.Context, instructions, e.Input, e.Output)
	if err != nil {
		return err
	}
	draft.TemplateID = ""
	draft.TemplateRevision = ""
	draft.Instructions = edited
	return nil
}

func (e *editor) reviseProfileInstructions(draft *configuration.ProfileDraft) error {
	instructions := profileInstructions(*draft, e.manager)
	var useEditor bool
	if err := e.form(huh.NewConfirm().Title("Edit instructions with $EDITOR?").Value(&useEditor)); err != nil {
		return err
	}
	if useEditor {
		edited, err := editInstructions(e.Context, instructions, e.Input, e.Output)
		if err != nil {
			return err
		}
		instructions = edited
	} else if err := e.form(huh.NewText().Title("Instructions").Value(&instructions)); err != nil {
		draft.TemplateID = ""
		draft.TemplateRevision = ""
		draft.Instructions = instructions
		return err
	}
	draft.TemplateID = ""
	draft.TemplateRevision = ""
	draft.Instructions = instructions
	return nil
}

func profileInstructions(draft configuration.ProfileDraft, manager *configuration.Manager) string {
	if draft.Instructions != "" {
		return draft.Instructions
	}
	if draft.TemplateID == "" || manager == nil {
		return draft.Instructions
	}
	template, found := manager.Template(draft.TemplateID)
	if !found {
		return draft.Instructions
	}
	return template.Instructions
}

func (e *editor) editProfileField(draft *configuration.ProfileDraft, spec profileFieldSpec) error {
	value := spec.value(*draft)
	if err := e.form(huh.NewInput().Title(spec.title).Value(&value)); err != nil {
		spec.set(draft, value)
		return err
	}
	spec.set(draft, value)
	return nil
}

func (e *editor) reviseProfile(draft *configuration.ProfileDraft) error {
	options := []huh.Option[string]{huh.NewOption("Instructions", profileInstructionsField)}
	for _, spec := range profileFieldSpecs {
		options = append(options, huh.NewOption(spec.title, spec.name))
	}
	var field string
	if err := e.form(huh.NewSelect[string]().Title("Revise Profile value").Options(options...).Value(&field)); err != nil {
		return err
	}
	if field == profileInstructionsField {
		return e.reviseProfileInstructions(draft)
	}
	spec, found := profileFieldSpecFor(field)
	if !found {
		return fmt.Errorf("unknown Profile revision field %q", field)
	}
	return e.editProfileField(draft, spec)
}

func profileFieldSpecFor(name string) (profileFieldSpec, bool) {
	for _, spec := range profileFieldSpecs {
		if spec.name == name {
			return spec, true
		}
	}
	return profileFieldSpec{}, false
}
