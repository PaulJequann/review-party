package configurationhub

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"reviewparty/internal/configuration"
)

type formKind string

const (
	formNone                   formKind = ""
	formOverview               formKind = "overview"
	formProfileFields          formKind = "profile-fields"
	formProfileSource          formKind = "profile-source"
	formProfileTemplate        formKind = "profile-template"
	formProfileTemplateLoading formKind = "profile-template-loading"
	formProfileInstructions    formKind = "profile-instructions"
	formEditor                 formKind = "editor"
	formParty                  formKind = "party"
	formReviewOperation        formKind = "review-operation"
	formReviewLoading          formKind = "review-loading"
	formReviewFields           formKind = "review-fields"
	formCopy                   formKind = "copy"
	formChanges                formKind = "changes"
	formPlanning               formKind = "planning"
	formConfirm                formKind = "confirm"
	formPublishing             formKind = "publishing"
)

type formSession struct {
	profile              profileFormState
	party                partyFormDraft
	copyName             string
	reviews              reviewFormDraft
	reviewSelection      configuration.ReviewSelection
	source               string
	templateID           string
	templateInstructions string
	instructions         string
	useEditor            bool
	confirm              bool
	planSummary          string
}

type profileFormState struct {
	draft     configuration.ProfileDraft
	target    string
	accessors map[string]*profileFieldAccessor
}

func newFormSession(drafts draftSet) *formSession {
	return &formSession{
		profile:      profileFormState{draft: drafts.profile, target: string(drafts.profile.Target)},
		party:        drafts.party,
		copyName:     drafts.copyName,
		reviews:      drafts.reviews,
		instructions: drafts.profile.Instructions,
	}
}

type profileFieldAccessor struct {
	draft *configuration.ProfileDraft
	spec  profileFieldSpec
	state *profileFormState
	input *huh.Input
}

func (accessor *profileFieldAccessor) Get() string {
	return accessor.spec.value(*accessor.draft)
}

func (accessor *profileFieldAccessor) Set(value string) {
	previous := accessor.spec.value(*accessor.draft)
	accessor.spec.set(accessor.draft, value)
	if accessor.state == nil || previous == accessor.spec.value(*accessor.draft) {
		return
	}
	accessor.refreshDependentInputs()
}

func (accessor *profileFieldAccessor) refreshDependentInputs() {
	var names []string
	switch accessor.spec.name {
	case "reviewer":
		names = []string{"model", "effort", "deadline"}
	case "model":
		names = []string{"effort", "deadline"}
	default:
		return
	}
	for _, name := range names {
		field := accessor.state.accessors[name]
		if field != nil && field.input != nil {
			field.input.Accessor(field)
		}
	}
}

func (model *Model) ensureSession() {
	if model.session == nil {
		model.session = newFormSession(model.drafts)
	}
}

func (model *Model) openArea(area Area) tea.Cmd {
	model.ensureSession()
	model.view = viewForm
	model.status = ""
	model.session = newFormSession(model.drafts)
	switch area {
	case AreaOverview:
		return model.openOverviewForm()
	case AreaProfiles:
		return model.openProfileFieldsForm()
	case AreaParties:
		return model.openPartyForm()
	case AreaReviews:
		return model.openReviewOperationForm()
	case AreaAdvanced:
		return model.openCopyForm()
	case AreaChanges:
		return model.openChangesForm()
	case areaSearch:
		model.status = "Search is available from the menu."
		return model.openOverviewForm()
	default:
		model.status = fmt.Sprintf("unknown Hub area %q", area)
		return model.openOverviewForm()
	}
}

func (model *Model) openForm(kind formKind, fields []huh.Field) tea.Cmd {
	model.view = viewForm
	model.formKind = kind
	model.form = newFormAdapter(fields, model.formWidth(), model.formHeight(), huh.ThemeFunc(huh.ThemeCharm))
	return model.form.Init()
}

func (model *Model) withForm(command tea.Cmd) (Model, tea.Cmd) {
	return *model, command
}

func (model Model) formWidth() int {
	if model.width > 0 {
		return model.width
	}
	return 80
}

func (model Model) formHeight() int {
	if model.height > 0 {
		return model.height
	}
	return 20
}

func (model *Model) openOverviewForm() tea.Cmd {
	return model.openForm(formOverview, []huh.Field{
		huh.NewConfirm().Title("Return to the menu").Value(&model.session.confirm),
		huh.NewNote().Title("Configuration overview").Description(model.overviewDescription()),
	})
}

func (model Model) overviewDescription() string {
	lines := append([]string(nil), model.snapshot.Overview...)
	for _, warning := range model.snapshot.Warnings {
		lines = append(lines, "warning: "+warning)
	}
	return strings.Join(lines, "\n")
}

func (model *Model) openProfileFieldsForm() tea.Cmd {
	state := &model.session.profile
	fields := []huh.Field{
		huh.NewSelect[string]().Title("Configuration scope").Options(scopeOptions()...).Value(&state.target),
		huh.NewNote().Title("Profile details").Description("Create a Review Profile. Reviewer changes reset its execution fields."),
	}
	state.accessors = make(map[string]*profileFieldAccessor, len(profileFieldSpecs))
	for _, spec := range profileFieldSpecs {
		accessor := &profileFieldAccessor{draft: &state.draft, spec: spec, state: state}
		accessor.input = huh.NewInput().Title(spec.title).Accessor(accessor)
		state.accessors[spec.name] = accessor
		fields = append(fields, accessor.input)
	}
	return model.openForm(formProfileFields, fields)
}

func (model *Model) openProfileSourceForm() tea.Cmd {
	return model.openForm(formProfileSource, []huh.Field{
		huh.NewNote().Title("Instruction source").Description("Use a packaged template or start with blank instructions."),
		huh.NewSelect[string]().Title("Instruction source").Options(
			huh.NewOption("Template", "template"), huh.NewOption("Blank", "blank"),
		).Value(&model.session.source),
	})
}

func (model Model) profileTemplateOptions() []huh.Option[string] {
	options := make([]huh.Option[string], 0)
	for _, item := range model.snapshot.Items {
		if item.Kind != itemTemplate {
			continue
		}
		options = append(options, huh.NewOption(item.Name+" ("+item.Detail+")", item.Name))
	}
	return options
}

func (model *Model) openProfileTemplateForm() tea.Cmd {
	options := model.profileTemplateOptions()
	if len(options) == 0 {
		model.status = "No packaged Review Profile Templates are available. Choose blank instructions."
		return model.openProfileSourceForm()
	}
	return model.openForm(formProfileTemplate, []huh.Field{
		huh.NewSelect[string]().Title("Template").Options(options...).Value(&model.session.templateID),
	})
}

func (model *Model) openProfileInstructionsForm() tea.Cmd {
	state := &model.session.profile
	fields := []huh.Field{}
	if state.draft.TemplateID != "" {
		fields = append(fields, huh.NewConfirm().Title("Edit instructions with $EDITOR?").Value(&model.session.useEditor))
		fields = append(fields, huh.NewNote().Title("Template instructions").Description(
			fmt.Sprintf("Using %s. Leave the editor option off to keep the template content.", state.draft.TemplateID),
		))
	} else {
		fields = append(fields, huh.NewText().Title("Instructions").Value(&model.session.instructions))
		fields = append(fields, huh.NewConfirm().Title("Edit instructions with $EDITOR?").Value(&model.session.useEditor))
	}
	return model.openForm(formProfileInstructions, fields)
}

func (model *Model) openPartyForm() tea.Cmd {
	draft := &model.session.party
	return model.openForm(formParty, []huh.Field{
		huh.NewSelect[string]().Title("Configuration scope").Options(scopeOptions()...).Value(&draft.scope),
		huh.NewInput().Title("Party name").Value(&draft.name),
		huh.NewInput().Title("Description").Value(&draft.description),
		huh.NewText().Title("Profiles, one scoped reference per line").Value(&draft.profiles),
		huh.NewInput().Title("Concurrency limit").Value(&draft.limit),
	})
}

func (model *Model) openReviewOperationForm() tea.Cmd {
	return model.openForm(formReviewOperation, []huh.Field{
		huh.NewSelect[string]().Title("Repository Reviews").Options(
			huh.NewOption("Add Profile or Party", "add"),
			huh.NewOption("Remove item", "remove"),
			huh.NewOption("Move item", "move"),
			huh.NewOption("Set concurrency", "concurrency"),
		).Value(&model.session.reviews.operation),
	})
}

func (model *Model) openReviewFieldsForm() tea.Cmd {
	draft := &model.session.reviews
	var fields []huh.Field
	switch draft.operation {
	case "add":
		fields = []huh.Field{
			huh.NewSelect[string]().Title("Selection group").Options(scopeOptions()...).Value(&draft.scope),
			huh.NewSelect[string]().Title("Kind").Options(huh.NewOption("Profile", "profile"), huh.NewOption("Party", "party")).Value(&draft.kind),
			huh.NewInput().Title("Name").Value(&draft.name),
		}
	case "remove":
		fields = []huh.Field{
			huh.NewSelect[string]().Title("Selection group").Options(scopeOptions()...).Value(&draft.scope),
			huh.NewInput().Title("Zero-based index").Value(&draft.index),
		}
	case "move":
		fields = []huh.Field{
			huh.NewSelect[string]().Title("Selection group").Options(scopeOptions()...).Value(&draft.scope),
			huh.NewInput().Title("From index").Value(&draft.from),
			huh.NewInput().Title("To index").Value(&draft.to),
		}
	case "concurrency":
		fields = []huh.Field{huh.NewInput().Title("Concurrency limit").Value(&draft.concurrency)}
	default:
		model.status = fmt.Sprintf("unknown Repository Reviews operation %q", draft.operation)
		return model.openReviewOperationForm()
	}
	return model.openForm(formReviewFields, fields)
}

func (model *Model) openCopyForm() tea.Cmd {
	return model.openForm(formCopy, []huh.Field{
		huh.NewInput().Title("Repository Profile to copy to Global Configuration").Value(&model.session.copyName),
	})
}

func (model *Model) openChangesForm() tea.Cmd {
	description := strings.Join(model.drafts.descriptions(), "\n")
	if description == "" {
		description = "No unfinished drafts."
	}
	return model.openForm(formChanges, []huh.Field{
		huh.NewConfirm().Title("Discard all unfinished drafts?").Value(&model.session.confirm),
		huh.NewNote().Title("Unfinished drafts").Description(description),
	})
}

func (model *Model) syncFormDraft() {
	if model.session == nil {
		return
	}
	switch model.formKind {
	case formProfileFields, formProfileSource, formProfileTemplate, formProfileTemplateLoading, formProfileInstructions, formEditor:
		state := model.session.profile
		state.draft.Target = configuration.Scope(state.target)
		if state.draft.TemplateID == "" {
			state.draft.Instructions = model.session.instructions
		}
		model.drafts.profile = state.draft
	case formParty:
		model.drafts.party = model.session.party
	case formReviewOperation, formReviewLoading, formReviewFields:
		model.drafts.reviews = model.session.reviews
	case formCopy:
		model.drafts.copyName = model.session.copyName
	case formNone, formOverview, formChanges, formPlanning, formConfirm, formPublishing:
		// These views do not edit a draft directly.
	}
}

func (model *Model) updateForm(message tea.Msg) (tea.Model, tea.Cmd) {
	var command tea.Cmd
	model.form, command = model.form.Update(message)
	model.syncFormDraft()
	switch model.form.State() {
	case huh.StateAborted:
		model.toMenu()
		return *model, nil
	case huh.StateCompleted:
		return model.completeForm()
	case huh.StateNormal:
		return *model, command
	}
	return *model, command
}

func (model *Model) completeForm() (tea.Model, tea.Cmd) {
	switch model.formKind {
	case formOverview, formChanges:
		return model.completeExitForm()
	case formProfileFields, formProfileSource, formProfileTemplate, formProfileInstructions:
		return model.completeProfileForm()
	case formParty, formReviewFields, formCopy:
		return model.completePlanForm()
	case formReviewOperation:
		return model.completeReviewOperationForm()
	case formConfirm:
		return model.completeConfirmationForm()
	case formNone, formProfileTemplateLoading, formEditor, formReviewLoading, formPlanning, formPublishing:
		// These states do not accept a completed form.
	}
	return *model, nil
}

func (model *Model) completeProfileForm() (tea.Model, tea.Cmd) {
	switch model.formKind {
	case formProfileFields:
		return model.completeProfileFieldsForm()
	case formProfileSource:
		return model.completeProfileSourceForm()
	case formProfileTemplate:
		return model.completeProfileTemplateForm()
	case formProfileInstructions:
		return model.completeProfileInstructionsForm()
	case formNone, formOverview, formProfileTemplateLoading, formEditor, formParty,
		formReviewOperation, formReviewLoading, formReviewFields, formCopy, formChanges,
		formPlanning, formConfirm, formPublishing:
		// Only profile forms reach this dispatcher.
	}
	return *model, nil
}

func (model *Model) completePlanForm() (tea.Model, tea.Cmd) {
	switch model.formKind {
	case formParty:
		return model.startPlan(planRequest{kind: planParty, party: model.drafts.party})
	case formReviewFields:
		return model.startPlan(planRequest{kind: planReviews, review: reviewPlanRequest{
			selection: model.session.reviewSelection,
			draft:     model.drafts.reviews,
		}})
	case formCopy:
		return model.startPlan(planRequest{kind: planCopy, copy: model.drafts.copyName})
	case formNone, formOverview, formProfileFields, formProfileSource, formProfileTemplate,
		formProfileTemplateLoading, formProfileInstructions, formEditor, formReviewOperation,
		formReviewLoading, formChanges, formPlanning, formConfirm, formPublishing:
		// Only plan-producing forms reach this dispatcher.
	}
	return *model, nil
}

func (model *Model) completeExitForm() (tea.Model, tea.Cmd) {
	if model.formKind == formChanges && model.session.confirm {
		model.drafts.clear()
	}
	model.toMenu()
	return *model, nil
}

func (model *Model) completeProfileFieldsForm() (tea.Model, tea.Cmd) {
	if profileNeedsSource(model.drafts.profile) {
		model.session.source = ""
		return model.withForm(model.openProfileSourceForm())
	}
	return model.startPlan(planRequest{kind: planProfile, profile: model.drafts.profile})
}

func (model *Model) completeProfileSourceForm() (tea.Model, tea.Cmd) {
	switch model.session.source {
	case "template":
		return model.withForm(model.openProfileTemplateForm())
	case "blank":
		model.session.profile.draft.TemplateID = ""
		model.session.profile.draft.TemplateRevision = ""
		model.session.instructions = model.drafts.profile.Instructions
		model.session.useEditor = false
		return model.withForm(model.openProfileInstructionsForm())
	default:
		model.status = fmt.Sprintf("unknown instruction source %q", model.session.source)
		return model.withForm(model.openProfileSourceForm())
	}
}

func (model *Model) completeProfileTemplateForm() (tea.Model, tea.Cmd) {
	model.session.profile.draft.TemplateID = model.session.templateID
	model.session.profile.draft.TemplateRevision = ""
	model.session.profile.draft.Instructions = ""
	model.formKind = formProfileTemplateLoading
	if model.runtime == nil || model.runtime.commands.loadTemplate == nil {
		return model.withForm(model.openProfileInstructionsForm())
	}
	return model.withForm(model.runtime.commands.loadTemplate(model.session.templateID))
}

func (model *Model) completeProfileInstructionsForm() (tea.Model, tea.Cmd) {
	if model.session.useEditor {
		return model.startInstructionEditor()
	}
	if model.session.profile.draft.TemplateID == "" {
		model.session.profile.draft.Instructions = model.session.instructions
	}
	model.syncFormDraft()
	return model.startPlan(planRequest{kind: planProfile, profile: model.drafts.profile})
}

func (model *Model) completeReviewOperationForm() (tea.Model, tea.Cmd) {
	model.formKind = formReviewLoading
	if model.runtime == nil || model.runtime.commands.loadReviewSelection == nil {
		model.session.reviewSelection = configuration.DefaultReviewSelection()
		return model.withForm(model.openReviewFieldsForm())
	}
	return model.withForm(model.runtime.commands.loadReviewSelection())
}

func (model *Model) completeConfirmationForm() (tea.Model, tea.Cmd) {
	if !model.session.confirm {
		model.toMenu()
		return *model, nil
	}
	model.formKind = formPublishing
	model.form = formAdapter{}
	if model.pending == nil {
		model.status = "No publish command is available."
		return model.reopenPlan(model.pendingKind)
	}
	return model.withForm(model.pending)
}

func (model *Model) startPlan(request planRequest) (tea.Model, tea.Cmd) {
	model.pendingKind = request.kind
	model.formKind = formPlanning
	model.form = formAdapter{}
	model.view = viewForm
	if model.runtime == nil || model.runtime.commands.plan == nil {
		model.status = "No plan command is available."
		return model.reopenPlan(request.kind)
	}
	return model.withForm(model.runtime.commands.plan(request))
}

func (model *Model) startInstructionEditor() (tea.Model, tea.Cmd) {
	instructions := model.session.instructions
	if model.session.profile.draft.TemplateID != "" {
		instructions = model.session.templateInstructions
	}
	model.formKind = formEditor
	model.form = formAdapter{}
	model.view = viewForm
	if model.runtime == nil {
		model.status = "No interactive editor runtime is available."
		return model.reopenPlan(planProfile)
	}
	command, err := newInstructionEditorCommand(model.runtime.context, instructions, model.runtime.input, model.runtime.output)
	if err != nil {
		model.status = err.Error()
		return model.withForm(model.openProfileInstructionsForm())
	}
	return *model, command
}

func (model *Model) reopenPlan(kind planKind) (Model, tea.Cmd) {
	switch kind {
	case planProfile:
		return model.withForm(model.openProfileFieldsForm())
	case planParty:
		return model.withForm(model.openPartyForm())
	case planReviews:
		return model.withForm(model.openReviewFieldsForm())
	case planCopy:
		return model.withForm(model.openCopyForm())
	default:
		model.toMenu()
		return *model, nil
	}
}

func (model *Model) receivePlan(message planReadyMsg) (tea.Model, tea.Cmd) {
	model.pendingKind = message.kind
	model.pending = message.publish
	model.session.planSummary = message.summary
	model.session.confirm = false
	model.status = ""
	return model.withForm(model.openForm(formConfirm, []huh.Field{
		huh.NewNote().Title("Proposed configuration plan").Description(message.summary),
		huh.NewConfirm().Title("Publish this complete plan?").Value(&model.session.confirm),
	}))
}

func (model *Model) receivePlanFailure(message planFailedMsg) (tea.Model, tea.Cmd) {
	model.status = fmt.Sprintf("Planning failed: %v", message.err)
	return model.reopenPlan(message.kind)
}

func (model *Model) receivePublishResult(message publishResultMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		model.status = fmt.Sprintf("Publication failed: %v", message.err)
		model.pending = nil
		return model.reopenPlan(message.kind)
	}
	model.snapshot = message.snapshot
	model.clearDraft(message.kind)
	model.pending = nil
	model.status = "Configuration published."
	if message.refreshErr != nil {
		model.status += " Refresh failed: " + message.refreshErr.Error()
	}
	model.toMenu()
	return *model, nil
}

func (model *Model) receiveReviewSelection(message reviewSelectionLoadedMsg) (tea.Model, tea.Cmd) {
	if model.formKind != formReviewLoading {
		return *model, nil
	}
	if message.err != nil {
		model.status = fmt.Sprintf("Loading Repository Reviews failed: %v", message.err)
		return model.withForm(model.openReviewOperationForm())
	}
	model.session.reviewSelection = message.selection
	return model.withForm(model.openReviewFieldsForm())
}

func (model *Model) receiveTemplate(message templateLoadedMsg) (tea.Model, tea.Cmd) {
	if model.formKind != formProfileTemplateLoading {
		return *model, nil
	}
	if message.err != nil {
		model.status = fmt.Sprintf("Loading template failed: %v", message.err)
		return model.withForm(model.openProfileSourceForm())
	}
	if !message.found {
		model.status = fmt.Sprintf("Unknown Review Profile Template %q", model.session.templateID)
		return model.withForm(model.openProfileSourceForm())
	}
	model.session.profile.draft.TemplateRevision = message.template.Revision
	model.session.templateInstructions = message.template.Instructions
	return model.withForm(model.openProfileInstructionsForm())
}

func (model *Model) receiveInstructionEdit(message instructionEditResultMsg) (tea.Model, tea.Cmd) {
	if model.formKind != formEditor {
		return *model, nil
	}
	if message.err != nil {
		model.status = message.err.Error()
		return model.withForm(model.openProfileInstructionsForm())
	}
	model.session.profile.draft.TemplateID = ""
	model.session.profile.draft.TemplateRevision = ""
	model.session.profile.draft.Instructions = message.instructions
	model.session.instructions = message.instructions
	model.session.useEditor = false
	model.syncFormDraft()
	return model.startPlan(planRequest{kind: planProfile, profile: model.drafts.profile})
}

func (model *Model) clearDraft(kind planKind) {
	switch kind {
	case planProfile:
		model.drafts.profile = configuration.ProfileDraft{}
	case planParty:
		model.drafts.party = partyFormDraft{}
	case planCopy:
		model.drafts.copyName = ""
	case planReviews:
		model.drafts.reviews = reviewFormDraft{}
	}
}

func (model *Model) toMenu() {
	model.view = viewMenu
	model.form = formAdapter{}
	model.formKind = formNone
	model.pending = nil
	model.pendingKind = ""
	model.viewport.SetContent(model.viewportContent())
}

func (model Model) renderForm() string {
	title := lipgloss.NewStyle().Bold(true).Render("Review Party Configuration Hub")
	var output strings.Builder
	fmt.Fprintf(&output, "%s\nGlobal Configuration · Repository %s\n\n", title, model.snapshot.Repository)
	if model.status != "" {
		fmt.Fprintf(&output, "configuration error: %s\n\n", model.status)
	}
	if content := model.form.View(); content != "" {
		output.WriteString(content)
	} else {
		switch model.formKind {
		case formPlanning:
			output.WriteString("Planning configuration changes...\n")
		case formEditor:
			output.WriteString("Editing instructions...\n")
		case formPublishing:
			output.WriteString("Publishing configuration changes...\n")
		case formNone, formOverview, formProfileFields, formProfileSource, formProfileTemplate,
			formProfileTemplateLoading, formProfileInstructions, formParty, formReviewOperation,
			formReviewLoading, formReviewFields, formCopy, formChanges, formConfirm:
			// The active form supplies the content.
		}
	}
	output.WriteString("\nEsc back  Ctrl-C quit\n")
	return output.String()
}
