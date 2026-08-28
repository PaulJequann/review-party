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
)

// ProfileOnboarding is the non-UI state machine for first Profile creation.
// It keeps a draft in memory until a caller validates and explicitly confirms
// one Manager Plan. Cancellation never publishes a partial Profile.
type ProfileOnboarding struct {
	draft ProfileDraft
	step  OnboardingStep
	plan  Plan
}

type onboardingStepSpec struct {
	step     OnboardingStep
	terminal bool
	ready    func(ProfileDraft) bool
	clear    func(*ProfileDraft)
	set      func(*ProfileDraft, OnboardingText)
}

var onboardingSteps = []onboardingStepSpec{
	{step: OnboardingChooseSource, ready: func(draft ProfileDraft) bool {
		return draft.TemplateID != "" || strings.TrimSpace(draft.Instructions) != ""
	}},
	{step: OnboardingName, ready: func(draft ProfileDraft) bool { return hasOnboardingText(draft.Name) }, clear: func(draft *ProfileDraft) { draft.Name = "" }, set: func(draft *ProfileDraft, value OnboardingText) { draft.Name = string(value) }},
	{step: OnboardingReviewer, ready: func(draft ProfileDraft) bool { return hasOnboardingText(draft.Reviewer) }, clear: func(draft *ProfileDraft) { draft.Reviewer = "" }, set: func(draft *ProfileDraft, value OnboardingText) { draft.Reviewer = string(value) }},
	{step: OnboardingModel, ready: func(draft ProfileDraft) bool { return hasOnboardingText(draft.Model) }, clear: func(draft *ProfileDraft) { draft.Model = "" }, set: func(draft *ProfileDraft, value OnboardingText) { draft.Model = string(value) }},
	{step: OnboardingEffort, ready: func(draft ProfileDraft) bool { return hasOnboardingText(draft.ReasoningEffort) }, clear: func(draft *ProfileDraft) { draft.ReasoningEffort = "" }, set: func(draft *ProfileDraft, value OnboardingText) { draft.ReasoningEffort = string(value) }},
	{step: OnboardingDeadline, ready: func(draft ProfileDraft) bool { return hasOnboardingText(draft.AttemptDeadline) }, clear: func(draft *ProfileDraft) { draft.AttemptDeadline = "" }, set: func(draft *ProfileDraft, value OnboardingText) { draft.AttemptDeadline = string(value) }},
	{step: OnboardingValidation, terminal: true},
	{step: OnboardingReview, terminal: true},
	{step: OnboardingComplete, terminal: true},
	{step: OnboardingCancelled, terminal: true},
}

// NewProfileOnboarding starts a Profile flow in the requested Configuration
// scope. It performs no filesystem work.
func NewProfileOnboarding(target Scope) *ProfileOnboarding {
	return &ProfileOnboarding{draft: ProfileDraft{Target: target}, step: OnboardingChooseSource}
}

// Step reports the next onboarding step.
func (flow *ProfileOnboarding) Step() OnboardingStep { return flow.step }

// Draft returns a copy of the current in-memory Profile draft.
func (flow *ProfileOnboarding) Draft() ProfileDraft {
	draft := flow.draft
	return draft
}

// ChooseTemplate selects immutable packaged instructions. The template is
// copied by Manager.PlanProfileCreation; later package updates cannot mutate
// this saved Profile.
func (flow *ProfileOnboarding) ChooseTemplate(manager *Manager, id string) error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	if manager == nil {
		return errors.New("onboarding requires a Configuration Manager")
	}
	for _, template := range manager.Templates() {
		if template.ID == id {
			flow.resetAfter(OnboardingChooseSource)
			flow.draft.TemplateID = id
			flow.draft.TemplateRevision = template.Revision
			flow.draft.Instructions = ""
			flow.advance(OnboardingName)
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
	flow.resetAfter(OnboardingChooseSource)
	flow.draft.TemplateID = ""
	flow.draft.TemplateRevision = ""
	flow.draft.Instructions = string(instructions)
	if strings.TrimSpace(string(instructions)) == "" {
		flow.invalidate(OnboardingChooseSource)
		return nil
	}
	flow.advance(OnboardingName)
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
	flow.resetAfter(OnboardingChooseSource)
	flow.draft.TemplateID = ""
	flow.draft.TemplateRevision = ""
	flow.draft.Instructions = string(instructions)
	if strings.TrimSpace(string(instructions)) == "" {
		flow.invalidate(OnboardingChooseSource)
		return nil
	}
	flow.invalidate(OnboardingName)
	return nil
}

// SetName supplies the Profile name.
func (flow *ProfileOnboarding) SetName(name OnboardingText) error {
	return flow.setTextField(OnboardingName, name)
}

// SetReviewer supplies the Reviewer ID.
func (flow *ProfileOnboarding) SetReviewer(reviewer OnboardingText) error {
	return flow.setTextField(OnboardingReviewer, reviewer)
}

// SetModel supplies the exact model ID, including a manually entered ID.
func (flow *ProfileOnboarding) SetModel(model OnboardingText) error {
	return flow.setTextField(OnboardingModel, model)
}

// SetEffort supplies the saved reasoning effort.
func (flow *ProfileOnboarding) SetEffort(effort OnboardingText) error {
	return flow.setTextField(OnboardingEffort, effort)
}

// SetDeadline supplies the finite Attempt deadline.
func (flow *ProfileOnboarding) SetDeadline(deadline OnboardingText) error {
	return flow.setTextField(OnboardingDeadline, deadline)
}

// Validate creates the reviewed, opaque Manager Plan without publishing it.
func (flow *ProfileOnboarding) Validate(manager *Manager, repository Repository) (Plan, error) {
	if err := flow.ensureEditable(); err != nil {
		return Plan{}, err
	}
	if flow.step != OnboardingValidation {
		return Plan{}, errors.New("complete onboarding fields before validation")
	}
	if manager == nil {
		return Plan{}, errors.New("onboarding requires a Configuration Manager")
	}
	plan, err := manager.PlanProfileCreation(repository, flow.draft)
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
func (flow *ProfileOnboarding) Confirm(manager *Manager) error {
	if flow.step != OnboardingReview || !flow.plan.Valid() {
		return errors.New("onboarding requires a valid reviewed Plan before confirmation")
	}
	if manager == nil {
		return errors.New("onboarding requires a Configuration Manager")
	}
	if err := manager.Publish(flow.plan); err != nil {
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

func (flow *ProfileOnboarding) setField(step OnboardingStep) error {
	if err := flow.ensureEditable(); err != nil {
		return err
	}
	if onboardingStepRank(flow.step) < onboardingStepRank(step) {
		return fmt.Errorf("onboarding is not ready for %s", step)
	}
	flow.invalidate(step)
	return nil
}

func (flow *ProfileOnboarding) setTextField(step OnboardingStep, value OnboardingText) error {
	if err := flow.setField(step); err != nil {
		return err
	}
	field, found := onboardingStep(step)
	if !found || field.set == nil {
		return fmt.Errorf("onboarding step %s is not an editable field", step)
	}
	field.set(&flow.draft, value)
	flow.resetAfter(step)
	if strings.TrimSpace(string(value)) == "" {
		flow.step = step
		return nil
	}
	flow.advance(nextOnboardingStep(step))
	return nil
}

func (flow *ProfileOnboarding) advance(step OnboardingStep) {
	flow.plan = Plan{}
	flow.step = step
}

func (flow *ProfileOnboarding) invalidate(step OnboardingStep) {
	flow.plan = Plan{}
	flow.step = step
}

func (flow *ProfileOnboarding) resetAfter(step OnboardingStep) {
	rank := onboardingStepRank(step)
	if rank < 0 {
		return
	}
	for index := rank + 1; index < len(onboardingSteps); index++ {
		if clear := onboardingSteps[index].clear; clear != nil {
			clear(&flow.draft)
		}
	}
}

func (flow *ProfileOnboarding) nextStep() OnboardingStep {
	for _, step := range onboardingSteps {
		if step.terminal || step.ready == nil {
			return step.step
		}
		if !step.ready(flow.draft) {
			return step.step
		}
	}
	return OnboardingValidation
}

func onboardingStepRank(step OnboardingStep) int {
	for rank, candidate := range onboardingSteps {
		if candidate.step == step {
			return rank
		}
	}
	return -1
}

func onboardingStep(step OnboardingStep) (onboardingStepSpec, bool) {
	for _, candidate := range onboardingSteps {
		if candidate.step == step {
			return candidate, true
		}
	}
	return onboardingStepSpec{}, false
}

func nextOnboardingStep(step OnboardingStep) OnboardingStep {
	rank := onboardingStepRank(step)
	if rank >= 0 && rank+1 < len(onboardingSteps) {
		return onboardingSteps[rank+1].step
	}
	return OnboardingValidation
}

func hasOnboardingText(value string) bool {
	return strings.TrimSpace(value) != ""
}
