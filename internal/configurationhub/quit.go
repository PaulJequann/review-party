package configurationhub

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type quitConfirmation struct {
	form    formAdapter
	discard bool
}

func (model Model) requestQuit() (tea.Model, tea.Cmd) {
	model.syncFormDraft()
	if model.drafts.empty() {
		return model, tea.Quit
	}
	confirmation := &quitConfirmation{}
	confirmation.form = newFormAdapter([]huh.Field{
		huh.NewConfirm().Title("Discard unfinished drafts and exit?").
			Affirmative("Discard and exit").Negative("Keep working").Value(&confirmation.discard),
	}, model.width, model.height-4, huh.ThemeFunc(huh.ThemeCharm))
	model.quit = confirmation
	return model, confirmation.form.Init()
}

func (model Model) updateQuit(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "esc", "ctrl+c":
		model.quit = nil
		return model, nil
	default:
		return model.updateQuitForm(message)
	}
}

func (model Model) updateQuitForm(message tea.Msg) (tea.Model, tea.Cmd) {
	var command tea.Cmd
	model.quit.form, command = model.quit.form.Update(message)
	switch model.quit.form.State() {
	case huh.StateCompleted:
		if model.quit.discard {
			model.drafts.clear()
			model.quit = nil
			return model, tea.Quit
		}
		model.quit = nil
	case huh.StateAborted:
		model.quit = nil
	case huh.StateNormal:
	}
	return model, command
}

func (model Model) updateOwnedForm(message formMessage) (tea.Model, tea.Cmd) {
	if model.quit != nil && message.form == model.quit.form.form {
		return model.updateQuitForm(message)
	}
	if model.view == viewForm && message.form == model.form.form {
		updated, command := model.updateForm(message)
		return refreshHubModel(updated), command
	}
	return model, nil
}

func (model Model) renderQuit() string {
	return model.quit.form.View() + "\n\nEnter confirms the choice. Escape or Ctrl+C keeps your work."
}
