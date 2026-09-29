package configurationhub

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

// manageProfiles routes the Profiles area to creation or execution editing.
func (e *editor) manageProfiles() error {
	var operation string
	if err := e.form(huh.NewSelect[string]().Title("Profiles").Options(
		huh.NewOption("Create a new Profile", "create"),
		huh.NewOption("Edit an existing Profile", "edit"),
	).Value(&operation)); err != nil {
		return err
	}
	switch operation {
	case "create":
		return e.createProfile()
	case "edit":
		return e.editProfile()
	default:
		return fmt.Errorf("unknown Profiles operation %q", operation)
	}
}

// editProfile revises one existing Profile's execution settings with the same
// harness-aware model discovery used by creation.
func (e *editor) editProfile() error {
	scope, name, err := e.chooseProfileToEdit()
	if err != nil {
		return err
	}
	profile, found, err := e.manager.LoadProfile(scope, e.Repository, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("Profile %q does not exist in %s Configuration", name, scope)
	}
	draft := configuration.ProfileDraft{
		Target: scope, Name: name,
		Reviewer: profile.Reviewer, Model: profile.Model,
		ReasoningEffort: profile.ReasoningEffort, AttemptDeadline: profile.AttemptDeadline,
		TemplateID: profile.TemplateID, TemplateRevision: profile.TemplateRevision,
		Instructions: profile.Instructions,
	}
	if err := e.reviseProfileExecution(&draft); err != nil {
		return err
	}
	return e.publishProfileEdit(scope, name, draft)
}

func (e *editor) chooseProfileToEdit() (configuration.Scope, string, error) {
	options := e.editableProfileOptions()
	if len(options) == 0 {
		return "", "", fmt.Errorf("no Profiles exist to edit; create one first")
	}
	var selected string
	if err := e.form(huh.NewSelect[string]().Title("Profile to edit").Options(options...).Value(&selected)); err != nil {
		return "", "", err
	}
	scope, name := splitEditableProfileRef(selected)
	return configuration.Scope(scope), name, nil
}

func (e *editor) editableProfileOptions() []huh.Option[string] {
	var options []huh.Option[string]
	for _, item := range e.snapshot.Items {
		if item.Kind != itemProfile {
			continue
		}
		value := string(item.Scope) + ":" + item.Name
		options = append(options, huh.NewOption("["+string(item.Scope)+"] "+item.Name+" — "+item.Detail, value))
	}
	return options
}

func splitEditableProfileRef(value string) (string, string) {
	scope, name, _ := strings.Cut(value, ":")
	return scope, name
}

// reviseProfileExecution offers execution fields until the author is done,
// reusing discovery-aware model and effort prompts.
func (e *editor) reviseProfileExecution(draft *configuration.ProfileDraft) error {
	for {
		if _, err := fmt.Fprintf(e.Output, "Current: reviewer=%s model=%s effort=%s deadline=%s\n",
			draft.Reviewer, draft.Model, draft.ReasoningEffort, draft.AttemptDeadline); err != nil {
			return err
		}
		var field string
		if err := e.form(huh.NewSelect[string]().Title("Revise Profile value").Options(
			huh.NewOption("Reviewer", "reviewer"),
			huh.NewOption("Model", "model"),
			huh.NewOption("Reasoning effort", "effort"),
			huh.NewOption("Attempt deadline", "deadline"),
			huh.NewOption("Done", "done"),
		).Value(&field)); err != nil {
			return err
		}
		if field == "done" {
			return nil
		}
		if err := e.reviseProfileExecutionField(draft, field); err != nil {
			return err
		}
	}
}

func (e *editor) reviseProfileExecutionField(draft *configuration.ProfileDraft, field string) error {
	switch field {
	case "reviewer":
		return e.reviseEditReviewer(draft)
	case "model":
		return e.reviseEditModel(draft)
	case "effort":
		return e.reviseEditEffort(draft)
	case "deadline":
		spec, _ := profileFieldSpecFor("deadline")
		return e.editProfileField(draft, spec)
	default:
		return fmt.Errorf("unknown Profile revision field %q", field)
	}
}

func (e *editor) reviseEditReviewer(draft *configuration.ProfileDraft) error {
	if e.Discovery == nil {
		spec, _ := profileFieldSpecFor("reviewer")
		return e.editProfileField(draft, spec)
	}
	options := make([]huh.Option[string], 0)
	for _, reviewer := range e.Discovery.Reviewers() {
		options = append(options, huh.NewOption(reviewer, reviewer))
	}
	if len(options) == 0 {
		spec, _ := profileFieldSpecFor("reviewer")
		return e.editProfileField(draft, spec)
	}
	selected := draft.Reviewer
	if err := e.form(huh.NewSelect[string]().Title("Reviewer").Options(options...).Value(&selected)); err != nil {
		return err
	}
	spec, _ := profileFieldSpecFor("reviewer")
	spec.set(draft, selected)
	return e.refreshEditModelAfterReviewer(draft)
}

// refreshEditModelAfterReviewer re-runs model discovery when the reviewer
// changed, mirroring creation's dependent-field reset.
func (e *editor) refreshEditModelAfterReviewer(draft *configuration.ProfileDraft) error {
	if e.Discovery == nil || draft.Model != "" {
		return nil
	}
	return e.reviseEditModel(draft)
}

func (e *editor) reviseEditModel(draft *configuration.ProfileDraft) error {
	return e.reviseEditChoice(draft, "model")
}

func (e *editor) reviseEditEffort(draft *configuration.ProfileDraft) error {
	return e.reviseEditChoice(draft, "effort")
}

func (e *editor) reviseEditChoice(draft *configuration.ProfileDraft, field string) error {
	spec, _ := profileFieldSpecFor(field)
	if e.Discovery == nil {
		return e.editProfileField(draft, spec)
	}
	choices, closeSession, err := e.accessibleProfileChoices(draft.Reviewer)
	if err != nil {
		return err
	}
	defer closeSession()
	if field == "model" {
		_, err := e.editAccessibleModel(draft, choices)
		return err
	}
	return e.editAccessibleEffort(draft, choices)
}

func (e *editor) publishProfileEdit(scope configuration.Scope, name string, draft configuration.ProfileDraft) error {
	plan, err := e.planProfileEdit(scope, name, draft)
	if err != nil {
		return err
	}
	if !plan.Valid() {
		if _, writeErr := fmt.Fprintf(e.Output, "Profile planning failed: %s\n", plan.Reason()); writeErr != nil {
			return writeErr
		}
		if err := e.reviseProfileExecution(&draft); err != nil {
			return err
		}
		return e.publishProfileEdit(scope, name, draft)
	}
	published, err := e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	if published {
		e.draftSet().profile = configuration.ProfileDraft{}
		return nil
	}
	if err != nil {
		return err
	}
	if err := e.reviseProfileExecution(&draft); err != nil {
		return err
	}
	return e.publishProfileEdit(scope, name, draft)
}

func (e *editor) planProfileEdit(scope configuration.Scope, name string, draft configuration.ProfileDraft) (configuration.Plan, error) {
	update := configuration.ProfileExecutionUpdate{
		Reviewer: draft.Reviewer, Model: draft.Model,
		ReasoningEffort: draft.ReasoningEffort, AttemptDeadline: draft.AttemptDeadline,
	}
	plan, err := e.manager.PlanProfileUpdate(e.Repository, scope, name, update)
	if err != nil {
		return configuration.Plan{}, err
	}
	if !plan.Valid() {
		return plan, nil
	}
	if e.ModelChoiceCheck != nil {
		check := e.ModelChoiceCheck(draft.Reviewer, draft.Model)
		plan = plan.WithWarnings(check.Warning(draft.Reviewer, draft.Model))
	}
	return plan, nil
}
