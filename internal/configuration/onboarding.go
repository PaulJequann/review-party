package configuration

import (
	"errors"
	"fmt"
	"strings"
)

// OnboardingStep is the stable sequence consumed by a human Hub editor.
type OnboardingStep string

// OnboardingText is caller-supplied text entered during Profile onboarding.
type OnboardingText string

// OnboardingField identifies an editable Profile field.
type OnboardingField string

const (
	OnboardingChooseSource OnboardingStep = "choose_source"
	OnboardingName         OnboardingStep = "name"
	OnboardingReviewer     OnboardingStep = "reviewer"
	OnboardingModel        OnboardingStep = "model"
	OnboardingEffort       OnboardingStep = "effort"
	OnboardingDeadline     OnboardingStep = "deadline"
	OnboardingValidation   OnboardingStep = "validation"
	OnboardingReview       OnboardingStep = "review"
	OnboardingComplete     OnboardingStep = "complete"
	OnboardingCancelled    OnboardingStep = "cancelled"

	OnboardingFieldName     OnboardingField = "name"
	OnboardingFieldReviewer OnboardingField = "reviewer"
	OnboardingFieldModel    OnboardingField = "model"
	OnboardingFieldEffort   OnboardingField = "effort"
	OnboardingFieldDeadline OnboardingField = "deadline"
)

// ProfileOnboarding is the non-UI state machine for first Profile creation.
// It keeps a draft in memory until a caller validates and explicitly confirms
// one Manager Plan. Cancellation never publishes a partial Profile.
type ProfileOnboarding struct {
	manager          *Manager
	draft            ProfileDraft
	modelChoiceCheck ModelChoiceCheck
	revising         bool
	step             OnboardingStep
	plan             Plan
}

// NewProfileOnboarding starts a Profile flow in the requested Configuration
// scope. It performs no filesystem work.
func NewProfileOnboarding(manager *Manager, target Scope) *ProfileOnboarding {
	return &ProfileOnboarding{manager: manager, draft: ProfileDraft{Target: target}, step: OnboardingChooseSource}
}

// Step reports the next onboarding step.
func (flow *ProfileOnboarding) Step() OnboardingStep { return flow.step }

// Draft returns a copy of the current in-memory Profile draft.
func (flow *ProfileOnboarding) Draft() ProfileDraft {
	return flow.draft
}

// ChooseTemplate selects immutable packaged instructions. The template is
// copied by Manager.PlanProfileCreation; later package updates cannot mutate
// this saved Profile.
func (flow *ProfileOnboarding) ChooseTemplate(id string) error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	if flow.manager == nil {
		return errors.New("onboarding requires a Configuration Manager")
	}
	for _, template := range flow.manager.Templates() {
		if template.ID == id {
			flow.clearDependentFields(OnboardingChooseSource)
			flow.revising = false
			flow.draft.TemplateID = id
			flow.draft.TemplateRevision = template.Revision
			flow.draft.Instructions = ""
			flow.plan = Plan{}
			flow.step = flow.nextStep()
			return nil
		}
	}
	return fmt.Errorf("unknown Review Profile Template %q", id)
}

// ChooseBlank starts a flow with caller-supplied instructions and no Template.
func (flow *ProfileOnboarding) ChooseBlank(instructions OnboardingText) error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	flow.clearDependentFields(OnboardingChooseSource)
	flow.revising = false
	flow.draft.TemplateID = ""
	flow.draft.TemplateRevision = ""
	flow.draft.Instructions = string(instructions)
	flow.plan = Plan{}
	flow.step = flow.nextStep()
	return nil
}

// SetInstructions edits the instruction material after choosing a source.
func (flow *ProfileOnboarding) SetInstructions(instructions OnboardingText) error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	if flow.step == OnboardingChooseSource {
		return errors.New("choose a Template or blank instructions first")
	}
	flow.draft.TemplateID = ""
	flow.draft.TemplateRevision = ""
	flow.draft.Instructions = string(instructions)
	flow.revising = false
	flow.modelChoiceCheck = ModelChoiceCheck{}
	flow.plan = Plan{}
	flow.step = flow.nextStep()
	return nil
}

// Set updates one executable Profile field and advances to the first missing
// field. During revision, unchanged fields are traversed in order so retained
// values remain editable. Changing an earlier field clears every dependent
// field.
func (flow *ProfileOnboarding) Set(field OnboardingField, value OnboardingText) error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	spec, found := onboardingFieldSpecFor(field)
	if !found {
		return fmt.Errorf("unknown onboarding field %q", field)
	}
	if !onboardingFieldReady(flow.draft, spec.step) {
		return fmt.Errorf("onboarding is not ready for %s", spec.step)
	}
	previous := spec.read(flow.draft)
	spec.write(&flow.draft, string(value))
	if previous != string(value) {
		flow.clearDependentFields(spec.step)
		flow.revising = false
	}
	flow.plan = Plan{}
	if flow.revising {
		flow.step = flow.nextRevisionStep(spec.step)
	} else {
		flow.step = flow.nextStep()
	}
	return nil
}

// SetModelChoiceCheck attaches an advisory immediate-choice check to the
// flow. It is consumed by Validate and never changes the saved Profile.
func (flow *ProfileOnboarding) SetModelChoiceCheck(check ModelChoiceCheck) error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	if flow.step == OnboardingReview {
		return errors.New("onboarding choice checks cannot change after validation")
	}
	flow.modelChoiceCheck = check
	flow.plan = Plan{}
	return nil
}

// Validate creates the reviewed, opaque Manager Plan without publishing it.
func (flow *ProfileOnboarding) Validate(repository Repository) (Plan, error) {
	if err := flow.ensureEditable(); err != nil {
		return Plan{}, err
	}
	if flow.step != OnboardingValidation {
		return Plan{}, errors.New("complete onboarding fields before validation")
	}
	if flow.manager == nil {
		return Plan{}, errors.New("onboarding requires a Configuration Manager")
	}
	plan, err := flow.manager.PlanProfileCreation(repository, flow.draft)
	if err != nil {
		return Plan{}, err
	}
	plan = plan.WithWarnings(flow.modelChoiceCheck.Warning(flow.draft.Reviewer, flow.draft.Model))
	flow.plan = plan
	if plan.Valid() {
		flow.step = OnboardingReview
	} else {
		flow.step = OnboardingValidation
	}
	return plan, nil
}

// Plan returns the last validation plan for the Hub's review screen.
func (flow *ProfileOnboarding) Plan() Plan { return flow.plan }

// Revise returns a retained, validated draft to its first editable field.
// It clears the reviewed Plan but keeps the selected source and executable
// values so a caller can correct and validate the draft again.
func (flow *ProfileOnboarding) Revise() error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	if flow.step != OnboardingValidation && flow.step != OnboardingReview {
		return errors.New("onboarding can only be revised after validation")
	}
	flow.modelChoiceCheck = ModelChoiceCheck{}
	flow.plan = Plan{}
	flow.revising = true
	flow.step = OnboardingName
	return nil
}

// Confirm publishes the last valid plan. The caller owns the human or
// machine-facing confirmation immediately before calling this method.
func (flow *ProfileOnboarding) Confirm() error {
	if flow.step != OnboardingReview || !flow.plan.Valid() {
		return errors.New("onboarding requires a valid reviewed Plan before confirmation")
	}
	if flow.manager == nil {
		return errors.New("onboarding requires a Configuration Manager")
	}
	if err := flow.manager.Publish(flow.plan); err != nil {
		return err
	}
	flow.step = OnboardingComplete
	return nil
}

// Cancel abandons publication while retaining the draft for a caller that
// wants to return to the editor.
func (flow *ProfileOnboarding) Cancel() error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	flow.plan = Plan{}
	flow.step = OnboardingCancelled
	return nil
}

// Resume returns a cancelled flow to the first incomplete editor step. It
// does not validate or publish the retained draft.
func (flow *ProfileOnboarding) Resume() error {
	if flow == nil {
		return errors.New("onboarding flow is nil")
	}
	if flow.step != OnboardingCancelled {
		return errors.New("onboarding is not cancelled")
	}
	flow.step = flow.nextStep()
	return nil
}

// Discard clears the draft after a caller has explicitly chosen to lose it.
func (flow *ProfileOnboarding) Discard() {
	flow.draft = ProfileDraft{Target: flow.draft.Target}
	flow.modelChoiceCheck = ModelChoiceCheck{}
	flow.revising = false
	flow.plan = Plan{}
	flow.step = OnboardingChooseSource
}

func (flow *ProfileOnboarding) ensureEditable() error {
	if flow == nil {
		return errors.New("onboarding flow is nil")
	}
	if flow.step == OnboardingComplete || flow.step == OnboardingCancelled {
		return fmt.Errorf("onboarding is %s", flow.step)
	}
	return nil
}

func (flow *ProfileOnboarding) clearDependentFields(step OnboardingStep) {
	clearFollowing := step == OnboardingChooseSource
	resetModelChoices := clearFollowing
	for _, spec := range onboardingFields {
		if spec.step == step {
			clearFollowing = true
			resetModelChoices = resetModelChoices || spec.resetModelChoices
			continue
		}
		if !clearFollowing {
			continue
		}
		spec.write(&flow.draft, "")
		resetModelChoices = resetModelChoices || spec.resetModelChoices
	}
	if resetModelChoices {
		flow.modelChoiceCheck = ModelChoiceCheck{}
	}
}

func (flow *ProfileOnboarding) nextStep() OnboardingStep {
	if flow.draft.TemplateID == "" && !hasOnboardingText(flow.draft.Instructions) {
		return OnboardingChooseSource
	}
	for _, spec := range onboardingFields {
		if !hasOnboardingText(spec.read(flow.draft)) {
			return spec.step
		}
	}
	return OnboardingValidation
}

func (flow *ProfileOnboarding) nextRevisionStep(step OnboardingStep) OnboardingStep {
	for index, spec := range onboardingFields {
		if spec.step != step {
			continue
		}
		if index+1 == len(onboardingFields) {
			return OnboardingValidation
		}
		return onboardingFields[index+1].step
	}
	return flow.nextStep()
}

func onboardingFieldSpecFor(field OnboardingField) (onboardingFieldSpec, bool) {
	for _, spec := range onboardingFields {
		if spec.field == field {
			return spec, true
		}
	}
	return onboardingFieldSpec{}, false
}

func onboardingFieldReady(draft ProfileDraft, target OnboardingStep) bool {
	if draft.TemplateID == "" && !hasOnboardingText(draft.Instructions) {
		return false
	}
	for _, spec := range onboardingFields {
		if spec.step == target {
			return true
		}
		if !hasOnboardingText(spec.read(draft)) {
			return false
		}
	}
	return false
}

type onboardingFieldSpec struct {
	field             OnboardingField
	step              OnboardingStep
	read              func(ProfileDraft) string
	write             func(*ProfileDraft, string)
	resetModelChoices bool
}

var onboardingFields = [...]onboardingFieldSpec{
	{field: OnboardingFieldName, step: OnboardingName, read: func(draft ProfileDraft) string { return draft.Name }, write: func(draft *ProfileDraft, value string) { draft.Name = value }},
	{field: OnboardingFieldReviewer, step: OnboardingReviewer, read: func(draft ProfileDraft) string { return draft.Reviewer }, write: func(draft *ProfileDraft, value string) { draft.Reviewer = value }, resetModelChoices: true},
	{field: OnboardingFieldModel, step: OnboardingModel, read: func(draft ProfileDraft) string { return draft.Model }, write: func(draft *ProfileDraft, value string) { draft.Model = value }, resetModelChoices: true},
	{field: OnboardingFieldEffort, step: OnboardingEffort, read: func(draft ProfileDraft) string { return draft.ReasoningEffort }, write: func(draft *ProfileDraft, value string) { draft.ReasoningEffort = value }},
	{field: OnboardingFieldDeadline, step: OnboardingDeadline, read: func(draft ProfileDraft) string { return draft.AttemptDeadline }, write: func(draft *ProfileDraft, value string) { draft.AttemptDeadline = value }},
}

func hasOnboardingText(value string) bool {
	return strings.TrimSpace(value) != ""
}
