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

// HookInstaller installs the git Integration for every declared Checkpoint.
// It prints what it would write, asks confirm only when something would be
// written, and reports the result to the Hub's output.
type HookInstaller func(confirm func() (bool, error)) error

// checkpointsStep shows the declared Checkpoints or offers to declare one,
// then offers the git Integration for whatever is declared.
func (e *editor) checkpointsStep() error {
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

// checkpointDraft holds the form answers; numbers stay text until parsed.
type checkpointDraft struct {
	name             configuration.CheckpointName
	exemptions       string
	smallChangeLines string
	waivers          configuration.WaiverPolicy
}

func (draft checkpointDraft) checkpoint() (configuration.Checkpoint, error) {
	checkpoint := configuration.NewCheckpoint()
	checkpoint.ExemptPaths = strings.Fields(draft.exemptions)
	checkpoint.Waivers = draft.waivers
	checkpoint.Integrations = []configuration.IntegrationName{configuration.IntegrationGit}
	lines, err := strconv.Atoi(strings.TrimSpace(draft.smallChangeLines))
	checkpoint.SmallChangeLines = lines
	return checkpoint, err
}

// declareCheckpoint offers to declare one Checkpoint and publishes it
// through its own Plan.
func (e *editor) declareCheckpoint() error {
	draft := checkpointDraft{name: configuration.CheckpointPrePush, smallChangeLines: "0", waivers: configuration.WaiversHuman}
	choice := huh.NewSelect[configuration.CheckpointName]().
		Title("Which Review Checkpoint should this repository declare?").
		Options(
			huh.NewOption("pre-push: the selected Reviews cover a change before it is pushed", configuration.CheckpointPrePush),
			huh.NewOption("pre-commit: the selected Reviews cover staged content before it is committed", configuration.CheckpointPreCommit),
			huh.NewOption("none", configuration.CheckpointName("")),
		).Value(&draft.name)
	if err := e.form(choice); err != nil || draft.name == "" {
		return err
	}
	if err := e.form(
		huh.NewInput().Title("Exempt paths (space-separated patterns such as *.md docs/**; blank for none)").Value(&draft.exemptions).Validate(validateExemptions),
		huh.NewInput().Title("Pass changes of at most this many changed lines (0 disables)").Value(&draft.smallChangeLines).Validate(validateLineLimit),
		huh.NewSelect[configuration.WaiverPolicy]().Title("Who may waive the Checkpoint?").Options(
			huh.NewOption("human: a person confirms in a terminal", configuration.WaiversHuman),
			huh.NewOption("anyone: any caller, including agents and scripts", configuration.WaiversAnyone),
			huh.NewOption("none: only Reviews pass it", configuration.WaiversNone),
		).Value(&draft.waivers),
	); err != nil {
		return err
	}
	checkpoint, err := draft.checkpoint()
	if err != nil {
		return err
	}
	if err := e.warnDocumentationExemption(checkpoint); err != nil {
		return err
	}
	plan, err := e.manager.Plan(e.Repository, []configuration.Intent{configuration.SetCheckpoint{Name: draft.name, Checkpoint: checkpoint}})
	if err != nil {
		return err
	}
	_, err = e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	return err
}

func validateExemptions(value string) error {
	var errs []error
	for _, pattern := range strings.Fields(value) {
		errs = append(errs, configuration.ValidatePathPattern(pattern))
	}
	return errors.Join(errs...)
}

func validateLineLimit(value string) error {
	lines, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || lines < 0 {
		return errors.New("enter a whole number of lines, 0 or more")
	}
	return nil
}

// warnDocumentationExemption warns when Markdown exemptions would keep a
// selected documentation Profile from ever being required at the Checkpoint.
func (e *editor) warnDocumentationExemption(checkpoint configuration.Checkpoint) error {
	if !checkpoint.Exempts("README.md") && !checkpoint.Exempts("docs/README.md") {
		return nil
	}
	profiles, err := e.selectedDocumentationProfiles()
	if err != nil || len(profiles) == 0 {
		return err
	}
	_, err = fmt.Fprintf(e.Output, "warning: exempting *.md means this Checkpoint never requires documentation Profile %s to review Markdown changes.\n", strings.Join(profiles, ", "))
	return err
}

// selectedDocumentationProfiles names the selected Profiles whose name or
// Template marks them as documentation reviews.
func (e *editor) selectedDocumentationProfiles() ([]string, error) {
	resolved, err := e.manager.ResolveRun(configuration.RunRequest{Repository: e.Repository})
	if err != nil {
		return nil, err
	}
	inventory, err := e.manager.ProfileInventory(e.Repository)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, definition := range inventory {
		selected := slices.ContainsFunc(resolved.Expanded, func(profile configuration.ExpandedProfile) bool {
			return profile.Scope == definition.Scope && profile.Profile == definition.Name
		})
		marker := strings.ToLower(definition.Name + " " + definition.Value.TemplateID)
		if selected && strings.Contains(marker, "doc") {
			names = append(names, definition.Name)
		}
	}
	return names, nil
}

// integrationsStep offers the git hooks for the declared Checkpoints,
// preselecting yes. The installer reports a hook that is already in place.
func (e *editor) integrationsStep() error {
	if e.InstallCheckpointHooks == nil {
		return nil
	}
	declared, err := e.manager.Checkpoints(e.Repository)
	if err != nil || len(declared) == 0 {
		return err
	}
	return e.InstallCheckpointHooks(func() (bool, error) {
		install := true
		err := e.form(huh.NewConfirm().Title("Install these git hooks now?").Value(&install))
		return install, err
	})
}
