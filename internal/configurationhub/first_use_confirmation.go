package configurationhub

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// First-use forms run synchronously. Host their confirmation in the Hub's
// scrollable plan view so opening an alternate screen never hides the Plan
// that was previously printed before the confirmation prompt.
type firstUseConfirmation struct {
	Model
	confirmed bool
	aborted   bool
}

func runFirstUseConfirmation(options RunOptions, summary string) (bool, error) {
	model := New(Snapshot{Repository: string(options.Repository), Targets: hubTargets(options.Repository)})
	model.view = viewPlanPreview
	model.planSummary = summary
	result, err := tea.NewProgram(firstUseConfirmation{Model: model},
		tea.WithContext(options.Context), tea.WithInput(options.Input), tea.WithOutput(options.Output),
	).Run()
	if err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return false, huh.ErrUserAborted
		}
		return false, err
	}
	finished, ok := result.(firstUseConfirmation)
	if !ok {
		return false, fmt.Errorf("setup confirmation returned %T", result)
	}
	if finished.aborted {
		return false, huh.ErrUserAborted
	}
	return finished.confirmed, nil
}

func (model firstUseConfirmation) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "p":
			model.confirmed = true
			return model, tea.Quit
		case "e":
			return model, tea.Quit
		case "esc", "ctrl+c", "q":
			model.aborted = true
			return model, tea.Quit
		}
	}
	updated, command := model.Model.Update(message)
	if hub, ok := updated.(Model); ok {
		model.Model = hub
	}
	return model, command
}

func (model firstUseConfirmation) View() tea.View {
	view := model.Model.View()
	view.WindowTitle = "Review Party setup"
	return view
}
