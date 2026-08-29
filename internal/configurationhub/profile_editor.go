package configurationhub

import (
	"fmt"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

func (e *editor) createProfile() error {
	if err := e.ensureProfileFlow(); err != nil {
		return err
	}
	for {
		finished, err := e.runProfileStep()
		if err != nil {
			return err
		}
		if finished {
			return nil
		}
	}
}

func (e *editor) ensureProfileFlow() error {
	if e.drafts.profile != nil {
		return nil
	}
	var scope string
	if err := e.form(huh.NewSelect[string]().Title("Configuration scope").Options(scopeOptions()...).Value(&scope)); err != nil {
		return err
	}
	e.drafts.profile = configuration.NewProfileOnboarding(e.manager, configuration.Scope(scope))
	return nil
}

func (e *editor) runProfileStep() (bool, error) {
	flow := e.drafts.profile
	step := flow.Step()
	switch step {
	case configuration.OnboardingChooseSource:
		return false, e.chooseProfileSource(flow)
	case configuration.OnboardingInstructions:
		return false, e.reviseProfileInstructions(flow)
	case configuration.OnboardingName, configuration.OnboardingReviewer, configuration.OnboardingModel,
		configuration.OnboardingEffort, configuration.OnboardingDeadline:
		return false, e.editProfileField(flow, profileFieldSpecs[step])
	case configuration.OnboardingValidation:
		return true, e.validateAndReviewProfile(flow)
	case configuration.OnboardingReview:
		return true, e.reviewProfile(flow)
	case configuration.OnboardingComplete:
		e.drafts.profile = nil
		return true, nil
	case configuration.OnboardingCancelled:
		return false, flow.Resume()
	default:
		return true, fmt.Errorf("unknown Profile onboarding step %q", step)
	}
}

func (e *editor) validateAndReviewProfile(flow *configuration.ProfileOnboarding) error {
	if e.ModelChoiceCheck != nil {
		draft := flow.Draft()
		if err := flow.SetModelChoiceCheck(e.ModelChoiceCheck(draft.Reviewer, draft.Model)); err != nil {
			return err
		}
	}
	plan, err := flow.Validate(e.Repository)
	if err != nil {
		return e.reviseProfileAfterError(flow, err)
	}
	if !plan.Valid() {
		return e.reviseProfileAfterError(flow, fmt.Errorf("invalid configuration plan: %s", plan.Reason()))
	}
	return e.reviewProfile(flow)
}

func (e *editor) reviseProfileAfterError(flow *configuration.ProfileOnboarding, cause error) error {
	if err := flow.Revise(); err != nil {
		return err
	}
	return cause
}

type profileFieldSpec struct {
	field configuration.OnboardingField
	title string
	value func(configuration.ProfileDraft) string
}

var profileFieldSpecs = map[configuration.OnboardingStep]profileFieldSpec{
	configuration.OnboardingName:     {field: configuration.OnboardingFieldName, title: "Profile name", value: func(draft configuration.ProfileDraft) string { return draft.Name }},
	configuration.OnboardingReviewer: {field: configuration.OnboardingFieldReviewer, title: "Reviewer", value: func(draft configuration.ProfileDraft) string { return draft.Reviewer }},
	configuration.OnboardingModel:    {field: configuration.OnboardingFieldModel, title: "Model", value: func(draft configuration.ProfileDraft) string { return draft.Model }},
	configuration.OnboardingEffort:   {field: configuration.OnboardingFieldEffort, title: "Reasoning effort", value: func(draft configuration.ProfileDraft) string { return draft.ReasoningEffort }},
	configuration.OnboardingDeadline: {field: configuration.OnboardingFieldDeadline, title: "Attempt deadline (for example 8m)", value: func(draft configuration.ProfileDraft) string { return draft.AttemptDeadline }},
}

func profileTemplateOptions(templates []configuration.Template) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(templates))
	for _, template := range templates {
		options = append(options, huh.NewOption(template.ID+" ("+template.Revision+")", template.ID))
	}
	return options
}

func (e *editor) chooseProfileSource(flow *configuration.ProfileOnboarding) error {
	var source string
	if err := e.form(huh.NewSelect[string]().Title("Instruction source").Options(huh.NewOption("Template", "template"), huh.NewOption("Blank", "blank")).Value(&source)); err != nil {
		return err
	}
	instructions, err := e.chooseProfileInstructions(flow, source)
	if err != nil {
		return err
	}
	return e.maybeEditProfileInstructions(flow, instructions)
}

func (e *editor) chooseProfileInstructions(flow *configuration.ProfileOnboarding, source string) (string, error) {
	switch source {
	case "template":
		return e.chooseProfileTemplate(flow)
	case "blank":
		return e.chooseBlankProfileInstructions(flow)
	default:
		return "", fmt.Errorf("unknown instruction source %q", source)
	}
}

func (e *editor) chooseProfileTemplate(flow *configuration.ProfileOnboarding) (string, error) {
	templates := profileTemplateOptions(e.manager.Templates())
	if len(templates) == 0 {
		return "", fmt.Errorf("no packaged Review Profile Templates are available; choose blank instructions")
	}
	var templateID string
	if err := e.form(huh.NewSelect[string]().Title("Template").Options(templates...).Value(&templateID)); err != nil {
		return "", err
	}
	if err := flow.ChooseTemplate(templateID); err != nil {
		return "", err
	}
	template, found := e.manager.Template(templateID)
	if !found {
		return "", fmt.Errorf("unknown Review Profile Template %q", templateID)
	}
	return template.Instructions, nil
}

func (e *editor) chooseBlankProfileInstructions(flow *configuration.ProfileOnboarding) (string, error) {
	var instructions string
	if err := e.form(huh.NewText().Title("Instructions").Value(&instructions)); err != nil {
		return instructions, retainProfileSourceOnFormError(flow, instructions, err)
	}
	if err := flow.ChooseBlank(configuration.OnboardingText(instructions)); err != nil {
		return "", err
	}
	return instructions, nil
}

func (e *editor) maybeEditProfileInstructions(flow *configuration.ProfileOnboarding, instructions string) error {
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
	return flow.SetInstructions(configuration.OnboardingText(edited))
}

func (e *editor) reviseProfileInstructions(flow *configuration.ProfileOnboarding) error {
	if err := e.maybeEditProfileInstructions(flow, flow.Draft().Instructions); err != nil {
		return err
	}
	if flow.Step() != configuration.OnboardingInstructions {
		return nil
	}
	return flow.ContinueRevision()
}

func (e *editor) editProfileField(flow *configuration.ProfileOnboarding, spec profileFieldSpec) error {
	value := spec.value(flow.Draft())
	err := e.form(huh.NewInput().Title(spec.title).Value(&value))
	if err != nil {
		if setErr := flow.Set(spec.field, configuration.OnboardingText(value)); setErr != nil {
			return setErr
		}
		return err
	}
	return flow.Set(spec.field, configuration.OnboardingText(value))
}

func (e *editor) reviewProfile(flow *configuration.ProfileOnboarding) error {
	published, err := e.reviewAndPublish(flow.Plan(), flow.Confirm)
	if published {
		e.drafts.profile = nil
	} else if err == nil {
		if reviseErr := flow.Revise(); reviseErr != nil {
			return reviseErr
		}
	}
	return err
}

func retainProfileSourceOnFormError(flow *configuration.ProfileOnboarding, instructions string, formErr error) error {
	if err := flow.ChooseBlank(configuration.OnboardingText(instructions)); err != nil {
		return err
	}
	return formErr
}
