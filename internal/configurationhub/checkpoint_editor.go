package configurationhub

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

type checkpointOperation int

const (
	checkpointSet checkpointOperation = iota + 1
	checkpointRemove
	checkpointInherit
	checkpointOptOut
	checkpointEnroll
)

func (operation checkpointOperation) String() string {
	switch operation {
	case checkpointSet:
		return "set"
	case checkpointRemove:
		return "remove"
	case checkpointInherit:
		return "inherit"
	case checkpointOptOut:
		return "opt out"
	case checkpointEnroll:
		return "enroll"
	}
	return fmt.Sprintf("checkpointOperation(%d)", int(operation))
}

func (operation checkpointOperation) label() string {
	switch operation {
	case checkpointSet:
		return "Set Checkpoint"
	case checkpointRemove:
		return "Remove Checkpoint"
	case checkpointInherit:
		return "Inherit Global Checkpoints"
	case checkpointOptOut:
		return "Opt out of Global Checkpoints"
	case checkpointEnroll:
		return "Enroll this repository"
	}
	return operation.String()
}

// checkpointFormDraft is one Checkpoints edit of one configuration file. The
// embedded draft names the Checkpoint a set or remove changes.
type checkpointFormDraft struct {
	target    configuration.Scope
	operation checkpointOperation
	checkpointDraft
}

// planCheckpointEdit is the one Checkpoints planner both Hub hosts and the
// first-use journey call. It refuses an edit that changes nothing with the
// fact that makes it a no-op.
func planCheckpointEdit(manager *configuration.Manager, repository configuration.Repository, draft checkpointFormDraft) (configuration.Plan, error) {
	plan, err := draft.plan(manager, repository)
	if err != nil {
		return configuration.Plan{}, err
	}
	if plan.Unchanged() {
		return configuration.Plan{}, errors.New(draft.unchangedFact())
	}
	return plan, nil
}

func (draft checkpointFormDraft) plan(manager *configuration.Manager, repository configuration.Repository) (configuration.Plan, error) {
	var intent configuration.Intent
	switch draft.operation {
	case checkpointEnroll:
		return manager.PlanEnrollment(repository)
	case checkpointSet:
		checkpoint, err := draft.checkpoint()
		if err != nil {
			return configuration.Plan{}, err
		}
		intent = configuration.SetCheckpoint{Target: draft.target, Name: draft.name, Checkpoint: checkpoint}
	case checkpointRemove:
		intent = configuration.RemoveCheckpoint{Target: draft.target, Name: draft.name}
	case checkpointInherit:
		intent = configuration.InheritGlobalCheckpoints{}
	case checkpointOptOut:
		intent = configuration.DeclareNoCheckpoints{}
	default:
		return configuration.Plan{}, fmt.Errorf("unknown Checkpoint operation %q", draft.operation)
	}
	return manager.Plan(repository, []configuration.Intent{intent})
}

// unchangedFact explains a no-op. Inherit and opt out never reach it: their
// intents refuse a no-op themselves.
func (draft checkpointFormDraft) unchangedFact() string {
	title := configurationTitle(draft.target)
	switch draft.operation {
	case checkpointSet:
		return fmt.Sprintf("%s already declares this %s Checkpoint.", title, draft.name)
	case checkpointRemove:
		return fmt.Sprintf("%s does not declare the %s Checkpoint.", title, draft.name)
	case checkpointEnroll:
		return "This repository is already enrolled: it has a Repository Configuration file."
	case checkpointInherit, checkpointOptOut:
	}
	return fmt.Sprintf("%s already holds these Checkpoints.", title)
}

// checkpointDraft holds the form answers; numbers stay text until parsed.
type checkpointDraft struct {
	name            configuration.CheckpointName
	requirement     configuration.CheckpointRequirement
	exemptions      string
	unreviewedLines string
	reviewBudget    string
	waivers         configuration.WaiverPolicy
	integrations    []configuration.IntegrationName
}

func newCheckpointDraft(name configuration.CheckpointName, checkpoint configuration.Checkpoint) checkpointDraft {
	return checkpointDraft{
		name: name, requirement: checkpoint.Requirement, exemptions: strings.Join(checkpoint.ExemptPaths, " "),
		unreviewedLines: strconv.Itoa(checkpoint.UnreviewedLines), reviewBudget: strconv.Itoa(checkpoint.ReviewBudget),
		waivers: checkpoint.Waivers, integrations: slices.Clone(checkpoint.Integrations),
	}
}

func (draft checkpointDraft) checkpoint() (configuration.Checkpoint, error) {
	checkpoint := configuration.NewCheckpoint()
	checkpoint.Requirement = draft.requirement
	checkpoint.ExemptPaths = strings.Fields(draft.exemptions)
	checkpoint.Waivers = draft.waivers
	checkpoint.Integrations = draft.integrations
	lines, err := strconv.Atoi(strings.TrimSpace(draft.unreviewedLines))
	if err != nil {
		return checkpoint, err
	}
	budget, err := strconv.Atoi(strings.TrimSpace(draft.reviewBudget))
	checkpoint.UnreviewedLines, checkpoint.ReviewBudget = lines, budget
	return checkpoint, err
}

// checkpointFields asks for every rule of one Checkpoint.
func checkpointFields(draft *checkpointDraft) []huh.Field {
	return []huh.Field{
		huh.NewSelect[configuration.CheckpointRequirement]().Title("What should the Checkpoint require?").Options(
			huh.NewOption("reviewed: a completed Review of every selected Profile covers the change", configuration.RequirementReviewed),
			huh.NewOption("judged: covered, and every Finding of those Reviews has a verdict", configuration.RequirementJudged),
		).Value(&draft.requirement),
		huh.NewInput().Title("Exempt paths (space-separated patterns such as *.md docs/**; blank for none)").Value(&draft.exemptions).Validate(validateExemptions),
		huh.NewInput().Title("How many unreviewed lines may follow a Review without a new one? (0 for none)").Value(&draft.unreviewedLines).Validate(validateUnreviewedLines),
		huh.NewInput().Title("How many Reviews may one change spend before a person must step in? (1 to 9)").Value(&draft.reviewBudget).Validate(validateReviewBudget),
		huh.NewSelect[configuration.WaiverPolicy]().Title("Who may waive the Checkpoint?").Options(
			huh.NewOption("human: a person confirms in a terminal", configuration.WaiversHuman),
			huh.NewOption("anyone: any caller, including agents and scripts", configuration.WaiversAnyone),
			huh.NewOption("none: only Reviews pass it", configuration.WaiversNone),
		).Value(&draft.waivers),
		huh.NewMultiSelect[configuration.IntegrationName]().Title("Which Integrations must every contributor install (the team floor)?").
			Options(integrationOptions(configuration.IntegrationNames())...).Value(&draft.integrations),
	}
}

func checkpointNameOptions(names []configuration.CheckpointName) []huh.Option[configuration.CheckpointName] {
	labels := map[configuration.CheckpointName]string{
		configuration.CheckpointPrePush:   "pre-push: the selected Reviews cover a change before it is pushed",
		configuration.CheckpointPreCommit: "pre-commit: the selected Reviews cover staged content before it is committed",
	}
	options := make([]huh.Option[configuration.CheckpointName], 0, len(names))
	for _, name := range names {
		options = append(options, huh.NewOption(labels[name], name))
	}
	return options
}

// checkpointNameField asks which Checkpoint a set declares or a remove stops
// declaring. A remove offers only the names the file declares.
func checkpointNameField(draft *checkpointFormDraft, file CheckpointFile) huh.Field {
	title, names := "Which Checkpoint should %s declare?", configuration.CheckpointNames()
	if draft.operation == checkpointRemove {
		title, names = "Which Checkpoint should %s stop declaring?", configuration.SortedCheckpointNames(file.Declared)
	}
	return huh.NewSelect[configuration.CheckpointName]().Title(fmt.Sprintf(title, configurationTitle(draft.target))).
		Options(checkpointNameOptions(names)...).Value(&draft.name)
}

// integrationsNotice runs the caller's undeclared-Integrations check after a
// Checkpoints publish. A failed check is reported, never fatal: the publish
// already happened.
func integrationsNotice(check func() (string, error)) string {
	if check == nil {
		return ""
	}
	notice, err := check()
	if err != nil {
		return "Checking installed Checkpoint Integrations failed: " + err.Error()
	}
	return notice
}

func (e *editor) editCheckpoints() error {
	view := e.snapshot.Checkpoints
	target, err := e.chooseFile(func(target configuration.Scope) string {
		file, _ := view.file(target)
		return view.headerState(file)
	})
	if err != nil {
		return err
	}
	file, _ := view.file(target)
	draft := &e.draftSet().checkpoints
	*draft = checkpointFormDraft{target: target}
	var options []huh.Option[checkpointOperation]
	for _, operation := range view.operations(file) {
		options = append(options, huh.NewOption(operation.label(), operation))
	}
	if err := e.form(huh.NewSelect[checkpointOperation]().Title(configurationTitle(target)).Options(options...).Value(&draft.operation)); err != nil {
		return err
	}
	if err := e.fillCheckpointDraft(draft, file); err != nil {
		return err
	}
	plan, err := planCheckpointEdit(e.manager, e.Repository, *draft)
	if err != nil {
		return err
	}
	published, err := e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	if !published || err != nil {
		return err
	}
	*draft = checkpointFormDraft{}
	if notice := integrationsNotice(e.UndeclaredIntegrations); notice != "" {
		_, err = fmt.Fprintln(e.Output, notice)
	}
	return err
}

// fillCheckpointDraft asks what a set or remove changes. A set starts from
// the prefilled Checkpoint of the chosen name.
func (e *editor) fillCheckpointDraft(draft *checkpointFormDraft, file CheckpointFile) error {
	switch draft.operation {
	case checkpointSet:
		draft.name = configuration.CheckpointPrePush
		if err := e.form(checkpointNameField(draft, file)); err != nil {
			return err
		}
		*draft = e.snapshot.Checkpoints.setDraft(draft.target, draft.name, configuration.NewCheckpoint())
		return e.form(checkpointFields(&draft.checkpointDraft)...)
	case checkpointRemove:
		return e.form(checkpointNameField(draft, file))
	case checkpointInherit, checkpointOptOut, checkpointEnroll:
	}
	return nil
}
