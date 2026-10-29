package configurationhub

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
	"reviewparty/internal/discovery"
)

type formKind string

const (
	formNone                   formKind = ""
	formOverview               formKind = "overview"
	formProfileFields          formKind = "profile-fields"
	formProfileChoicesLoading  formKind = "profile-choices-loading"
	formProfileModel           formKind = "profile-model"
	formProfileModelManual     formKind = "profile-model-manual"
	formProfileEffort          formKind = "profile-effort"
	formProfileEffortManual    formKind = "profile-effort-manual"
	formProfileDeadline        formKind = "profile-deadline"
	formProfileSource          formKind = "profile-source"
	formProfileTemplate        formKind = "profile-template"
	formProfileTemplateLoading formKind = "profile-template-loading"
	formProfileInstructions    formKind = "profile-instructions"
	formEditor                 formKind = "editor"
	formParty                  formKind = "party"
	formReviewFields           formKind = "review-fields"
	formCheckpointName         formKind = "checkpoint-name"
	formCheckpointFields       formKind = "checkpoint-fields"
	formCopy                   formKind = "copy"
	formChanges                formKind = "changes"
	formPlanning               formKind = "planning"
	formProfileEdit            formKind = "profile-edit"
	formProfileEditLoading     formKind = "profile-edit-loading"
)

type formSession struct {
	profile              profileFormState
	party                partyFormDraft
	copyName             string
	editProfile          string
	reviews              reviewFormDraft
	checkpoints          checkpointFormDraft
	source               string
	templateID           string
	templateInstructions string
	instructions         string
	useEditor            bool
	confirm              bool
}

type profileFormState struct {
	draft       configuration.ProfileDraft
	target      string
	editing     bool
	accessors   map[string]*profileFieldAccessor
	choices     []discovery.ModelChoice
	selected    string
	effort      string
	manualModel string
	generation  uint64
	diagnostic  string
}

const (
	manualProfileChoice  = "__manual__"
	refreshProfileChoice = "__refresh__"
)

func newFormSession(drafts draftSet) *formSession {
	return &formSession{
		profile:      profileFormState{draft: drafts.profile, target: string(drafts.profile.Target)},
		party:        drafts.party,
		copyName:     drafts.copyName,
		reviews:      drafts.reviews,
		checkpoints:  drafts.checkpoints,
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
	case AreaReviews, AreaCheckpoints:
		model.status = fmt.Sprintf("%s are edited from their rows.", area)
		return model.openOverviewForm()
	case AreaChanges:
		return model.openChangesForm()
	case areaCopyProfile:
		return model.openCopyForm()
	case areaSearch:
		model.status = "Search is available from the menu."
		return model.openOverviewForm()
	default:
		model.status = fmt.Sprintf("unknown Hub area %q", area)
		return model.openOverviewForm()
	}
}

func (model *Model) openReviewFieldsFor(operation reviewOperation, target configuration.Scope) tea.Cmd {
	model.session.reviews.operation = operation
	model.session.reviews.target = target
	return model.openReviewFieldsForm()
}

func (model Model) startRowReview(operation reviewOperation) (tea.Model, tea.Cmd) {
	row, ok := model.cursorRow()
	if !ok || row.Kind != itemReview {
		model.status = "Choose a Review row."
		return model, nil
	}
	draft := reviewFormDraft{target: row.Target, operation: operation, reviewEntry: row.entry()}
	if operation == reviewMove {
		if model.moveFrom.Target != row.Target || model.moveFrom.Scope != row.Scope {
			model.status = "Move within one group."
			return model, nil
		}
		draft.reviewEntry, draft.to = model.moveFrom.entry(), row.Index
	}
	return model.startReviewPlan(draft)
}

func (model Model) startReviewPlan(draft reviewFormDraft) (tea.Model, tea.Cmd) {
	model.session.reviews = draft
	model.drafts.reviews = draft
	return model.startPlan(planRequest{kind: planReviews, review: reviewPlanRequest{draft: draft, globalOnly: !model.snapshot.writes(configuration.ScopeRepository)}})
}

func (model Model) startCheckpointPlan(draft checkpointFormDraft) (tea.Model, tea.Cmd) {
	model.session.checkpoints = draft
	model.drafts.checkpoints = draft
	return model.startPlan(planRequest{kind: planCheckpoints, checkpoint: draft})
}

func (model *Model) openCheckpointSet(row Item) tea.Cmd {
	model.status = ""
	if row.Kind == itemCheckpoint {
		model.session.checkpoints = model.snapshot.Checkpoints.setDraft(row.Target, configuration.CheckpointName(row.Name), configuration.NewCheckpoint())
		return model.openCheckpointFieldsForm()
	}
	model.session.checkpoints = checkpointFormDraft{target: row.Target, operation: checkpointSet, checkpointDraft: checkpointDraft{name: configuration.CheckpointPrePush}}
	file, _ := model.snapshot.Checkpoints.file(row.Target)
	return model.openForm(formCheckpointName, []huh.Field{checkpointNameField(&model.session.checkpoints, file)})
}

func (model *Model) openCheckpointFieldsForm() tea.Cmd {
	return model.openForm(formCheckpointFields, checkpointFields(&model.session.checkpoints.checkpointDraft))
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
	return model.frameBudget(renderOptions{styled: true}).viewport
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
		lines = append(lines, warningStyle.Render("▲ "+warning))
	}
	return strings.Join(lines, "\n")
}

func (model *Model) openProfileSourceForm() tea.Cmd {
	return model.openForm(formProfileSource, []huh.Field{
		huh.NewNote().Title("Instruction source").Description("Start from a packaged or skill Review Profile Template, or from blank instructions."),
		huh.NewSelect[string]().Title("Instruction source").Options(
			huh.NewOption("Template", "template"), huh.NewOption("Blank", "blank"),
		).Value(&model.session.source),
	})
}

func (model Model) profileTemplateOptions() []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(model.snapshot.Templates))
	for _, template := range model.snapshot.Templates {
		options = append(options, huh.NewOption(template.ID+" (Review Profile Template "+template.Revision+")", template.ID))
	}
	return options
}

func (model *Model) openProfileTemplateForm() tea.Cmd {
	options := model.profileTemplateOptions()
	if len(options) == 0 {
		model.status = "No Review Profile Templates are available. Choose blank instructions."
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
	options := model.profileReferenceOptions()
	profileSelect := huh.NewMultiSelect[string]().Title("Profiles").
		OptionsFunc(func() []huh.Option[string] { return partyMemberOptions(options, draft.scope) }, &draft.scope).
		Value(&draft.profileRefs).WithHeight(min(max(len(options)+1, 2), 8))
	return model.openForm(formParty, append(scopeFields(model.snapshot, &draft.scope),
		huh.NewInput().Title("Party name").Value(&draft.name),
		huh.NewInput().Title("Description").Value(&draft.description),
		profileSelect,
		huh.NewInput().Title("Concurrency limit").Value(&draft.limit),
	))
}

func (model *Model) openReviewFieldsForm() tea.Cmd {
	draft := &model.session.reviews
	fields, err := reviewFields(draft, model.snapshot)
	if err != nil {
		model.toMenu()
		model.status = err.Error()
		return nil
	}
	if draft.operation == reviewMove {
		fields = append(fields, reviewDestinationField(draft, model.snapshot.Items))
	}
	if len(fields) == 0 {
		model.toMenu()
		model.drafts.reviews = reviewFormDraft{}
		return nil
	}
	return model.openForm(formReviewFields, fields)
}

func (model *Model) openCopyForm() tea.Cmd {
	options := model.repositoryProfileOptions()
	if len(options) == 0 {
		model.status = "No Repository Profiles exist to copy."
		return nil
	}
	return model.openForm(formCopy, []huh.Field{
		huh.NewSelect[string]().Title("Repository Profile to copy to Global Configuration").Options(options...).Value(&model.session.copyName),
	})
}

func (model Model) profileReferenceOptions() []huh.Option[string] {
	return model.profileOptions("Profiles", true)
}

func (model Model) repositoryProfileOptions() []huh.Option[string] {
	return model.profileOptions("Repository Profiles", false)
}

func (model Model) profileOptions(title string, bothScopes bool) []huh.Option[string] {
	options := make([]huh.Option[string], 0)
	for _, scope := range []ItemScope{"global", "repository"} {
		if !bothScopes && scope != ItemScope("repository") {
			continue
		}
		for _, item := range model.snapshot.Items {
			if item.Kind == itemProfile && item.Scope == scope {
				value := string(scope) + ":" + item.Name
				if !bothScopes {
					value = item.Name
				}
				options = append(options, huh.NewOption("["+string(scope)+"] "+item.Name, value))
			}
		}
	}
	return options
}

func validatePositiveInteger(value string) error {
	limit, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || limit <= 0 {
		return fmt.Errorf("must be an integer greater than 0")
	}
	return nil
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
	case formProfileFields, formProfileChoicesLoading, formProfileModel, formProfileModelManual, formProfileEffort,
		formProfileEffortManual, formProfileDeadline, formProfileSource, formProfileTemplate,
		formProfileTemplateLoading, formProfileInstructions, formEditor:
		state := model.session.profile
		state.draft.Target = configuration.Scope(state.target)
		if state.draft.TemplateID == "" {
			state.draft.Instructions = model.session.instructions
		}
		model.drafts.profile = state.draft
	case formParty:
		model.drafts.party = model.session.party
	case formReviewFields:
		model.drafts.reviews = model.session.reviews
	case formCheckpointName, formCheckpointFields:
		model.drafts.checkpoints = model.session.checkpoints
	case formCopy:
		model.drafts.copyName = model.session.copyName
	case formNone, formOverview, formChanges, formPlanning, formProfileEdit, formProfileEditLoading:
		// These views do not edit a draft directly.
	}
}

func (model *Model) updateForm(message tea.Msg) (tea.Model, tea.Cmd) {
	if _, resized := message.(tea.WindowSizeMsg); resized {
		message = tea.WindowSizeMsg{Width: model.formWidth(), Height: model.formHeight()}
	}
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
	case formProfileEdit:
		return model.completeProfileEditSelectForm()
	case formProfileFields, formProfileModel, formProfileModelManual, formProfileEffort,
		formProfileEffortManual, formProfileDeadline, formProfileSource, formProfileTemplate, formProfileInstructions:
		return model.completeProfileForm()
	case formParty, formReviewFields, formCheckpointFields, formCopy:
		return model.completePlanForm()
	case formCheckpointName:
		draft := model.session.checkpoints
		model.session.checkpoints = model.snapshot.Checkpoints.setDraft(draft.target, draft.name, configuration.NewCheckpoint())
		return model.withForm(model.openCheckpointFieldsForm())
	case formNone, formProfileChoicesLoading, formProfileTemplateLoading, formEditor, formPlanning,
		formProfileEditLoading:
		// These states do not accept a completed form.
	}
	return *model, nil
}

func (model *Model) completeProfileForm() (tea.Model, tea.Cmd) {
	switch model.formKind {
	case formProfileFields:
		return model.completeProfileFieldsForm()
	case formProfileModel, formProfileModelManual, formProfileEffort, formProfileEffortManual, formProfileDeadline:
		return model.completeProfileChoiceForm()
	case formProfileSource:
		return model.completeProfileSourceForm()
	case formProfileTemplate:
		return model.completeProfileTemplateForm()
	case formProfileInstructions:
		return model.completeProfileInstructionsForm()
	case formNone, formOverview, formProfileChoicesLoading, formProfileTemplateLoading, formEditor, formParty,
		formReviewFields, formCheckpointName, formCheckpointFields, formCopy, formChanges,
		formPlanning, formProfileEdit, formProfileEditLoading:
		// Only profile forms reach this dispatcher.
	}
	return *model, nil
}

func (model *Model) completeProfileChoiceForm() (tea.Model, tea.Cmd) {
	switch model.formKind {
	case formProfileModel:
		return model.completeProfileModelForm()
	case formProfileModelManual:
		return model.completeProfileModelManualForm()
	case formProfileEffort:
		return model.completeProfileEffortForm()
	case formProfileEffortManual:
		return model.withForm(model.openProfileDeadlineForm())
	case formProfileDeadline:
		return model.completeProfileExecutionForm()
	case formNone, formOverview, formProfileFields, formProfileChoicesLoading, formProfileSource,
		formProfileTemplate, formProfileTemplateLoading, formProfileInstructions, formEditor,
		formParty, formReviewFields, formCheckpointName, formCheckpointFields, formCopy, formChanges,
		formPlanning, formProfileEdit, formProfileEditLoading:
		return *model, nil
	}
	return *model, nil
}

func (model *Model) completePlanForm() (tea.Model, tea.Cmd) {
	switch model.formKind {
	case formParty:
		return model.startPlan(planRequest{kind: planParty, party: model.drafts.party})
	case formReviewFields:
		return model.startPlan(planRequest{kind: planReviews, review: reviewPlanRequest{draft: model.drafts.reviews, globalOnly: !model.snapshot.writes(configuration.ScopeRepository)}})
	case formCheckpointFields:
		return model.startPlan(planRequest{kind: planCheckpoints, checkpoint: model.drafts.checkpoints})
	case formCopy:
		return model.startPlan(planRequest{kind: planCopy, copy: model.drafts.copyName})
	case formNone, formOverview, formProfileFields, formProfileSource, formProfileTemplate,
		formProfileChoicesLoading, formProfileModel, formProfileModelManual, formProfileEffort,
		formProfileEffortManual, formProfileDeadline, formProfileTemplateLoading, formProfileInstructions, formEditor,
		formCheckpointName, formChanges, formPlanning, formProfileEdit, formProfileEditLoading:
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

func (model *Model) startPlan(request planRequest) (tea.Model, tea.Cmd) {
	model.pendingKind = request.kind
	model.formKind = formPlanning
	model.form = formAdapter{}
	model.view = viewForm
	if model.runtime == nil || model.runtime.commands.plan == nil {
		model.status = "No plan command is available."
		return model.reopenPlan(request.kind)
	}
	if request.receipts == nil {
		request.receipts = model.runtime.receipts
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
	case planProfileEdit:
		if model.isProfileEdit() {
			return model.withForm(model.openProfileEditFieldsForm())
		}
		return model.withForm(model.openProfileEditSelectForm())
	case planParty:
		return model.withForm(model.openPartyForm())
	case planReviews:
		return model.withForm(model.openReviewFieldsForm())
	case planCheckpoints:
		if model.session.checkpoints.operation == checkpointSet {
			return model.withForm(model.openCheckpointFieldsForm())
		}
		model.toMenu()
		model.drafts.checkpoints = checkpointFormDraft{}
		return *model, nil
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
	model.planSummary = message.summary
	model.viewport.GotoTop()
	model.planWarnings = message.warnings
	model.status = ""
	model.view = viewPlanPreview
	model.form = formAdapter{}
	model.formKind = formNone
	return *model, nil
}

func (model *Model) receivePlanFailure(message planFailedMsg) (tea.Model, tea.Cmd) {
	model.status = fmt.Sprintf("Planning failed: %v", message.err)
	return model.reopenPlan(message.kind)
}

func (model *Model) receivePublishResult(message publishResultMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		model.outcome = fmt.Sprintf("Publish failed: %v (%s)", message.err, model.publishedTarget(message.kind))
		model.outcomeGood = false
		model.status = ""
		model.pending = nil
		return model.reopenPlan(message.kind)
	}
	publishedTarget := model.publishedTarget(message.kind)
	model.snapshot = message.snapshot
	model.clearDraft(message.kind)
	model.pending = nil
	model.outcome = "Published " + publishedTarget
	model.outcomeGood = true
	model.notice = message.notice
	model.status = ""
	if message.refreshErr != nil {
		model.status = "Refresh failed: " + message.refreshErr.Error()
	}
	model.toMenu()
	return *model, nil
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
	case planProfile, planProfileEdit:
		model.drafts.profile = configuration.ProfileDraft{}
	case planParty:
		model.drafts.party = partyFormDraft{}
	case planCopy:
		model.drafts.copyName = ""
	case planReviews:
		model.drafts.reviews = reviewFormDraft{}
	case planCheckpoints:
		model.drafts.checkpoints = checkpointFormDraft{}
	}
}

func (model *Model) toMenu() {
	if model.runtime != nil {
		model.runtime.closeProfileChoices()
	}
	model.view = viewMenu
	model.form = formAdapter{}
	model.formKind = formNone
	model.pending = nil
	model.pendingKind = ""
	model.viewport.GotoTop()
}
