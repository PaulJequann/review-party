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
	flow.modelChoiceCheck = ModelChoiceCheck{}
	flow.plan = Plan{}
	flow.step = flow.nextStep()
	return nil
}

// Set updates one executable Profile field and advances to the first missing
// field. Changing an earlier field clears every dependent field.
func (flow *ProfileOnboarding) Set(field OnboardingField, value OnboardingText) error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	step, found := onboardingFieldStep(field)
	if !found {
		return fmt.Errorf("unknown onboarding field %q", field)
	}
	if onboardingStepRank(flow.step) < onboardingStepRank(step) {
		return fmt.Errorf("onboarding is not ready for %s", step)
	}
	setOnboardingField(&flow.draft, field, value)
	flow.clearDependentFields(step)
	flow.plan = Plan{}
	flow.step = flow.nextStep()
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
	plan, err := flow.manager.PlanProfileCreationWithModelChoiceCheck(repository, flow.draft, flow.modelChoiceCheck)
	if err != nil {
		return Plan{}, err
	}
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
	switch step {
	case OnboardingChooseSource:
		flow.draft.Name = ""
		flow.draft.Reviewer = ""
		flow.draft.Model = ""
		flow.draft.ReasoningEffort = ""
		flow.draft.AttemptDeadline = ""
		flow.modelChoiceCheck = ModelChoiceCheck{}
	case OnboardingName:
		flow.draft.Reviewer = ""
		flow.draft.Model = ""
		flow.draft.ReasoningEffort = ""
		flow.draft.AttemptDeadline = ""
		flow.modelChoiceCheck = ModelChoiceCheck{}
	case OnboardingReviewer:
		flow.draft.Model = ""
		flow.draft.ReasoningEffort = ""
		flow.draft.AttemptDeadline = ""
		flow.modelChoiceCheck = ModelChoiceCheck{}
	case OnboardingModel:
		flow.draft.ReasoningEffort = ""
		flow.draft.AttemptDeadline = ""
		flow.modelChoiceCheck = ModelChoiceCheck{}
	case OnboardingEffort:
		flow.draft.AttemptDeadline = ""
	}
}

func (flow *ProfileOnboarding) nextStep() OnboardingStep {
	if flow.draft.TemplateID == "" && !hasOnboardingText(flow.draft.Instructions) {
		return OnboardingChooseSource
	}
	if !hasOnboardingText(flow.draft.Name) {
		return OnboardingName
	}
	if !hasOnboardingText(flow.draft.Reviewer) {
		return OnboardingReviewer
	}
	if !hasOnboardingText(flow.draft.Model) {
		return OnboardingModel
	}
	if !hasOnboardingText(flow.draft.ReasoningEffort) {
		return OnboardingEffort
	}
	if !hasOnboardingText(flow.draft.AttemptDeadline) {
		return OnboardingDeadline
	}
	return OnboardingValidation
}

func onboardingStepRank(step OnboardingStep) int {
	if rank, found := onboardingStepOrder[step]; found {
		return rank
	}
	return -1
}

var onboardingStepOrder = map[OnboardingStep]int{
	OnboardingChooseSource: 0,
	OnboardingName:         1,
	OnboardingReviewer:     2,
	OnboardingModel:        3,
	OnboardingEffort:       4,
	OnboardingDeadline:     5,
	OnboardingValidation:   6,
	OnboardingReview:       7,
	OnboardingComplete:     8,
	OnboardingCancelled:    9,
}

func onboardingFieldStep(field OnboardingField) (OnboardingStep, bool) {
	switch field {
	case OnboardingFieldName:
		return OnboardingName, true
	case OnboardingFieldReviewer:
		return OnboardingReviewer, true
	case OnboardingFieldModel:
		return OnboardingModel, true
	case OnboardingFieldEffort:
		return OnboardingEffort, true
	case OnboardingFieldDeadline:
		return OnboardingDeadline, true
	}
	return "", false
}

func setOnboardingField(draft *ProfileDraft, field OnboardingField, value OnboardingText) {
	switch field {
	case OnboardingFieldName:
		draft.Name = string(value)
	case OnboardingFieldReviewer:
		draft.Reviewer = string(value)
	case OnboardingFieldModel:
		draft.Model = string(value)
	case OnboardingFieldEffort:
		draft.ReasoningEffort = string(value)
	case OnboardingFieldDeadline:
		draft.AttemptDeadline = string(value)
	}
}

func hasOnboardingText(value string) bool {
	return strings.TrimSpace(value) != ""
}
