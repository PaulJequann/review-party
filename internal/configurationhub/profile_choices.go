package configurationhub

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
	"reviewparty/internal/discovery"
)

func (model Model) updateProfileChoiceMessage(message tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch message := message.(type) {
	case profileChoicesOpenedMsg:
		updated, command := model.receiveProfileChoicesOpened(message)
		return updated, command, true
	case profileChoicesRefreshedMsg:
		updated, command := model.receiveProfileChoicesRefreshed(message)
		return updated, command, true
	default:
		return model, nil, false
	}
}

func (model *Model) openProfileModelForm() tea.Cmd {
	state := &model.session.profile
	state.selected = initialProfileModel(state.draft.Model, state.choices)
	// huh hashes bindings with hashstructure, which ignores unexported
	// fields, so bind the fields themselves rather than the state struct.
	// huh loads options only once the hash differs from zero, and an empty
	// choices slice alone hashes to zero, so bind a list that never does.
	return model.openForm(formProfileModel, []huh.Field{
		huh.NewNote().Title("Model availability").DescriptionFunc(func() string { return profileChoiceDiagnostic(state) }, &state.diagnostic),
		huh.NewSelect[string]().Title("Model").Description("Press / to filter.").
			OptionsFunc(func() []huh.Option[string] { return profileModelOptions(state.choices, state.draft.Model) }, []any{&state.choices, &state.draft.Model}).
			Value(&state.selected).Filtering(true).WithHeight(7),
	})
}

func (model *Model) openProfileFieldsForm() tea.Cmd {
	state := &model.session.profile
	fields := []huh.Field{
		huh.NewSelect[string]().Title("Configuration scope").Options(scopeOptions()...).Value(&state.target),
		huh.NewNote().Title("Profile details").Description("Choose a Reviewer. Model and effort choices follow."),
	}
	state.accessors = make(map[string]*profileFieldAccessor, len(profileFieldSpecs))
	fields = append(fields, model.profileIdentityFields(state)...)
	for _, name := range []string{"model", "effort", "deadline"} {
		spec, _ := profileFieldSpecFor(name)
		accessor := &profileFieldAccessor{draft: &state.draft, spec: spec, state: state}
		accessor.input = huh.NewInput().Title(spec.title).Accessor(accessor)
		state.accessors[name] = accessor
	}
	return model.openForm(formProfileFields, fields)
}

func (model *Model) profileIdentityFields(state *profileFormState) []huh.Field {
	nameSpec, _ := profileFieldSpecFor("name")
	name := &profileFieldAccessor{draft: &state.draft, spec: nameSpec, state: state}
	name.input = huh.NewInput().Title(nameSpec.title).Accessor(name)
	state.accessors[nameSpec.name] = name
	reviewerSpec, _ := profileFieldSpecFor("reviewer")
	reviewer := &profileFieldAccessor{draft: &state.draft, spec: reviewerSpec, state: state}
	state.accessors[reviewerSpec.name] = reviewer
	if model.runtime == nil || model.runtime.choices.service == nil {
		reviewer.input = huh.NewInput().Title(reviewerSpec.title).Accessor(reviewer)
		return []huh.Field{name.input, reviewer.input}
	}
	options := make([]huh.Option[string], 0)
	for _, id := range model.runtime.choices.service.Reviewers() {
		options = append(options, huh.NewOption(id+" (availability checked after selection)", id))
	}
	return []huh.Field{name.input, huh.NewSelect[string]().Title(reviewerSpec.title).Options(options...).Accessor(reviewer)}
}

// profileModelOptions lists refresh first, then every choice, marking the
// Profile's current model. A current model absent from the choices is still
// listed so keeping it never requires manual entry.
func profileModelOptions(choices []discovery.ModelChoice, current string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(choices)+3)
	options = append(options, huh.NewOption("↻ Refresh available models", refreshProfileChoice))
	listed := false
	for _, choice := range choices {
		label := choice.Model.ID
		if choice.Model.ID == current {
			label += " [current]"
			listed = true
		}
		options = append(options, huh.NewOption(label, choice.Model.ID))
	}
	if !listed && current != "" {
		options = append(options, huh.NewOption(current+" [current]", current))
	}
	return append(options, huh.NewOption("Enter a model ID manually", manualProfileChoice))
}

// initialProfileModel picks the option the Model select starts on: the
// Profile's current model, else the first discovered model, else Refresh.
// The result is always one of profileModelOptions(choices, current).
func initialProfileModel(current string, choices []discovery.ModelChoice) string {
	if current != "" {
		return current
	}
	if len(choices) > 0 {
		return choices[0].Model.ID
	}
	return refreshProfileChoice
}

func profileChoiceDiagnostic(state *profileFormState) string {
	if state.diagnostic != "" {
		return state.diagnostic
	}
	return "Checking Reviewer availability and authentication. Manual entry remains available."
}

func (model *Model) openProfileManualModelForm() tea.Cmd {
	state := &model.session.profile
	state.manualModel = state.draft.Model
	return model.openForm(formProfileModelManual, []huh.Field{
		huh.NewInput().Title("Model ID").Description("Enter the exact identifier. Unknown models require confirmation before publish.").Value(&state.manualModel),
	})
}

func (model *Model) completeProfileModelManualForm() (tea.Model, tea.Cmd) {
	state := &model.session.profile
	// Route through the model spec setter so a changed model clears the
	// dependent effort and deadline, mirroring choice selection. The setter
	// is a no-op when the value is unchanged.
	spec, _ := profileFieldSpecFor("model")
	spec.set(&state.draft, state.manualModel)
	return model.withForm(model.openProfileEffortForm())
}

func (model *Model) openProfileEffortForm() tea.Cmd {
	state := &model.session.profile
	state.effort = state.draft.ReasoningEffort
	efforts := profileReasoningEfforts(state.choices, state.draft.Model)
	if len(efforts) == 0 {
		return model.openProfileManualEffortForm()
	}
	return model.openForm(formProfileEffort, []huh.Field{
		huh.NewSelect[string]().Title("Reasoning effort").Description("Values reported for the selected model.").
			Options(profileEffortOptions(efforts)...).Value(&state.effort).WithHeight(6),
	})
}

func profileEffortOptions(efforts []string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(efforts)+1)
	for _, effort := range efforts {
		options = append(options, huh.NewOption(effort, effort))
	}
	return append(options, huh.NewOption("Enter an effort manually", manualProfileChoice))
}

func profileReasoningEfforts(choices []discovery.ModelChoice, model string) []string {
	for _, choice := range choices {
		if choice.Model.ID == model {
			return append([]string(nil), choice.Model.ReasoningEfforts...)
		}
	}
	return nil
}

func (model *Model) openProfileManualEffortForm() tea.Cmd {
	return model.openForm(formProfileEffortManual, []huh.Field{
		huh.NewNote().Title("Reasoning effort").Description("This model did not report effort choices. Enter an exact value manually."),
		huh.NewInput().Title("Reasoning effort").Value(&model.session.profile.draft.ReasoningEffort),
	})
}

func (model *Model) openProfileDeadlineForm() tea.Cmd {
	return model.openForm(formProfileDeadline, []huh.Field{
		huh.NewInput().Title("Attempt deadline (for example 8m)").Value(&model.session.profile.draft.AttemptDeadline),
	})
}

func (model *Model) completeProfileFieldsForm() (tea.Model, tea.Cmd) {
	state := &model.session.profile
	state.draft.Target = configuration.Scope(state.target)
	model.syncFormDraft()
	model.formKind = formProfileChoicesLoading
	model.form = formAdapter{}
	if model.runtime == nil {
		return model.withForm(model.openProfileManualModelForm())
	}
	return *model, model.runtime.openProfileChoices(state.draft.Reviewer)
}

func (model *Model) completeProfileModelForm() (tea.Model, tea.Cmd) {
	state := &model.session.profile
	if state.selected == refreshProfileChoice {
		return model.completeProfileFieldsForm()
	}
	if state.selected == manualProfileChoice {
		return model.withForm(model.openProfileManualModelForm())
	}
	if state.draft.Model != state.selected {
		state.draft.Model = state.selected
		state.draft.ReasoningEffort = ""
		state.draft.AttemptDeadline = ""
	}
	return model.withForm(model.openProfileEffortForm())
}

func (model *Model) completeProfileEffortForm() (tea.Model, tea.Cmd) {
	if model.session.profile.effort == manualProfileChoice {
		return model.withForm(model.openProfileManualEffortForm())
	}
	model.session.profile.draft.ReasoningEffort = model.session.profile.effort
	return model.withForm(model.openProfileDeadlineForm())
}

func (model *Model) completeProfileExecutionForm() (tea.Model, tea.Cmd) {
	model.syncFormDraft()
	if model.isProfileEdit() {
		return model.startPlan(planRequest{kind: planProfileEdit, profile: model.drafts.profile})
	}
	if profileNeedsSource(model.drafts.profile) {
		model.session.source = ""
		return model.withForm(model.openProfileSourceForm())
	}
	return model.startPlan(planRequest{kind: planProfile, profile: model.drafts.profile})
}

func (model *Model) receiveProfileChoicesOpened(message profileChoicesOpenedMsg) (tea.Model, tea.Cmd) {
	state := &model.session.profile
	if !model.acceptsProfileChoices(message) {
		return *model, nil
	}
	state.generation = message.generation
	state.choices = message.choices
	state.diagnostic = "Checking Reviewer availability and authentication. Manual entry remains available."
	return model.withForm(tea.Batch(model.openProfileModelForm(), message.refresh))
}

func (model *Model) acceptsProfileChoices(message profileChoicesOpenedMsg) bool {
	return model.formKind == formProfileChoicesLoading &&
		model.session.profile.draft.Reviewer == message.reviewer &&
		model.runtime.currentProfileChoiceGeneration(message.generation)
}

func (model *Model) receiveProfileChoicesRefreshed(message profileChoicesRefreshedMsg) (tea.Model, tea.Cmd) {
	state := &model.session.profile
	if state.generation != message.generation || state.draft.Reviewer != message.reviewer {
		return *model, nil
	}
	state.choices = discovery.MergeChoices(state.choices, message.result.Models, discovery.ChoiceSourceDiscovered)
	state.diagnostic = profileDiscoveryDiagnostic(message.result)
	if model.formKind == formProfileModel {
		// Deliver the refresh to the open form so its bound options and
		// diagnostic re-evaluate without waiting for the next keypress.
		return model.updateForm(message)
	}
	return *model, nil
}

func profileDiscoveryDiagnostic(result discovery.Result) string {
	parts := []string{"Reviewer " + result.Reviewer + ": " + string(result.Status), "authentication: " + string(result.Authentication.Status)}
	if result.Diagnostic != "" {
		parts = append(parts, result.Diagnostic)
	} else if result.Authentication.Diagnostic != "" {
		parts = append(parts, result.Authentication.Diagnostic)
	}
	return strings.Join(parts, "; ")
}

func (e *editor) editProfileFieldsWithChoices(draft *configuration.ProfileDraft) error {
	if profileExecutionComplete(*draft) {
		return nil
	}
	if err := e.editAccessibleProfileIdentity(draft); err != nil {
		return err
	}
	choices, closeSession, err := e.accessibleProfileChoices(draft.Reviewer)
	if err != nil {
		return err
	}
	defer closeSession()
	choices, err = e.editAccessibleModel(draft, choices)
	if err != nil {
		return err
	}
	if err := e.editAccessibleEffort(draft, choices); err != nil {
		return err
	}
	deadline, _ := profileFieldSpecFor("deadline")
	if strings.TrimSpace(deadline.value(*draft)) == "" {
		return e.editProfileField(draft, deadline)
	}
	return nil
}

func profileExecutionComplete(draft configuration.ProfileDraft) bool {
	values := []string{draft.Name, draft.Reviewer, draft.Model, draft.ReasoningEffort, draft.AttemptDeadline}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func (e *editor) editAccessibleProfileIdentity(draft *configuration.ProfileDraft) error {
	name, _ := profileFieldSpecFor("name")
	if strings.TrimSpace(draft.Name) == "" {
		if err := e.editProfileField(draft, name); err != nil {
			return err
		}
	}
	if strings.TrimSpace(draft.Reviewer) != "" {
		return nil
	}
	options := make([]huh.Option[string], 0)
	for _, reviewer := range e.Discovery.Reviewers() {
		options = append(options, huh.NewOption(reviewer, reviewer))
	}
	value := options[0].Value
	if err := e.form(huh.NewSelect[string]().Title("Reviewer").Options(options...).Value(&value)); err != nil {
		return err
	}
	reviewer, _ := profileFieldSpecFor("reviewer")
	reviewer.set(draft, value)
	return nil
}

func (e *editor) openAccessibleChoices(reviewer string) discovery.ChoiceSession {
	sources, err := e.manager.ProfileModelChoices(e.Repository, reviewer)
	if err != nil {
		sources = configuration.ProfileModelChoiceSources{}
	}
	return e.Discovery.Open(e.Context, discovery.ChoiceRequest{Reviewer: reviewer, Configured: sources.Configured, Packaged: sources.Packaged})
}

func (e *editor) accessibleProfileChoices(reviewer string) ([]discovery.ModelChoice, func(), error) {
	session := e.openAccessibleChoices(reviewer)
	choices := session.Choices
	if len(choices) == 0 {
		// No immediate choices: wait for the bounded refresh instead of
		// offering manual entry alone while discovery is still running.
		// The session always delivers one result, so this terminates.
		if result, ok := <-session.Refresh; ok {
			choices = discovery.MergeChoices(choices, result.Models, discovery.ChoiceSourceDiscovered)
			_, writeErr := fmt.Fprintln(e.Output, profileDiscoveryDiagnostic(result))
			return choices, session.Close, writeErr
		}
	}
	message := "Reviewer availability is still being checked; manual entry remains available."
	select {
	case result := <-session.Refresh:
		choices = discovery.MergeChoices(choices, result.Models, discovery.ChoiceSourceDiscovered)
		message = profileDiscoveryDiagnostic(result)
	default:
	}
	_, writeErr := fmt.Fprintln(e.Output, message)
	return choices, session.Close, writeErr
}

// refreshAccessibleChoices waits for one live observation, which the
// discovery deadline bounds, and merges it into fresh immediate choices.
func (e *editor) refreshAccessibleChoices(reviewer string) ([]discovery.ModelChoice, error) {
	session := e.openAccessibleChoices(reviewer)
	defer session.Close()
	choices := session.Choices
	result, ok := <-session.Refresh
	if !ok {
		return choices, nil
	}
	_, err := fmt.Fprintln(e.Output, profileDiscoveryDiagnostic(result))
	return discovery.MergeChoices(choices, result.Models, discovery.ChoiceSourceDiscovered), err
}

// editAccessibleModel returns the choices in effect after any manual
// refresh so effort prompts read the same observation.
func (e *editor) editAccessibleModel(draft *configuration.ProfileDraft, choices []discovery.ModelChoice) ([]discovery.ModelChoice, error) {
	selected := initialProfileModel(draft.Model, choices)
	if err := e.form(huh.NewSelect[string]().Title("Model").Options(profileModelOptions(choices, draft.Model)...).Value(&selected)); err != nil {
		return choices, err
	}
	model, _ := profileFieldSpecFor("model")
	switch selected {
	case refreshProfileChoice:
		refreshed, err := e.refreshAccessibleChoices(draft.Reviewer)
		if err != nil {
			return refreshed, err
		}
		return e.editAccessibleModel(draft, refreshed)
	case manualProfileChoice:
		entered := draft.Model
		if err := e.form(huh.NewInput().Title("Model ID").Value(&entered)); err != nil {
			return choices, err
		}
		model.set(draft, entered)
	default:
		model.set(draft, selected)
	}
	return choices, nil
}

func (e *editor) editAccessibleEffort(draft *configuration.ProfileDraft, choices []discovery.ModelChoice) error {
	efforts := profileReasoningEfforts(choices, draft.Model)
	if len(efforts) == 0 {
		if _, err := fmt.Fprintln(e.Output, "The selected model did not report reasoning efforts; enter an exact value."); err != nil {
			return err
		}
		return e.form(huh.NewInput().Title("Reasoning effort").Value(&draft.ReasoningEffort))
	}
	selected := draft.ReasoningEffort
	if selected == "" {
		selected = efforts[0]
	}
	if err := e.form(huh.NewSelect[string]().Title("Reasoning effort").Options(profileEffortOptions(efforts)...).Value(&selected)); err != nil {
		return err
	}
	if selected == manualProfileChoice {
		return e.form(huh.NewInput().Title("Reasoning effort").Value(&draft.ReasoningEffort))
	}
	draft.ReasoningEffort = selected
	return nil
}
