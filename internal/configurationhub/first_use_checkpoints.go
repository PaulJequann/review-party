package configurationhub

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

// HookInstaller installs one Integration for every declared Checkpoint, into
// the team's files or, when personal, the Caller's own. It prints what it
// would write, asks confirm only when something would be written, and reports
// the result to the Hub's output.
type HookInstaller func(integration configuration.IntegrationName, personal bool, confirm func() (bool, error)) error

func (e *editor) checkpointsStep() error {
	if err := e.offerEnrollment(); err != nil {
		return err
	}
	declared, err := e.manager.Checkpoints(e.Repository)
	switch {
	case err != nil:
		return err
	case len(declared) == 0:
		err = e.declareCheckpoint()
	default:
		var summary strings.Builder
		for _, name := range configuration.SortedCheckpointNames(declared) {
			summary.WriteString("Checkpoint " + declared[name].Summary(name) + "\n")
		}
		_, err = io.WriteString(e.Output, summary.String())
	}
	if err != nil {
		return err
	}
	return e.integrationsStep()
}

func (e *editor) offerEnrollment() error {
	enrollment, err := e.manager.Enrollment(e.Repository)
	if err != nil || !enrollment.Offered() {
		return err
	}
	var summary strings.Builder
	for _, name := range configuration.SortedCheckpointNames(enrollment.GlobalCheckpoints) {
		summary.WriteString("Global Checkpoint " + enrollment.GlobalCheckpoints[name].Summary(name) + "\n")
	}
	if _, err := io.WriteString(e.Output, summary.String()); err != nil {
		return err
	}
	var enroll bool
	if err := e.form(huh.NewConfirm().Title("Enroll this repository in your Global Checkpoints?").Value(&enroll)); err != nil || !enroll {
		return err
	}
	plan, err := planCheckpointEdit(e.manager, e.Repository, checkpointFormDraft{target: configuration.ScopeRepository, operation: checkpointEnroll})
	if err != nil {
		return err
	}
	_, err = e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	return err
}

// declareCheckpoint offers to declare one Checkpoint and publishes it
// through its own Plan.
func (e *editor) declareCheckpoint() error {
	name := configuration.CheckpointPrePush
	choice := huh.NewSelect[configuration.CheckpointName]().
		Title("Which Review Checkpoint should this repository declare?").
		Options(append(checkpointNameOptions(configuration.CheckpointNames()), huh.NewOption("none", configuration.CheckpointName("")))...).
		Value(&name)
	if err := e.form(choice); err != nil || name == "" {
		return err
	}
	if err := e.refresh(); err != nil {
		return err
	}
	fallback := configuration.NewCheckpoint()
	fallback.Integrations = slices.Concat([]configuration.IntegrationName{configuration.IntegrationGit}, e.AgentsOnPath, []configuration.IntegrationName{configuration.IntegrationAgentsMD})
	draft := e.snapshot.Checkpoints.setDraft(configuration.ScopeRepository, name, fallback)
	if err := e.form(checkpointFields(&draft.checkpointDraft)...); err != nil {
		return err
	}
	checkpoint, err := draft.checkpoint()
	if err != nil {
		return err
	}
	if err := e.warnDocumentationExemption(checkpoint); err != nil {
		return err
	}
	plan, err := planCheckpointEdit(e.manager, e.Repository, draft)
	if err != nil {
		return err
	}
	_, err = e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	return err
}

func validateExemptions(value string) error {
	patterns, err := parseExemptions(value)
	if err != nil {
		return err
	}
	var errs []error
	for _, pattern := range patterns {
		errs = append(errs, configuration.ValidatePathPattern(pattern))
	}
	return errors.Join(errs...)
}

func validateUnreviewedLines(value string) error {
	lines, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || lines < 0 {
		return errors.New("enter a whole number of lines, 0 or more")
	}
	return nil
}

func validateReviewBudget(value string) error {
	budget, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || !configuration.ValidReviewBudget(budget) {
		return fmt.Errorf("enter a whole number of Reviews from 1 to %d", configuration.MaxReviewBudget)
	}
	return nil
}

// warnDocumentationExemption warns when Markdown exemptions would keep a
// selected documentation Profile from ever being required at the Checkpoint.
func (e *editor) warnDocumentationExemption(checkpoint configuration.Checkpoint) error {
	if len(checkpoint.MarkdownExemptions()) == 0 {
		return nil
	}
	profiles, err := e.manager.SelectedDocumentationProfiles(e.Repository)
	if err != nil || len(profiles) == 0 {
		return err
	}
	_, err = fmt.Fprintf(e.Output, "warning: exempting *.md means this Checkpoint never requires documentation Profile %s to review Markdown changes.\n", strings.Join(profiles, ", "))
	return err
}

// integrationOptions labels Integrations for a multi-select.
func integrationOptions(integrations []configuration.IntegrationName) []huh.Option[configuration.IntegrationName] {
	labels := map[configuration.IntegrationName]string{
		configuration.IntegrationGit:        "git: git hooks",
		configuration.IntegrationClaudeCode: "claude-code: a Claude Code PreToolUse hook",
		configuration.IntegrationCodex:      "codex: a Codex PreToolUse hook",
		configuration.IntegrationAgentsMD:   "agents-md: a block in AGENTS.md, or CLAUDE.md when only that exists",
	}
	options := make([]huh.Option[configuration.IntegrationName], 0, len(integrations))
	for _, integration := range integrations {
		options = append(options, huh.NewOption(labels[integration], integration))
	}
	return options
}

// integrationsStep installs the team floor the declared Checkpoints list,
// preselecting yes, then offers the Caller Agent hooks outside the floor as
// personal additions, preselecting the agents on PATH. The installer reports
// a hook that is already in place.
func (e *editor) integrationsStep() error {
	if e.InstallCheckpointHooks == nil {
		return nil
	}
	declared, err := e.manager.Checkpoints(e.Repository)
	if err != nil || len(declared) == 0 {
		return err
	}
	var personal []configuration.IntegrationName
	for _, integration := range configuration.IntegrationNames() {
		switch {
		case configuration.FloorIntegrates(declared, integration):
			err = e.InstallCheckpointHooks(integration, false, e.confirmHooks(integration))
		case slices.Contains(callerAgentIntegrations, integration):
			personal = append(personal, integration)
		}
		if err != nil {
			return err
		}
	}
	return e.personalIntegrations(personal)
}

// callerAgentIntegrations are the Integrations a Caller may install for
// themselves alone: the Caller Agent hooks. git hooks and the AGENTS.md block
// live in files the team shares.
var callerAgentIntegrations = []configuration.IntegrationName{configuration.IntegrationClaudeCode, configuration.IntegrationCodex}

func (e *editor) confirmHooks(integration configuration.IntegrationName) func() (bool, error) {
	return func() (bool, error) {
		install, title := true, "Install these "+string(integration)+" hooks now?"
		if integration == configuration.IntegrationAgentsMD {
			title = "Write this agents-md block now?"
		}
		err := e.form(huh.NewConfirm().Title(title).Value(&install))
		return install, err
	}
}

// personalIntegrations offers Caller Agent hooks for the Caller alone.
func (e *editor) personalIntegrations(offered []configuration.IntegrationName) error {
	if len(offered) == 0 {
		return nil
	}
	chosen := slices.DeleteFunc(slices.Clone(offered), func(integration configuration.IntegrationName) bool {
		return !slices.Contains(e.AgentsOnPath, integration)
	})
	if err := e.form(huh.NewMultiSelect[configuration.IntegrationName]().Title("Install agent hooks for yourself only?").
		Options(integrationOptions(offered)...).Value(&chosen)); err != nil {
		return err
	}
	for _, integration := range chosen {
		if err := e.InstallCheckpointHooks(integration, true, e.confirmHooks(integration)); err != nil {
			return err
		}
	}
	return nil
}
